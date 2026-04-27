package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
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
	httpPolicy HTTPRequestPolicy
	httpClient *http.Client
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
func NewAgentLoopExecutor(llm AgentLLMClient, shell Executor, file Executor, git Executor, maxTurns int, gatedTools []string, httpPolicy HTTPRequestPolicy) *AgentLoopExecutor {
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
	// Apply safe defaults to the HTTP policy. Operators who don't
	// configure GATEWAY_TASK_HTTP_* still get sensible bounds.
	if httpPolicy.Timeout <= 0 {
		httpPolicy.Timeout = 30 * time.Second
	}
	if httpPolicy.MaxResponseBytes <= 0 {
		httpPolicy.MaxResponseBytes = 256 * 1024
	}
	// Dedicated client per executor so the timeout is enforced and
	// connections are pooled. We don't enable redirects-following
	// past 10 (Go's default) — agents that get stuck redirect-looping
	// blow through their max-turns cap before causing damage.
	httpClient := &http.Client{Timeout: httpPolicy.Timeout}

	return &AgentLoopExecutor{
		llm:        llm,
		shell:      shell,
		file:       file,
		git:        git,
		maxTurns:   maxTurns,
		gatedTools: gated,
		httpPolicy: httpPolicy,
		httpClient: httpClient,
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

	// Per-task cost ceiling. spec.Task.BudgetMicrosUSD acts as a hard
	// cap on the cumulative LLM spend for this *task* (across the
	// entire resume chain), not just this run. Zero/negative disables
	// the cap. We accumulate ChatResponse.Cost.TotalMicrosUSD after
	// each turn and bail when (priorCost + costSpent) crosses the
	// ceiling. Without priorCost the operator could escape the
	// ceiling by repeatedly resuming a maxed-out run; including it
	// here keeps the ceiling meaningful across the chain.
	costCeiling := spec.Task.BudgetMicrosUSD
	costSpent := int64(0)
	priorCost := int64(0)
	if spec.ResumeCheckpoint != nil {
		priorCost = spec.ResumeCheckpoint.PriorCostMicrosUSD
		// Same-run mid-approval resume: seed costSpent with the
		// pre-pause spend so ceiling checks and the persisted Total
		// account for it. Cross-run resumes see zero here (new run
		// hasn't spent anything yet).
		costSpent = spec.ResumeCheckpoint.ThisRunCostMicrosUSD
	}
	turnCosts := make([]TurnCostRecord, 0, e.maxTurns)

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
			finalResult.CostMicrosUSD = costSpent
			finalResult.TurnCosts = turnCosts
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
				failed, ferr := e.failedFromError(spec, allSteps, allArtifacts, nextIndex, turnStartedAt,
					fmt.Sprintf("LLM call failed on turn %d: %v", turn, err))
				if failed != nil {
					failed.CostMicrosUSD = costSpent
					failed.TurnCosts = turnCosts
				}
				return failed, ferr
			}
			if r == nil || len(r.Choices) == 0 {
				failed, ferr := e.failedFromError(spec, allSteps, allArtifacts, nextIndex, turnStartedAt,
					fmt.Sprintf("LLM returned empty response on turn %d", turn))
				if failed != nil {
					failed.CostMicrosUSD = costSpent
					failed.TurnCosts = turnCosts
				}
				return failed, ferr
			}
			resp = r
			// Accumulate the LLM cost for this turn. Even when the
			// per-task ceiling is disabled we surface the running
			// total via ExecutionResult so the runner can persist
			// per-run cost telemetry. CachedInputMicrosUSD is folded
			// into TotalMicrosUSD upstream (see CostBreakdown), so
			// using Total directly accounts correctly for cache hits.
			turnCost := resp.Cost.TotalMicrosUSD
			costSpent += turnCost
			assistantMsg = resp.Choices[0].Message

			// 2. Record this turn's "thinking" step — captures the
			// assistant message content + which tools it asked for,
			// plus the per-turn LLM cost in OutputSummary so the run
			// replay UI can render cost next to the turn label
			// without joining against the events feed.
			thinkingStep := buildThinkingStep(spec, nextIndex, turn, turnStartedAt, assistantMsg, resp, costSpent)
			nextIndex++
			if err := upsertTaskStep(spec, thinkingStep); err != nil {
				return nil, err
			}
			allSteps = append(allSteps, thinkingStep)

			// Per-turn cost record. We surface this on ExecutionResult
			// so the runner can emit one `agent.turn.completed` event
			// per turn for replay/operator UIs. CumulativeMicrosUSD is
			// this-run-only; the runner adds priorCost when emitting.
			turnCosts = append(turnCosts, TurnCostRecord{
				Turn:                turn,
				StepID:              thinkingStep.ID,
				CostMicrosUSD:       turnCost,
				CumulativeMicrosUSD: costSpent,
				ToolCallCount:       len(assistantMsg.ToolCalls),
			})

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
				finalResult.CostMicrosUSD = costSpent
				finalResult.TurnCosts = turnCosts
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
					CostMicrosUSD:    costSpent,
					TurnCosts:        turnCosts,
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

		// Per-task cost ceiling check. We do this AFTER the turn is
		// fully recorded (assistant message + tool results in the
		// conversation snapshot) so the operator sees what was paid
		// for. The ceiling is task-cumulative — priorCost (spend in
		// earlier runs of the resume chain) plus costSpent (this
		// run). Crossing the ceiling marks the run failed with an
		// actionable error; future turns don't fire. Operators can
		// raise the ceiling and resume to continue.
		if costCeiling > 0 && (priorCost+costSpent) >= costCeiling {
			msg := fmt.Sprintf("agent loop hit per-task cost ceiling: spent %d µUSD this run + %d µUSD prior = %d µUSD, ceiling %d µUSD", costSpent, priorCost, priorCost+costSpent, costCeiling)
			finalResult.Status = "failed"
			finalResult.LastError = msg
			finalResult.OtelStatusCode = "error"
			finalResult.OtelStatusMessage = "cost_ceiling_exceeded"
			finalResult.Steps = allSteps
			finalResult.Artifacts = allArtifacts
			finalResult.CostMicrosUSD = costSpent
			finalResult.TurnCosts = turnCosts
			return finalResult, nil
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
	finalResult.CostMicrosUSD = costSpent
	finalResult.TurnCosts = turnCosts
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

	case "read_file":
		var args readFileArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return fmt.Sprintf("invalid arguments for read_file: %v", err), nil, nil, nil
		}
		return readFileTool(spec, args, stepIndex, startedAt, call.Function.Name)

	case "list_dir":
		var args listDirArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return fmt.Sprintf("invalid arguments for list_dir: %v", err), nil, nil, nil
		}
		return listDirTool(spec, args, stepIndex, startedAt, call.Function.Name)

	case "http_request":
		var args httpRequestArgs
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return fmt.Sprintf("invalid arguments for http_request: %v", err), nil, nil, nil
		}
		return e.httpRequestTool(ctx, spec, args, stepIndex, startedAt, call.Function.Name)

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
		{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "read_file",
				Description: "Read the contents of a file in the task workspace. Use this instead of `shell_exec(cat ...)` — it's faster, doesn't need a shell, and isn't gated by approval.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"path": {"type": "string", "description": "Relative path under the workspace."},
						"max_bytes": {"type": "integer", "minimum": 1, "maximum": 1048576, "default": 65536, "description": "Cap the read to this many bytes. Larger files are truncated; the truncation is reported in the result."}
					},
					"required": ["path"]
				}`),
			},
		},
		{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "list_dir",
				Description: "List files and directories under a workspace path. Use this instead of `shell_exec(ls ...)` for a structured listing that includes file sizes and types.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"path": {"type": "string", "default": ".", "description": "Relative path under the workspace. '.' or empty = workspace root."}
					}
				}`),
			},
		},
		{
			Type: "function",
			Function: types.ToolFunction{
				Name:        "http_request",
				Description: "Make an outbound HTTP(S) request. Use for fetching URLs, calling external APIs, or posting to webhooks. Response body is capped to keep prompts cheap; private IPs and unsafe schemes are blocked unless the operator opts in.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"url": {"type": "string", "description": "Absolute http:// or https:// URL."},
						"method": {"type": "string", "enum": ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"], "default": "GET"},
						"headers": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Optional request headers as a flat object."},
						"body": {"type": "string", "description": "Optional request body. For JSON APIs, set Content-Type explicitly via headers."}
					},
					"required": ["url"]
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

type readFileArgs struct {
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type listDirArgs struct {
	Path string `json:"path,omitempty"`
}

type httpRequestArgs struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// ─── Helpers ────────────────────────────────────────────────────────

func buildThinkingStep(spec ExecutionSpec, index, turn int, startedAt time.Time, msg types.Message, resp *types.ChatResponse, runCumulativeMicrosUSD int64) types.TaskStep {
	toolNames := make([]string, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		toolNames = append(toolNames, tc.Function.Name)
	}
	model := ""
	turnCost := int64(0)
	if resp != nil {
		model = resp.Model
		turnCost = resp.Cost.TotalMicrosUSD
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
		// cost_micros_usd is this turn's LLM spend; the UI renders
		// it next to the turn label in the conversation viewer so
		// operators see cost in context. run_cumulative_cost_micros_usd
		// is the running total for this run only — task-level
		// cumulative (including prior runs in the resume chain) lives
		// on the run cost badge in the header. Both numbers serialize
		// as JSON ints; clients should treat absent/zero as "no LLM
		// cost was attributed" (e.g. resumed-after-approval steps
		// emitted via buildResumeThinkingStep).
		OutputSummary: map[string]any{
			"content_chars":                  len(msg.Content),
			"tool_calls":                     toolNames,
			"finish_reason":                  finishReason(resp),
			"cost_micros_usd":                turnCost,
			"run_cumulative_cost_micros_usd": runCumulativeMicrosUSD,
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

// ─── Inline read tools ──────────────────────────────────────────────
//
// `read_file` and `list_dir` are deliberately implemented inline here
// rather than going through the FileExecutor. They're read-only,
// don't need a sandbox, and the LLM hits them frequently — keeping
// them off the executor path saves goroutine + sandbox overhead, and
// makes them naturally exempt from the approval gate (read-only is
// always safe).
//
// Path safety: every relative path is resolved against the workspace
// root and rejected if the result would land outside. This is the
// same protection a sandbox would provide; we do it explicitly here
// because we're bypassing the sandbox.

const (
	readFileDefaultMaxBytes = 64 * 1024
	readFileHardCapBytes    = 1024 * 1024
	listDirEntryCap         = 500
)

// resolveWorkspacePath joins relPath onto the run's workspace root and
// rejects the result if it escapes. Returns the absolute path (safe
// to read) or an error message suitable for the tool result.
func resolveWorkspacePath(spec ExecutionSpec, relPath string) (string, string) {
	root := strings.TrimSpace(spec.Task.WorkingDirectory)
	if root == "" {
		// No workspace configured — operate from current dir as a
		// permissive fallback for tests. In production runner sets
		// this to the run's WorkspacePath before dispatching.
		root, _ = os.Getwd()
	}
	rel := strings.TrimSpace(relPath)
	if rel == "" || rel == "." {
		return root, ""
	}
	// Reject absolute paths outright — agent must operate inside the
	// workspace. Path-traversal via `..` is caught below by the prefix
	// check on the cleaned absolute path.
	if filepath.IsAbs(rel) {
		return "", fmt.Sprintf("path must be relative to the workspace, got absolute: %q", rel)
	}
	abs := filepath.Clean(filepath.Join(root, rel))
	rootClean := filepath.Clean(root)
	if abs != rootClean && !strings.HasPrefix(abs, rootClean+string(filepath.Separator)) {
		return "", fmt.Sprintf("path %q escapes the workspace root", rel)
	}
	return abs, ""
}

// readFileTool reads a workspace file and returns the content as the
// tool result text. Bounded by max_bytes; binary files are reported
// rather than dumped (to avoid pushing garbage into the conversation).
func readFileTool(spec ExecutionSpec, args readFileArgs, stepIndex int, startedAt time.Time, toolName string) (string, *types.TaskStep, []types.TaskArtifact, error) {
	abs, errMsg := resolveWorkspacePath(spec, args.Path)
	if errMsg != "" {
		return errMsg, nil, nil, nil
	}
	maxBytes := args.MaxBytes
	if maxBytes <= 0 {
		maxBytes = readFileDefaultMaxBytes
	}
	if maxBytes > readFileHardCapBytes {
		maxBytes = readFileHardCapBytes
	}

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("read_file: %v", err), nil, nil, nil
	}
	if info.IsDir() {
		return fmt.Sprintf("read_file: %q is a directory; use list_dir instead", args.Path), nil, nil, nil
	}

	f, err := os.Open(abs)
	if err != nil {
		return fmt.Sprintf("read_file: %v", err), nil, nil, nil
	}
	defer f.Close()

	buf := make([]byte, maxBytes)
	n, _ := f.Read(buf)
	content := buf[:n]
	truncated := info.Size() > int64(n)

	// Crude but effective binary detection: if any of the first 512
	// bytes is a NUL, treat as binary and don't return content. The
	// LLM doesn't benefit from raw binary in its conversation.
	probe := content
	if len(probe) > 512 {
		probe = probe[:512]
	}
	for _, b := range probe {
		if b == 0 {
			return fmt.Sprintf("read_file: %q is a binary file (%d bytes); skipped content. Use file_write to overwrite or shell_exec for inspection.", args.Path, info.Size()), nil, nil, nil
		}
	}

	step := buildReadFileStep(spec, stepIndex, startedAt, toolName, args.Path, info.Size(), int64(n), truncated)
	var b strings.Builder
	fmt.Fprintf(&b, "path=%s size=%d bytes=%d", args.Path, info.Size(), n)
	if truncated {
		fmt.Fprintf(&b, " truncated=true")
	}
	b.WriteString("\n--- content ---\n")
	b.Write(content)
	if truncated {
		b.WriteString("\n…(truncated)")
	}
	return b.String(), &step, nil, nil
}

// listDirTool lists a workspace directory. Returns one line per entry
// with kind (file/dir/link) and size. Capped at listDirEntryCap so
// huge directories don't bloat the conversation.
func listDirTool(spec ExecutionSpec, args listDirArgs, stepIndex int, startedAt time.Time, toolName string) (string, *types.TaskStep, []types.TaskArtifact, error) {
	abs, errMsg := resolveWorkspacePath(spec, args.Path)
	if errMsg != "" {
		return errMsg, nil, nil, nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("list_dir: %v", err), nil, nil, nil
	}
	if !info.IsDir() {
		return fmt.Sprintf("list_dir: %q is not a directory", args.Path), nil, nil, nil
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Sprintf("list_dir: %v", err), nil, nil, nil
	}
	// Sort for deterministic output — saves token churn across
	// equivalent calls in different turns.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	relPath := args.Path
	if relPath == "" {
		relPath = "."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "path=%s entries=%d", relPath, len(entries))
	if len(entries) > listDirEntryCap {
		fmt.Fprintf(&b, " truncated=%d", listDirEntryCap)
	}
	b.WriteString("\n")
	emitted := 0
	for _, entry := range entries {
		if emitted >= listDirEntryCap {
			break
		}
		kind := "file"
		size := int64(0)
		if entry.IsDir() {
			kind = "dir"
		} else if entry.Type()&os.ModeSymlink != 0 {
			kind = "link"
		}
		if fi, err := entry.Info(); err == nil && !fi.IsDir() {
			size = fi.Size()
		}
		fmt.Fprintf(&b, "%-4s %10d  %s\n", kind, size, entry.Name())
		emitted++
	}

	step := buildListDirStep(spec, stepIndex, startedAt, toolName, relPath, len(entries))
	return b.String(), &step, nil, nil
}

func buildReadFileStep(spec ExecutionSpec, index int, startedAt time.Time, toolName, path string, fileSize, readBytes int64, truncated bool) types.TaskStep {
	return types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    index,
		Kind:     "tool",
		Title:    fmt.Sprintf("read_file %s", path),
		Status:   "completed",
		Phase:    "execution",
		Result:   telemetry.ResultSuccess,
		ToolName: toolName,
		Input: map[string]any{
			"path":      path,
			"size":      fileSize,
			"truncated": truncated,
		},
		OutputSummary: map[string]any{
			"bytes_read": readBytes,
		},
		StartedAt:  startedAt,
		FinishedAt: time.Now().UTC(),
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
}

func buildListDirStep(spec ExecutionSpec, index int, startedAt time.Time, toolName, path string, entryCount int) types.TaskStep {
	return types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    index,
		Kind:     "tool",
		Title:    fmt.Sprintf("list_dir %s", path),
		Status:   "completed",
		Phase:    "execution",
		Result:   telemetry.ResultSuccess,
		ToolName: toolName,
		Input: map[string]any{
			"path": path,
		},
		OutputSummary: map[string]any{
			"entry_count": entryCount,
		},
		StartedAt:  startedAt,
		FinishedAt: time.Now().UTC(),
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
}

// ─── http_request tool ──────────────────────────────────────────────
//
// The HTTP tool is the agent's only outbound-network surface. It runs
// through e.httpClient (constructed once at executor init with the
// configured timeout) and applies three layers of safety:
//
//   1. Scheme allowlist — only http/https. file://, ftp://, gopher://
//      etc. are rejected outright.
//   2. SSRF guard — by default any host that resolves to a loopback,
//      private, or link-local IP is blocked (cf. RFC 1918 / 4193 /
//      6890). Operators flip GATEWAY_TASK_HTTP_ALLOW_PRIVATE_IPS=true
//      to permit this; useful for agents that hit the gateway's own
//      admin API or a sidecar service.
//   3. Hostname allowlist — when GATEWAY_TASK_HTTP_ALLOWED_HOSTS is
//      set, only those exact host names are reachable. Subdomains are
//      NOT inferred (api.openai.com vs openai.com) — operators write
//      what they mean.
//
// Response body is capped to MaxResponseBytes to keep prompts cheap.
// Truncation is reported in the tool result so the agent can ask for
// more if needed (e.g. via a follow-up call with a Range header).

func (e *AgentLoopExecutor) httpRequestTool(ctx context.Context, spec ExecutionSpec, args httpRequestArgs, stepIndex int, startedAt time.Time, toolName string) (string, *types.TaskStep, []types.TaskArtifact, error) {
	method := strings.ToUpper(strings.TrimSpace(args.Method))
	if method == "" {
		method = "GET"
	}
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
	default:
		return fmt.Sprintf("http_request: unsupported method %q", method), nil, nil, nil
	}

	parsed, err := url.Parse(strings.TrimSpace(args.URL))
	if err != nil {
		return fmt.Sprintf("http_request: invalid URL: %v", err), nil, nil, nil
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Sprintf("http_request: scheme %q is not allowed; use http or https", parsed.Scheme), nil, nil, nil
	}
	host := parsed.Hostname()
	if host == "" {
		return "http_request: URL has no host", nil, nil, nil
	}

	// Hostname allowlist — exact match only.
	if len(e.httpPolicy.AllowedHosts) > 0 {
		ok := false
		for _, h := range e.httpPolicy.AllowedHosts {
			if strings.EqualFold(strings.TrimSpace(h), host) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Sprintf("http_request: host %q is not in the configured allowlist", host), nil, nil, nil
		}
	}

	// SSRF guard. Block loopback / private / link-local unless the
	// operator opted in. We resolve the host and check every address
	// — a hostname like `internal.example.com` could legitimately
	// resolve to 10.0.0.5, and we want to catch that, not just
	// literal IPs in the URL.
	if !e.httpPolicy.AllowPrivateIPs {
		if msg := checkPublicHost(ctx, host); msg != "" {
			return msg, nil, nil, nil
		}
	}

	var body io.Reader
	if args.Body != "" {
		body = strings.NewReader(args.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return fmt.Sprintf("http_request: build request: %v", err), nil, nil, nil
	}
	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Sprintf("http_request: %v", err), nil, nil, nil
	}
	defer resp.Body.Close()

	max := e.httpPolicy.MaxResponseBytes
	limited := io.LimitReader(resp.Body, int64(max)+1) // +1 to detect overflow
	raw, _ := io.ReadAll(limited)
	truncated := false
	if len(raw) > max {
		raw = raw[:max]
		truncated = true
	}

	step := buildHTTPRequestStep(spec, stepIndex, startedAt, toolName, method, parsed.String(), resp.StatusCode, len(raw), truncated)

	var b strings.Builder
	fmt.Fprintf(&b, "status=%d url=%s bytes=%d", resp.StatusCode, parsed.String(), len(raw))
	if truncated {
		fmt.Fprintf(&b, " truncated=true")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		fmt.Fprintf(&b, " content_type=%s", ct)
	}
	b.WriteString("\n--- body ---\n")
	b.Write(raw)
	if truncated {
		fmt.Fprintf(&b, "\n…(truncated at %d bytes; configure GATEWAY_TASK_HTTP_MAX_RESPONSE_BYTES to widen)", max)
	}
	return b.String(), &step, nil, nil
}

// checkPublicHost returns an error message string if any of the
// host's resolved IPs falls in a blocked range. Empty string = safe.
//
// We resolve via net.DefaultResolver (DNS) explicitly here rather
// than relying on the http client's transport, because we want to
// inspect the IPs BEFORE the connection happens. A cleaner long-term
// solution wraps net.Dialer.Control with the same check (which also
// catches DNS rebinding). For v0.1 this is enough.
func checkPublicHost(ctx context.Context, host string) string {
	// Literal IP shortcut.
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Sprintf("http_request: target IP %s is private/loopback/link-local; set GATEWAY_TASK_HTTP_ALLOW_PRIVATE_IPS=true to permit", ip)
		}
		return ""
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Sprintf("http_request: dns lookup failed: %v", err)
	}
	for _, a := range addrs {
		if isBlockedIP(a.IP) {
			return fmt.Sprintf("http_request: host %s resolves to a private/loopback/link-local address (%s); set GATEWAY_TASK_HTTP_ALLOW_PRIVATE_IPS=true to permit", host, a.IP)
		}
	}
	return ""
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

func buildHTTPRequestStep(spec ExecutionSpec, index int, startedAt time.Time, toolName, method, urlStr string, status, bytesRead int, truncated bool) types.TaskStep {
	return types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    index,
		Kind:     "tool",
		Title:    fmt.Sprintf("%s %s", method, urlStr),
		Status:   "completed",
		Phase:    "execution",
		Result:   telemetry.ResultSuccess,
		ToolName: toolName,
		Input: map[string]any{
			"method": method,
			"url":    urlStr,
		},
		OutputSummary: map[string]any{
			"status":     status,
			"bytes_read": bytesRead,
			"truncated":  truncated,
		},
		StartedAt:  startedAt,
		FinishedAt: time.Now().UTC(),
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
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

// countAssistantTurns returns the number of assistant messages in the
// saved conversation. Each agent_loop turn produces exactly one
// assistant message (with tool_calls or a final answer), so the count
// equals the number of completed turns. Used by the retry-from-turn-N
// codepath to validate the requested turn lies within range.
func countAssistantTurns(messages []types.Message) int {
	n := 0
	for _, m := range messages {
		if m.Role == "assistant" {
			n++
		}
	}
	return n
}

// truncateConversationToTurn drops the Nth assistant message and
// everything that follows it, so the next LLM call re-issues turn N
// against the same prior context. The system message (if present) and
// the user prompt are preserved, as are any prior assistant turns and
// their tool results — the operator gets to explore an alternative
// path from turn N forward.
//
// turn must be >= 1 and <= countAssistantTurns(messages). turn=1
// truncates back to just the prelude (system + user); turn=N for the
// final turn drops only that turn's assistant message.
//
// Returns a fresh slice; the input is not modified.
func truncateConversationToTurn(messages []types.Message, turn int) ([]types.Message, error) {
	if turn < 1 {
		return nil, fmt.Errorf("turn must be >= 1, got %d", turn)
	}
	assistantSeen := 0
	cutIndex := -1
	for i, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		assistantSeen++
		if assistantSeen == turn {
			cutIndex = i
			break
		}
	}
	if cutIndex == -1 {
		return nil, fmt.Errorf("turn %d not found: conversation has %d assistant turn(s)", turn, assistantSeen)
	}
	out := make([]types.Message, cutIndex)
	copy(out, messages[:cutIndex])
	return out, nil
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
// On a fresh run, it prepends the composed system prompt (from the
// runner's four-layer resolver) before the user prompt. On a resume,
// it returns the JSON-decoded prior conversation from the source
// run's persisted agent_conversation artifact — the loop continues
// exactly where it left off, preserving tool results, prior reasoning,
// AND the original system prompt (it's already in the saved message
// array; we don't re-compose).
//
// If the resume artifact is missing or malformed (corrupt JSON, edited
// out of band) we fall back to the fresh-start state. That degrades
// gracefully: the agent re-plans rather than crashing.
func hydrateConversation(spec ExecutionSpec) []types.Message {
	if spec.ResumeCheckpoint != nil && len(spec.ResumeCheckpoint.AgentConversation) > 0 {
		var saved []types.Message
		if err := json.Unmarshal(spec.ResumeCheckpoint.AgentConversation, &saved); err == nil && len(saved) > 0 {
			return saved
		}
	}
	// Fresh run: optionally prepend the composed system prompt.
	// Empty SystemPrompt means none of the four layers contributed
	// (operator hasn't configured anything, no tenant prompt, no
	// CLAUDE.md/AGENTS.md, no per-task prompt) — skip the system
	// message entirely rather than emit an empty one.
	messages := make([]types.Message, 0, 2)
	if strings.TrimSpace(spec.SystemPrompt) != "" {
		messages = append(messages, types.Message{Role: "system", Content: spec.SystemPrompt})
	}
	messages = append(messages, types.Message{Role: "user", Content: spec.Task.Prompt})
	return messages
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
