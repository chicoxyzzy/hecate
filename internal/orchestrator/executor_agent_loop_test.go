package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

// scriptedLLM returns a canned response on each call. Tests build the
// script in advance — { "turn 1 wants shell_exec(ls)", "turn 2 wants
// final answer" } — and the loop drives through it. Each call records
// what messages it received so we can assert the conversation grew
// correctly.
type scriptedLLM struct {
	responses []*types.ChatResponse
	calls     atomic.Int32
	lastReqs  []types.ChatRequest
}

func (s *scriptedLLM) Chat(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	idx := int(s.calls.Load())
	s.calls.Add(1)
	s.lastReqs = append(s.lastReqs, req)
	if idx >= len(s.responses) {
		return nil, errors.New("scriptedLLM: ran out of canned responses")
	}
	return s.responses[idx], nil
}

// stubExecutor records what task it was asked to run and returns a
// canned ExecutionResult. Saves us from spinning up a real shell
// sandbox in unit tests.
type stubExecutor struct {
	calls  []types.Task
	result *ExecutionResult
}

func (s *stubExecutor) Execute(_ context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	s.calls = append(s.calls, spec.Task)
	if s.result != nil {
		return s.result, nil
	}
	return &ExecutionResult{Status: "completed"}, nil
}

func makeAssistantMsg(content string, calls ...types.ToolCall) types.Message {
	return types.Message{Role: "assistant", Content: content, ToolCalls: calls}
}

func makeChatResp(msg types.Message) *types.ChatResponse {
	return &types.ChatResponse{
		Choices: []types.ChatChoice{{Message: msg, FinishReason: "stop"}},
	}
}

func newAgentLoopSpec(t *testing.T) ExecutionSpec {
	t.Helper()
	var counter atomic.Int32
	return ExecutionSpec{
		Task: types.Task{
			ID:     "task-1",
			Prompt: "summarize the working directory",
			Tenant: "team-a",
		},
		Run: types.TaskRun{
			ID:    "run-1",
			Model: "gpt-4o-mini",
		},
		StartedAt: time.Now().UTC(),
		NewID: func(prefix string) string {
			counter.Add(1)
			return fmt.Sprintf("%s-%d", prefix, counter.Load())
		},
		UpsertStep:     func(types.TaskStep) error { return nil },
		UpsertArtifact: func(types.TaskArtifact) error { return nil },
	}
}

func TestAgentLoop_FinalAnswerOnFirstTurn(t *testing.T) {
	// Simplest happy path: assistant answers immediately, no tool
	// calls. Loop should produce one thinking step + one final-answer
	// artifact and return completed.
	llm := &scriptedLLM{
		responses: []*types.ChatResponse{
			makeChatResp(makeAssistantMsg("The working directory contains a README.")),
		},
	}
	loop := NewAgentLoopExecutor(llm, &stubExecutor{}, &stubExecutor{}, &stubExecutor{}, 8)
	res, err := loop.Execute(context.Background(), newAgentLoopSpec(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("Status = %q, want completed", res.Status)
	}
	if len(res.Steps) != 1 {
		t.Errorf("Steps = %d, want 1 (just the thinking step)", len(res.Steps))
	}
	if len(res.Artifacts) != 1 || res.Artifacts[0].Name != "agent-final-answer.txt" {
		t.Errorf("Artifacts = %+v, want one final-answer artifact", res.Artifacts)
	}
	if !strings.Contains(res.Artifacts[0].ContentText, "README") {
		t.Errorf("final answer content lost: %s", res.Artifacts[0].ContentText)
	}
}

func TestAgentLoop_ToolCallThenAnswer(t *testing.T) {
	// Realistic two-turn flow: LLM calls shell_exec, gets the result,
	// then produces a final answer. Asserts the dispatched task
	// carries the right command and that the second LLM request sees
	// the tool result in its conversation history.
	shell := &stubExecutor{
		result: &ExecutionResult{
			Status: "completed",
			Artifacts: []types.TaskArtifact{
				{Kind: "stdout", Name: "stdout.txt", ContentText: "README.md\nmain.go\n"},
			},
		},
	}
	llm := &scriptedLLM{
		responses: []*types.ChatResponse{
			makeChatResp(makeAssistantMsg("", types.ToolCall{
				ID:   "call-1",
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      "shell_exec",
					Arguments: `{"command":"ls"}`,
				},
			})),
			makeChatResp(makeAssistantMsg("Two files: README.md and main.go.")),
		},
	}
	loop := NewAgentLoopExecutor(llm, shell, &stubExecutor{}, &stubExecutor{}, 8)
	res, err := loop.Execute(context.Background(), newAgentLoopSpec(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("Status = %q, want completed", res.Status)
	}
	if len(shell.calls) != 1 || shell.calls[0].ShellCommand != "ls" {
		t.Errorf("shell tool calls: %+v, want one call with command='ls'", shell.calls)
	}
	// Steps: thinking-1 + tool-1 + thinking-2 = 3
	if len(res.Steps) != 3 {
		t.Errorf("Steps = %d, want 3 (thinking + tool + thinking)", len(res.Steps))
	}
	// Second LLM request must have seen the tool result.
	if len(llm.lastReqs) != 2 {
		t.Fatalf("LLM call count = %d, want 2", len(llm.lastReqs))
	}
	secondReq := llm.lastReqs[1]
	foundToolMsg := false
	for _, m := range secondReq.Messages {
		if m.Role == "tool" && m.ToolCallID == "call-1" && strings.Contains(m.Content, "README.md") {
			foundToolMsg = true
		}
	}
	if !foundToolMsg {
		t.Errorf("second LLM request missing tool-role message; got: %+v", secondReq.Messages)
	}
}

func TestAgentLoop_MaxTurnsHonored(t *testing.T) {
	// LLM keeps asking for tool calls forever; loop must stop at
	// maxTurns and return failed status. Without this cap a runaway
	// agent could exhaust the model budget.
	loopingResponse := makeChatResp(makeAssistantMsg("", types.ToolCall{
		ID: "call-x", Type: "function",
		Function: types.ToolCallFunction{Name: "shell_exec", Arguments: `{"command":"ls"}`},
	}))
	llm := &scriptedLLM{}
	for i := 0; i < 20; i++ {
		llm.responses = append(llm.responses, loopingResponse)
	}
	loop := NewAgentLoopExecutor(llm, &stubExecutor{}, &stubExecutor{}, &stubExecutor{}, 3)
	res, err := loop.Execute(context.Background(), newAgentLoopSpec(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed (max turns)", res.Status)
	}
	if !strings.Contains(res.LastError, "maxTurns=3") {
		t.Errorf("LastError = %q, want mention of maxTurns=3", res.LastError)
	}
	if got := llm.calls.Load(); got != 3 {
		t.Errorf("LLM calls = %d, want 3 (capped)", got)
	}
}

func TestAgentLoop_LLMErrorBubbles(t *testing.T) {
	// LLM call fails → loop produces a "failed" step and returns
	// failed status. The error message must reach the run output so
	// the operator can diagnose.
	llm := &scriptedLLM{} // empty responses → returns error on first call
	loop := NewAgentLoopExecutor(llm, &stubExecutor{}, &stubExecutor{}, &stubExecutor{}, 8)
	res, err := loop.Execute(context.Background(), newAgentLoopSpec(t))
	if err != nil {
		t.Fatalf("Execute (should not return Go-level error): %v", err)
	}
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed", res.Status)
	}
	if !strings.Contains(res.LastError, "LLM call failed") {
		t.Errorf("LastError = %q, want 'LLM call failed'", res.LastError)
	}
}

func TestAgentLoop_NoLLM_FailsWithActionableError(t *testing.T) {
	// agent_loop without an LLM is a misconfiguration, not a use case.
	// The loop must surface a clear error so the operator knows to
	// wire a model rather than seeing a confusing silent success.
	loop := NewAgentLoopExecutor(nil, &stubExecutor{}, &stubExecutor{}, &stubExecutor{}, 8)
	res, err := loop.Execute(context.Background(), newAgentLoopSpec(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Status != "failed" {
		t.Errorf("Status = %q, want failed", res.Status)
	}
	if !strings.Contains(res.LastError, "requires an LLM") {
		t.Errorf("LastError = %q, want mention of 'requires an LLM'", res.LastError)
	}
}

func TestAgentLoop_BadToolArgsBecomeToolError(t *testing.T) {
	// Malformed tool arguments must NOT crash the loop or become a
	// Go error — the LLM should see the parse error as its tool
	// result and decide what to do. Then on the next turn we provide
	// a valid answer to terminate the loop.
	llm := &scriptedLLM{
		responses: []*types.ChatResponse{
			makeChatResp(makeAssistantMsg("", types.ToolCall{
				ID: "call-1", Type: "function",
				Function: types.ToolCallFunction{Name: "shell_exec", Arguments: `not json`},
			})),
			makeChatResp(makeAssistantMsg("I gave up.")),
		},
	}
	shell := &stubExecutor{}
	loop := NewAgentLoopExecutor(llm, shell, &stubExecutor{}, &stubExecutor{}, 8)
	res, err := loop.Execute(context.Background(), newAgentLoopSpec(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Status != "completed" {
		t.Errorf("Status = %q, want completed", res.Status)
	}
	// The shell executor should NOT have been called — args were
	// invalid, the dispatcher returned an error string instead of
	// running the tool.
	if len(shell.calls) != 0 {
		t.Errorf("shell tool was called despite bad args: %+v", shell.calls)
	}
	// The second LLM request should have a tool-role message
	// describing the parse failure.
	secondReq := llm.lastReqs[1]
	hasParseError := false
	for _, m := range secondReq.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "invalid arguments") {
			hasParseError = true
		}
	}
	if !hasParseError {
		t.Errorf("expected parse-error tool message in conversation; got: %+v", secondReq.Messages)
	}
}

func TestAgentLoop_UnknownToolBecomesToolError(t *testing.T) {
	// LLM hallucinates a tool name; loop must report it as a tool
	// failure rather than crashing the run.
	llm := &scriptedLLM{
		responses: []*types.ChatResponse{
			makeChatResp(makeAssistantMsg("", types.ToolCall{
				ID: "call-1", Type: "function",
				Function: types.ToolCallFunction{Name: "fly_to_moon", Arguments: `{}`},
			})),
			makeChatResp(makeAssistantMsg("Sorry, I can't.")),
		},
	}
	loop := NewAgentLoopExecutor(llm, &stubExecutor{}, &stubExecutor{}, &stubExecutor{}, 8)
	res, err := loop.Execute(context.Background(), newAgentLoopSpec(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Status != "completed" {
		t.Errorf("Status = %q, want completed", res.Status)
	}
	// Tool message must carry the "unknown tool" hint.
	secondReq := llm.lastReqs[1]
	hasUnknown := false
	for _, m := range secondReq.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "unknown tool") {
			hasUnknown = true
		}
	}
	if !hasUnknown {
		t.Errorf("expected unknown-tool tool message; got: %+v", secondReq.Messages)
	}
}

func TestAgentLoop_ContextCancellation(t *testing.T) {
	// If the run is cancelled mid-loop (operator hits Cancel, gateway
	// shuts down), the loop must exit cleanly with cancelled status.
	llm := &scriptedLLM{
		responses: []*types.ChatResponse{
			makeChatResp(makeAssistantMsg("", types.ToolCall{
				ID: "call-1", Type: "function",
				Function: types.ToolCallFunction{Name: "shell_exec", Arguments: `{"command":"ls"}`},
			})),
		},
	}
	loop := NewAgentLoopExecutor(llm, &stubExecutor{}, &stubExecutor{}, &stubExecutor{}, 8)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	res, err := loop.Execute(ctx, newAgentLoopSpec(t))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Status != "cancelled" {
		t.Errorf("Status = %q, want cancelled", res.Status)
	}
}
