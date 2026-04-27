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
	llm        AgentLLMClient
	shell      Executor
	file       Executor
	git        Executor
	maxTurns   int
	gatedTools map[string]struct{}
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
//
// gatedTools is the set of tool names that require operator approval
// before execution (e.g. {"shell_exec", "git_exec"}). When the LLM
// asks for any tool in this set, the loop pauses, emits an approval
// record, and returns awaiting_approval. The runner persists the
// approval; when the operator approves, the same run is re-queued
// and the loop hydrates from the saved conversation, dispatches the
// previously-pending tool calls, and continues. Empty/nil = no gating
// (every tool runs immediately).
func NewAgentLoopExecutor(llm AgentLLMClient, shell Executor, file Executor, git Executor, maxTurns int, gatedTools []string) *AgentLoopExecutor {
	if maxTurns <= 0 {
		maxTurns = 8
	}
	gated := make(map[string]struct{}, len(gatedTools))
	for _, name := range gatedTools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		gated[name] = struct{}{}
	}
	return &AgentLoopExecutor{
		llm:        llm,
		shell:      shell,
		file:       file,
		git:        git,
		maxTurns:   maxTurns,
		gatedTools: gated,
	}
}

// isGated reports whether a tool call requires operator approval.
func (e *AgentLoopExecutor) isGated(toolName string) bool {
	if len(e.gatedTools) == 0 {
		return false
	}
	_, ok := e.gatedTools[toolName]
	return ok
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

	// Build the initial conversation. On resume, we hydrate from the
	// previous run's persisted conversation artifact so the agent
	// continues the exact dialogue rather than restarting from scratch
	// — preserves prior tool results, partial reasoning, and avoids
	// re-paying for tokens already spent. Fresh runs start with just
	// the user prompt.
	//
	// We don't currently inject a system prompt — the task's own
	// Prompt carries enough intent. Per-tenant system prompts are a
	// later add.
	messages := hydrateConversation(spec)
	tools := agentToolDefinitions()
	// Stable artifact ID for this run's conversation snapshot. Same
	// ID across turns means UpsertArtifact replaces the contents in
	// place rather than creating a new artifact each time, so the
	// run's artifact list stays clean.
	conversationArtifactID := "convo-" + spec.Run.ID

	finalResult := &ExecutionResult{
		Status:    "completed",
		Steps:     allSteps,
		Artifacts: allArtifacts,
	}

	// Resume detection: if the conversation tail is an assistant
	// message with tool_calls and no following tool messages, we're
	// resuming after operator approval. Dispatch the pending tool
	// calls before doing the next LLM turn — they were just approved.
	pendingToolCalls := pendingToolCallsForResume(messages)

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

		var assistantMsg types.Message
		var resp *types.ChatResponse
		turnStartedAt := time.Now().UTC()

		if len(pendingToolCalls) > 0 {
			// Skip the LLM call this turn — the assistant message is
			// already at the tail of `messages` (saved by the previous
			// run). Dispatch the approved tool calls and let the next
			// turn's LLM call reason over the results.
			assistantMsg = messages[len(messages)-1]
			thinkingStep := buildResumeThinkingStep(spec, nextIndex, turn, turnStartedAt, assistantMsg)
			nextIndex++
			if err := upsertTaskStep(spec, thinkingStep); err != nil {
				return nil, err
			}
			allSteps = append(allSteps, thinkingStep)
		} else {
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
			r, err := e.llm.Chat(ctx, req)
			if err != nil {
				return e.failedFromError(spec, allSteps, allArtifacts, nextIndex, turnStartedAt,
					fmt.Sprintf("LLM call failed on turn %d: %v", turn, err))
			}
			if r == nil || len(r.Choices) == 0 {
				return e.failedFromError(spec, allSteps, allArtifacts, nextIndex, turnStartedAt,
					fmt.Sprintf("LLM returned empty response on turn %d", turn))
			}
			resp = r
			assistantMsg = resp.Choices[0].Message

			// 2. Record this turn's "thinking" step — captures the
			// assistant message content + which tools it asked for.
			thinkingStep := buildThinkingStep(spec, nextIndex, turn, turnStartedAt, assistantMsg, resp)
			nextIndex++
			if err := upsertTaskStep(spec, thinkingStep); err != nil {
				return nil, err
			}
			allSteps = append(allSteps, thinkingStep)

			// 3. Append the assistant message to the running conversation.
			messages = append(messages, assistantMsg)
			// Persist snapshot — crash between LLM response and tool
			// dispatch still leaves a recoverable state.
			if art, err := upsertConversationArtifact(spec, conversationArtifactID, messages, turn, turnStartedAt); err != nil {
				return nil, err
			} else if art != nil && len(allArtifacts) == 0 {
				allArtifacts = append(allArtifacts, *art)
			}

			// 4. If no tool calls, assistant gave a final answer.
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

			// 4b. Approval gate. If any tool in this turn is gated,
			// pause the loop: persist conversation (already done),
			// emit an approval record covering all pending tool
			// calls, return awaiting_approval. The runner persists
			// the approval and stops the run; on operator approve,
			// the same run is re-queued and we re-enter the loop
			// with the same conversation tail — this branch is
			// short-circuited by the resume-detection above.
			gatedNames := e.gatedToolsInTurn(assistantMsg.ToolCalls)
			if len(gatedNames) > 0 {
				approval := buildApprovalForTurn(spec, turn, gatedNames, turnStartedAt)
				awaitingStep := buildAwaitingApprovalStep(spec, nextIndex, turn, turnStartedAt, approval)
				nextIndex++
				if err := upsertTaskStep(spec, awaitingStep); err != nil {
					return nil, err
				}
				allSteps = append(allSteps, awaitingStep)
				return &ExecutionResult{
					Status:           "awaiting_approval",
					Steps:            allSteps,
					Artifacts:        allArtifacts,
					PendingApprovals: []types.TaskApproval{approval},
					OtelStatusCode:   "ok",
				}, nil
			}
		}

		// 5. Dispatch each tool call in order.
		callsToRun := assistantMsg.ToolCalls
		for _, toolCall := range callsToRun {
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
			messages = append(messages, types.Message{
				Role:       "tool",
				Content:    toolResultText,
				ToolCallID: toolCall.ID,
			})
			_ = dispatchErr
		}
		// Snapshot after tool results.
		if _, err := upsertConversationArtifact(spec, conversationArtifactID, messages, turn, turnStartedAt); err != nil {
			return nil, err
		}
		// Resume mode is a one-shot — clear so subsequent turns hit
		// the LLM normally.
		pendingToolCalls = nil
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

// pendingToolCallsForResume detects the resume-after-approval state:
// the conversation tail is an assistant message with tool_calls and
// no subsequent tool-role results. Returns the list of tool calls
// that need dispatching. Empty slice = fresh turn (LLM call needed).
func pendingToolCallsForResume(messages []types.Message) []types.ToolCall {
	if len(messages) == 0 {
		return nil
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" || len(last.ToolCalls) == 0 {
		return nil
	}
	// Tool calls in the trailing assistant message exist; check that
	// none of them have already been resolved by a later tool message.
	// Since we just confirmed `last` is the tail, if tool messages
	// for these calls existed they'd be after `last` — they don't,
	// so all calls are pending.
	return last.ToolCalls
}

// gatedToolsInTurn returns the names of gated tools that appear in
// this turn's tool calls. Empty if no gating applies.
func (e *AgentLoopExecutor) gatedToolsInTurn(calls []types.ToolCall) []string {
	if len(e.gatedTools) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(calls))
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if !e.isGated(c.Function.Name) {
			continue
		}
		if _, dup := seen[c.Function.Name]; dup {
			continue
		}
		seen[c.Function.Name] = struct{}{}
		out = append(out, c.Function.Name)
	}
	return out
}

// buildApprovalForTurn constructs the approval record covering one
// or more gated tool calls in a turn. The reason text lists the tool
// names so the operator UI can render a clear "approve agent's use of
// shell_exec, git_exec" prompt without parsing the conversation.
func buildApprovalForTurn(spec ExecutionSpec, turn int, gatedNames []string, when time.Time) types.TaskApproval {
	return types.TaskApproval{
		ID:        spec.NewID("approval"),
		TaskID:    spec.Task.ID,
		RunID:     spec.Run.ID,
		Kind:      "agent_loop_tool_call",
		Status:    "pending",
		Reason:    fmt.Sprintf("Agent requested tools that require approval: %s", strings.Join(gatedNames, ", ")),
		CreatedAt: when,
		RequestID: spec.RequestID,
		TraceID:   spec.TraceID,
	}
}

// buildAwaitingApprovalStep is the timeline step the run UI shows
// while paused. Carries the approval id so the operator UI can link
// the step to the approval action.
func buildAwaitingApprovalStep(spec ExecutionSpec, index, turn int, when time.Time, approval types.TaskApproval) types.TaskStep {
	return types.TaskStep{
		ID:         spec.NewID("step"),
		TaskID:     spec.Task.ID,
		RunID:      spec.Run.ID,
		Index:      index,
		Kind:       "approval",
		Title:      fmt.Sprintf("Awaiting approval — turn %d", turn),
		Status:     "awaiting_approval",
		Phase:      "approval",
		Result:     telemetry.ResultSuccess,
		ToolName:   "builtin.agent_loop_approval",
		ApprovalID: approval.ID,
		Input: map[string]any{
			"turn":   turn,
			"reason": approval.Reason,
		},
		StartedAt:  when,
		FinishedAt: when,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
}

// buildResumeThinkingStep marks the timeline entry for a resumed turn
// (where we skip the LLM call because the assistant message was
// produced by the previous run). Lets the operator see in the run
// history that the agent didn't re-think — it just dispatched the
// approved calls.
func buildResumeThinkingStep(spec ExecutionSpec, index, turn int, when time.Time, msg types.Message) types.TaskStep {
	toolNames := make([]string, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		toolNames = append(toolNames, tc.Function.Name)
	}
	return types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    index,
		Kind:     "model",
		Title:    fmt.Sprintf("Agent turn %d (resumed after approval)", turn),
		Status:   "completed",
		Phase:    "thinking",
		Result:   telemetry.ResultSuccess,
		ToolName: "builtin.agent_loop_resume",
		Input: map[string]any{
			"turn":           turn,
			"resumed":        true,
			"tool_calls":     toolNames,
			"approved_tools": toolNames,
		},
		StartedAt:  when,
		FinishedAt: when,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
}

// hydrateConversation returns the conversation history for this run.
// On a fresh run, it's just the initial user prompt. On a resume, it's
// the JSON-decoded prior conversation from the source run's
// persisted agent_conversation artifact — the loop continues exactly
// where it left off, preserving tool results and assistant reasoning.
//
// If the resume artifact is missing or malformed (corrupt JSON, edited
// out of band) we fall back to the user-prompt-only state. That
// degrades gracefully: the agent re-plans rather than crashing.
func hydrateConversation(spec ExecutionSpec) []types.Message {
	freshStart := []types.Message{
		{Role: "user", Content: spec.Task.Prompt},
	}
	if spec.ResumeCheckpoint == nil || len(spec.ResumeCheckpoint.AgentConversation) == 0 {
		return freshStart
	}
	var saved []types.Message
	if err := json.Unmarshal(spec.ResumeCheckpoint.AgentConversation, &saved); err != nil {
		return freshStart
	}
	if len(saved) == 0 {
		return freshStart
	}
	return saved
}

// upsertConversationArtifact writes the current conversation snapshot
// to a stable artifact ID. Returns the artifact when it's newly
// created (or on the first call) so the caller can include it in the
// run's artifact list. Idempotent across turns: the same ID means the
// artifact's content is replaced in place rather than appended.
func upsertConversationArtifact(spec ExecutionSpec, id string, messages []types.Message, turn int, when time.Time) (*types.TaskArtifact, error) {
	if spec.UpsertArtifact == nil {
		return nil, nil
	}
	payload, err := json.Marshal(messages)
	if err != nil {
		// Marshal failures here are fatal — every Message field is
		// JSON-marshalable by construction; a failure would be a
		// runtime corruption we shouldn't paper over.
		return nil, fmt.Errorf("marshal agent conversation: %w", err)
	}
	art := types.TaskArtifact{
		ID:          id,
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		Kind:        "agent_conversation",
		Name:        "agent-conversation.json",
		Description: fmt.Sprintf("Agent loop conversation snapshot after turn %d", turn),
		MimeType:    "application/json",
		StorageKind: "inline",
		ContentText: string(payload),
		SizeBytes:   int64(len(payload)),
		Status:      "ready",
		CreatedAt:   when,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}
	if err := spec.UpsertArtifact(art); err != nil {
		return nil, err
	}
	return &art, nil
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
