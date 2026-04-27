package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

// AgentLLMClient is the seam the agent loop uses to talk to a model.
// Production wires this to gateway.Service.HandleChat — that gives the
// agent the same provider routing, caching, budget tracking, and audit
// trail as any other client. Tests substitute a fake.
//
// The interface accepts a full ChatRequest (with Tools populated) and
// returns a ChatResponse — the loop then inspects the assistant's
// message for tool_calls.
type AgentLLMClient interface {
	Chat(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error)
}

// AgentLLMClientFunc is the function-form of AgentLLMClient — saves
// callers from having to declare a struct just to satisfy a one-method
// interface. Production wiring uses this to adapt
// gateway.Service.HandleChat (which returns a wrapped ChatResult) into
// the bare ChatResponse the loop expects.
type AgentLLMClientFunc func(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error)

func (f AgentLLMClientFunc) Chat(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	return f(ctx, req)
}

// AgentLoopExecutor drives an LLM in a tool-use loop. The flow each
// turn:
//
//  1. Send the conversation (system prompt + user prompt + prior turns)
//     plus the tool catalog to the LLM
//  2. If the assistant returns tool_calls, dispatch each one to the
//     local tool executor (shell / git / file) and append the result
//     as a tool-role message
//  3. If no tool_calls, the assistant has finished; return its message
//     as the final answer
//  4. Loop until done or MaxTurns hits
//
// We don't gate mid-loop tool calls on approvals for v0.1 — the
// approval gate fires once at the task level, and the agent then runs
// to completion. Mid-loop approval (pause-and-resume the conversation)
// is a v0.2 feature; it needs persisted conversation state and
// explicit resume semantics.
type AgentLoopExecutor struct {
	llm      AgentLLMClient
	shell    Executor
	file     Executor
	git      Executor
	maxTurns int
}

// NewAgentLoopExecutor constructs the loop. A nil LLM client is
// allowed at construction time so the gateway can boot before its
// chat service is wired (main.go calls SetAgentLLMClient as a second
// step). Running an agent_loop task with a nil client fails fast
// with a clear "no LLM configured" error — the right signal for the
// operator to wire a model before retrying.
//
// maxTurns caps how many LLM round-trips a single run can do. 0 (or
// negative) defaults to 8 — generous enough for typical multi-step
// tasks but tight enough that a runaway loop costs <$0.10 even on
// expensive models.
func NewAgentLoopExecutor(llm AgentLLMClient, shell Executor, file Executor, git Executor, maxTurns int) *AgentLoopExecutor {
	if maxTurns <= 0 {
		maxTurns = 8
	}
	return &AgentLoopExecutor{
		llm:      llm,
		shell:    shell,
		file:     file,
		git:      git,
		maxTurns: maxTurns,
	}
}

// Execute runs the loop. Steps and artifacts produced by each turn
// (model thinking + tool execution) are upserted via the spec's
// callbacks; the final ExecutionResult mirrors the standard executor
// shape so the runner can persist it identically.
func (e *AgentLoopExecutor) Execute(ctx context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	if spec.NewID == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	if e.llm == nil {
		// No LLM configured — fall back to deterministic pass-through.
		// This isn't an "agent loop" but it's better than a hard
		// failure at runtime. The operator sees the result and knows
		// to configure a model.
		return e.runWithoutLLM(ctx, spec)
	}

	startedAt := spec.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	baseIndex := 0
	if spec.ResumeCheckpoint != nil && spec.ResumeCheckpoint.LastStepIndex > 0 {
		baseIndex = spec.ResumeCheckpoint.LastStepIndex
	}
	nextIndex := baseIndex + 1

	allSteps := make([]types.TaskStep, 0, e.maxTurns*2)
	allArtifacts := make([]types.TaskArtifact, 0, e.maxTurns)

	// Build the initial conversation. The user-supplied prompt is the
	// user message; we don't currently inject a system prompt — the
	// task's own Prompt carries enough intent. Per-tenant system
	// prompts are a v0.2 feature.
	messages := []types.Message{
		{Role: "user", Content: spec.Task.Prompt},
	}
	tools := agentToolDefinitions()

	finalResult := &ExecutionResult{
		Status:    "completed",
		Steps:     allSteps,
		Artifacts: allArtifacts,
	}

	for turn := 1; turn <= e.maxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			finalResult.Status = "cancelled"
			finalResult.LastError = err.Error()
			finalResult.OtelStatusCode = "error"
			finalResult.OtelStatusMessage = "context cancelled mid-loop"
			finalResult.Steps = allSteps
			finalResult.Artifacts = allArtifacts
			return finalResult, nil
		}

		// 1. LLM round-trip.
		req := types.ChatRequest{
			RequestID: spec.RequestID,
			Model:     spec.Run.Model,
			Messages:  messages,
			Tools:     tools,
			Scope: types.RequestScope{
				Tenant: spec.Task.Tenant,
				User:   spec.Task.User,
			},
		}
		turnStartedAt := time.Now().UTC()
		resp, err := e.llm.Chat(ctx, req)
		if err != nil {
			return e.failedFromError(spec, allSteps, allArtifacts, nextIndex, turnStartedAt,
				fmt.Sprintf("LLM call failed on turn %d: %v", turn, err))
		}
		if resp == nil || len(resp.Choices) == 0 {
			return e.failedFromError(spec, allSteps, allArtifacts, nextIndex, turnStartedAt,
				fmt.Sprintf("LLM returned empty response on turn %d", turn))
		}
		assistantMsg := resp.Choices[0].Message

		// 2. Record this turn's "thinking" step — captures the
		// assistant message content + which tools it asked for. Even
		// when the assistant only emits tool_calls (no text), we
		// still produce a step so the run timeline shows the agent's
		// decision points.
		thinkingStep := buildThinkingStep(spec, nextIndex, turn, turnStartedAt, assistantMsg, resp)
		nextIndex++
		if err := upsertTaskStep(spec, thinkingStep); err != nil {
			return nil, err
		}
		allSteps = append(allSteps, thinkingStep)

		// 3. Append the assistant message to the running conversation
		// regardless — even an answer-only turn must be in history if
		// we somehow re-enter the loop later.
		messages = append(messages, assistantMsg)

		// 4. If no tool calls, the assistant gave a final answer.
		// Save the final-answer artifact and exit.
		if len(assistantMsg.ToolCalls) == 0 {
			finalArtifact := buildFinalAnswerArtifact(spec, thinkingStep.ID, turnStartedAt, assistantMsg.Content)
			if err := upsertTaskArtifact(spec, finalArtifact); err != nil {
				return nil, err
			}
			allArtifacts = append(allArtifacts, finalArtifact)
			finalResult.Steps = allSteps
			finalResult.Artifacts = allArtifacts
			finalResult.OtelStatusCode = "ok"
			return finalResult, nil
		}

		// 5. Dispatch each tool call in order. Failures append a tool
		// result with the error text — the LLM can decide how to
		// recover. We don't stop the loop on tool failure; the agent
		// might intend to handle it. The max-turns cap is the
		// universal safety net.
		for _, toolCall := range assistantMsg.ToolCalls {
			toolResultText, toolStep, toolArtifacts, dispatchErr := e.dispatchToolCall(ctx, spec, toolCall, nextIndex)
			if toolStep != nil {
				if err := upsertTaskStep(spec, *toolStep); err != nil {
					return nil, err
				}
				allSteps = append(allSteps, *toolStep)
				nextIndex++
			}
			for _, art := range toolArtifacts {
				if err := upsertTaskArtifact(spec, art); err != nil {
					return nil, err
				}
				allArtifacts = append(allArtifacts, art)
			}
			// Append the tool-result message to the conversation so
			// the next LLM turn can see what happened.
			messages = append(messages, types.Message{
				Role:       "tool",
				Content:    toolResultText,
				ToolCallID: toolCall.ID,
			})
			// dispatchErr is non-nil only on internal errors (unknown
			// tool, malformed args). Real tool failures are captured
			// in toolResultText.
			_ = dispatchErr
		}
	}

	// Hit max turns without a final answer. Mark incomplete; the user
	// can resume the run if they want more turns.
	finalResult.Status = "failed"
	finalResult.LastError = fmt.Sprintf("agent loop hit maxTurns=%d without producing a final answer", e.maxTurns)
	finalResult.OtelStatusCode = "error"
	finalResult.OtelStatusMessage = "max_turns_exceeded"
	finalResult.Steps = allSteps
	finalResult.Artifacts = allArtifacts
	return finalResult, nil
}

// dispatchToolCall translates one assistant tool_call into an Executor
// invocation, returning the result text the LLM sees on the next turn.
//
// Returns:
//   - toolResultText: what to feed back as the tool-role message
//   - toolStep: the orchestrator step for this tool execution (nil if
//     the call couldn't be dispatched)
//   - toolArtifacts: any artifacts the tool produced
//   - dispatchErr: non-nil for *internal* errors (unknown tool,
//     malformed args); tool-level failures are encoded in toolResultText
func (e *AgentLoopExecutor) dispatchToolCall(ctx context.Context, spec ExecutionSpec, call types.ToolCall, stepIndex int) (string, *types.TaskStep, []types.TaskArtifact, error) {
	startedAt := time.Now().UTC()

	// Decode the tool arguments. Each tool gets its own typed shape;
	// see agentToolDefinitions() for the schemas. A malformed args
	// blob is reported back to the LLM as a tool failure rather than
	// crashing the run — gives the model a chance to retry.
	switch call.Function.Name {
	case "shell_exec":
		var args shellExecArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return fmt.Sprintf("invalid arguments for shell_exec: %v", err), nil, nil, nil
		}
		taskCopy := spec.Task
		taskCopy.ExecutionKind = "shell"
		taskCopy.ShellCommand = args.Command
		taskCopy.WorkingDirectory = args.WorkingDirectory
		return e.runSubExecutor(ctx, spec, e.shell, taskCopy, stepIndex, startedAt, call.ID, call.Function.Name)

	case "git_exec":
		var args gitExecArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return fmt.Sprintf("invalid arguments for git_exec: %v", err), nil, nil, nil
		}
		taskCopy := spec.Task
		taskCopy.ExecutionKind = "git"
		taskCopy.GitCommand = args.Command
		taskCopy.WorkingDirectory = args.WorkingDirectory
		return e.runSubExecutor(ctx, spec, e.git, taskCopy, stepIndex, startedAt, call.ID, call.Function.Name)

	case "file_write":
		var args fileWriteArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return fmt.Sprintf("invalid arguments for file_write: %v", err), nil, nil, nil
		}
		op := args.Operation
		if op == "" {
			op = "write"
		}
		taskCopy := spec.Task
		taskCopy.ExecutionKind = "file"
		taskCopy.FilePath = args.Path
		taskCopy.FileContent = args.Content
		taskCopy.FileOperation = op
		return e.runSubExecutor(ctx, spec, e.file, taskCopy, stepIndex, startedAt, call.ID, call.Function.Name)

	default:
		return fmt.Sprintf("unknown tool: %s", call.Function.Name), nil, nil, nil
	}
}

// runSubExecutor delegates to one of the per-kind executors and
// massages the result back into the shape the agent loop wants. The
// returned step belongs to this loop iteration and gets re-indexed at
// the call site to keep step.Index monotonic across mixed turns.
func (e *AgentLoopExecutor) runSubExecutor(ctx context.Context, spec ExecutionSpec, exec Executor, task types.Task, stepIndex int, startedAt time.Time, toolCallID, toolName string) (string, *types.TaskStep, []types.TaskArtifact, error) {
	if exec == nil {
		return fmt.Sprintf("%s tool is not configured on this gateway", toolName), nil, nil, nil
	}
	subSpec := ExecutionSpec{
		Task:       task,
		Run:        spec.Run,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
		RootSpanID: spec.RootSpanID,
		StartedAt:  startedAt,
		NewID:      spec.NewID,
		// Sub-executor must NOT independently upsert into the store —
		// we batch artifacts/steps at the agent-loop level so the
		// indices stay coherent. Pass nil callbacks; the returned
		// ExecutionResult carries the rows for us to renumber.
		UpsertStep:     nil,
		UpsertArtifact: nil,
	}
	subResult, err := exec.Execute(ctx, subSpec)
	if err != nil {
		return fmt.Sprintf("%s tool internal error: %v", toolName, err), nil, nil, nil
	}
	if subResult == nil {
		return fmt.Sprintf("%s tool returned nothing", toolName), nil, nil, nil
	}

	// Build a single agent-loop step that summarizes the sub-tool's
	// outcome. We don't replay every sub-step the per-kind executor
	// produced — that would clutter the timeline. Instead, the step's
	// OutputSummary captures the tool's status + last error, and any
	// artifacts (stdout/stderr/files) get linked.
	finishedAt := time.Now().UTC()
	step := types.TaskStep{
		ID:         spec.NewID("step"),
		TaskID:     spec.Task.ID,
		RunID:      spec.Run.ID,
		Index:      stepIndex,
		Kind:       "tool",
		Title:      fmt.Sprintf("%s (%s)", toolName, subResult.Status),
		Status:     subResult.Status,
		Phase:      "execution",
		Result:     resultFromStatus(subResult.Status),
		ToolName:   toolName,
		Input:      toolInputForLog(toolName, task),
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
	step.OutputSummary = map[string]any{
		"sub_status":     subResult.Status,
		"last_error":     subResult.LastError,
		"sub_step_count": len(subResult.Steps),
		"artifact_count": len(subResult.Artifacts),
	}

	// Re-stamp artifacts with the loop's step ID so the run UI groups
	// them under this turn rather than the sub-executor's step.
	artifacts := make([]types.TaskArtifact, 0, len(subResult.Artifacts))
	for _, art := range subResult.Artifacts {
		art.StepID = step.ID
		artifacts = append(artifacts, art)
	}

	// What the LLM sees on the next turn. We summarize for token
	// efficiency: include status, error if any, and a digest of the
	// stdout/file content. Full artifacts are still in the run for
	// the UI; the LLM gets the relevant signal.
	resultText := summarizeSubResult(subResult)
	return resultText, &step, artifacts, nil
}

// runWithoutLLM is the failure path: agent_loop tasks REQUIRE an LLM
// client. Without one we emit a single failed step with an actionable
// error so the operator sees the cause in the run output and knows
// to wire a model. Operators who want deterministic shell/git/file
// execution should use those execution kinds directly.
func (e *AgentLoopExecutor) runWithoutLLM(_ context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	startedAt := spec.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	const errMsg = "agent_loop requires an LLM client — configure a provider and restart, or use execution_kind=shell/git/file for deterministic tasks"
	step := types.TaskStep{
		ID:         spec.NewID("step"),
		TaskID:     spec.Task.ID,
		RunID:      spec.Run.ID,
		Index:      1,
		Kind:       "model",
		Title:      "Agent loop unavailable",
		Status:     "failed",
		Phase:      "planning",
		Result:     telemetry.ResultError,
		ToolName:   "builtin.agent_loop",
		Error:      errMsg,
		StartedAt:  startedAt,
		FinishedAt: startedAt,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
	if err := upsertTaskStep(spec, step); err != nil {
		return nil, err
	}
	return &ExecutionResult{
		Status:            "failed",
		Steps:             []types.TaskStep{step},
		LastError:         errMsg,
		OtelStatusCode:    "error",
		OtelStatusMessage: errMsg,
	}, nil
}

// failedFromError appends a synthetic "agent loop failed" step that
// carries the error message as its output. Returns a "failed"
// ExecutionResult ready for the runner.
func (e *AgentLoopExecutor) failedFromError(spec ExecutionSpec, allSteps []types.TaskStep, allArtifacts []types.TaskArtifact, stepIndex int, startedAt time.Time, msg string) (*ExecutionResult, error) {
	step := types.TaskStep{
		ID:         spec.NewID("step"),
		TaskID:     spec.Task.ID,
		RunID:      spec.Run.ID,
		Index:      stepIndex,
		Kind:       "model",
		Title:      "Agent loop failed",
		Status:     "failed",
		Phase:      "execution",
		Result:     telemetry.ResultError,
		ToolName:   "builtin.agent_loop",
		Error:      msg,
		StartedAt:  startedAt,
		FinishedAt: time.Now().UTC(),
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
	if err := upsertTaskStep(spec, step); err != nil {
		return nil, err
	}
	allSteps = append(allSteps, step)
	return &ExecutionResult{
		Status:            "failed",
		Steps:             allSteps,
		Artifacts:         allArtifacts,
		LastError:         msg,
		OtelStatusCode:    "error",
		OtelStatusMessage: msg,
	}, nil
}

// ─── Tool definitions ────────────────────────────────────────────────

// agentToolDefinitions returns the OpenAI tool-call format the loop
// passes to the LLM each turn. Names match the dispatch switch in
// dispatchToolCall(). Schemas are JSON Schema 2020-12, kept minimal
// because verbose schemas waste tokens.
func agentToolDefinitions() []types.Tool {
	return []types.Tool{
		{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "shell_exec",
				Description: "Execute a shell command in the task workspace. Use for any inspection or computation that doesn't write a file.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"command": {"type": "string", "description": "The shell command to run, e.g. 'ls -la' or 'cat README.md'."},
						"working_directory": {"type": "string", "description": "Optional subdirectory under the workspace. Empty = workspace root."}
					},
					"required": ["command"]
				}`),
			},
		},
		{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "git_exec",
				Description: "Run a git command in the task workspace. Use for inspecting history, status, diffs.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"command": {"type": "string", "description": "The git subcommand and args, e.g. 'status' or 'log --oneline -5'."},
						"working_directory": {"type": "string", "description": "Optional subdirectory under the workspace."}
					},
					"required": ["command"]
				}`),
			},
		},
		{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "file_write",
				Description: "Write or append to a file in the task workspace. Use to produce deliverables or update existing files.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"path": {"type": "string", "description": "Relative path under the workspace, e.g. 'output.txt' or 'src/main.py'."},
						"content": {"type": "string", "description": "The full content to write (for write) or to append (for append)."},
						"operation": {"type": "string", "enum": ["write", "append"], "default": "write", "description": "write replaces the file; append adds to the end."}
					},
					"required": ["path", "content"]
				}`),
			},
		},
	}
}

type shellExecArgs struct {
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory,omitempty"`
}

type gitExecArgs struct {
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory,omitempty"`
}

type fileWriteArgs struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Operation string `json:"operation,omitempty"`
}

// ─── Helpers ────────────────────────────────────────────────────────

func buildThinkingStep(spec ExecutionSpec, index, turn int, startedAt time.Time, msg types.Message, resp *types.ChatResponse) types.TaskStep {
	toolNames := make([]string, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		toolNames = append(toolNames, tc.Function.Name)
	}
	model := ""
	if resp != nil {
		model = resp.Model
	}
	return types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    index,
		Kind:     "model",
		Title:    fmt.Sprintf("Agent turn %d", turn),
		Status:   "completed",
		Phase:    "thinking",
		Result:   telemetry.ResultSuccess,
		ToolName: "builtin.agent_loop_llm",
		Input: map[string]any{
			"turn":  turn,
			"model": model,
		},
		OutputSummary: map[string]any{
			"content_chars": len(msg.Content),
			"tool_calls":    toolNames,
			"finish_reason": finishReason(resp),
		},
		StartedAt:  startedAt,
		FinishedAt: time.Now().UTC(),
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
}

func buildFinalAnswerArtifact(spec ExecutionSpec, stepID string, startedAt time.Time, content string) types.TaskArtifact {
	return types.TaskArtifact{
		ID:          spec.NewID("artifact"),
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		StepID:      stepID,
		Kind:        "summary",
		Name:        "agent-final-answer.txt",
		Description: "Agent loop final answer",
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: content,
		SizeBytes:   int64(len(content)),
		Status:      "ready",
		CreatedAt:   startedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}
}

func toolInputForLog(name string, task types.Task) map[string]any {
	switch name {
	case "shell_exec":
		return map[string]any{"command": task.ShellCommand, "working_directory": task.WorkingDirectory}
	case "git_exec":
		return map[string]any{"command": task.GitCommand, "working_directory": task.WorkingDirectory}
	case "file_write":
		return map[string]any{"path": task.FilePath, "operation": task.FileOperation, "content_chars": len(task.FileContent)}
	}
	return nil
}

// summarizeSubResult builds the text the LLM sees as the tool result.
// We include status + last_error + a content digest (stdout for
// shell/git, the written path for file_write) — enough for the model
// to decide what to do next without bloating the next prompt.
//
// The token-efficiency trade-off: dumping full stdout would let the
// model "see" the file it just inspected, but pushes context cost up
// fast on a real task. Operators can ship a custom executor that
// summarizes more aggressively if they have specific token budgets.
func summarizeSubResult(r *ExecutionResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "status=%s", r.Status)
	if r.LastError != "" {
		fmt.Fprintf(&b, "\nerror=%s", r.LastError)
	}
	for _, art := range r.Artifacts {
		switch art.Kind {
		case "stdout", "stderr":
			content := art.ContentText
			if len(content) > 4000 {
				content = content[:4000] + "…(truncated)"
			}
			fmt.Fprintf(&b, "\n--- %s ---\n%s", art.Kind, content)
		case "file":
			fmt.Fprintf(&b, "\nwrote file: %s (%d bytes)", art.Name, art.SizeBytes)
		}
	}
	return b.String()
}

// resultFromStatus maps an executor's status string ("completed",
// "failed", etc.) to the telemetry result vocabulary
// ("success" / "error"). The telemetry package itself only knows
// success / denied / error, so we collapse the executor's richer
// status set into those buckets for the agent-loop step output.
func resultFromStatus(status string) string {
	switch status {
	case "completed":
		return telemetry.ResultSuccess
	case "failed", "cancelled":
		return telemetry.ResultError
	}
	return telemetry.ResultError
}

func finishReason(resp *types.ChatResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].FinishReason
}
