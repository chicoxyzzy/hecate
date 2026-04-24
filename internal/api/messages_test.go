package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

func TestMessagesNonStreamTranslatesRequestAndResponse(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID:        "chatcmpl-msgs-1",
			Model:     "gpt-4o-mini-2024-07-18",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices: []types.ChatChoice{{
				Index: 0,
				Message: types.Message{
					Role:    "assistant",
					Content: "Hello, human.",
				},
				FinishReason: "stop",
			}},
			Usage: types.Usage{PromptTokens: 12, CompletionTokens: 4, TotalTokens: 16},
		},
	}

	handler := newTestHTTPHandler(logger, provider)

	body := `{
		"model": "gpt-4o-mini",
		"max_tokens": 128,
		"system": "You are terse.",
		"messages": [
			{"role": "user", "content": "Hi."}
		]
	}`

	recorder := performRequest(t, handler, http.MethodPost, "/v1/messages", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", recorder.Code, recorder.Body.String())
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if got := recorder.Header().Get("X-Runtime-Provider"); got != "openai" {
		t.Fatalf("X-Runtime-Provider = %q, want openai", got)
	}

	var resp AnthropicMessagesResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.Type != "message" {
		t.Fatalf("type = %q, want message", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Fatalf("role = %q, want assistant", resp.Role)
	}
	if resp.Model != "gpt-4o-mini-2024-07-18" {
		t.Fatalf("model = %q, want resolved model", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", resp.StopReason)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v, want input=12 output=4", resp.Usage)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "text" || resp.Content[0].Text != "Hello, human." {
		t.Fatalf("content = %+v, want single text block", resp.Content)
	}
}

func TestMessagesSystemBlockArrayAndStructuredMessages(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// Capture the request that reaches the provider to assert the system
	// prompt and structured tool_result content were correctly normalised.
	var captured types.ChatRequest
	provider := &recordingProvider{
		fakeProvider: fakeProvider{
			name: "openai",
			response: &types.ChatResponse{
				ID:        "chatcmpl-msgs-2",
				Model:     "gpt-4o-mini",
				CreatedAt: time.Unix(1_700_000_001, 0).UTC(),
				Choices: []types.ChatChoice{{
					Index: 0,
					Message: types.Message{
						Role:    "assistant",
						Content: "Tool complete.",
					},
					FinishReason: "length",
				}},
				Usage: types.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
			},
		},
		captured: &captured,
	}

	handler := newTestHTTPHandler(logger, provider)

	body := `{
		"model": "gpt-4o-mini",
		"max_tokens": 32,
		"system": [
			{"type": "text", "text": "Act as a helpful assistant."},
			{"type": "text", "text": "Answer briefly."}
		],
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "What is 2+2?"}
			]},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "toolu_1", "name": "calc", "input": {"a": 2, "b": 2}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": [
					{"type": "text", "text": "4"}
				]}
			]}
		]
	}`

	recorder := performRequest(t, handler, http.MethodPost, "/v1/messages", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", recorder.Code, recorder.Body.String())
	}

	var resp AnthropicMessagesResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.StopReason != "max_tokens" {
		t.Fatalf("stop_reason = %q, want max_tokens", resp.StopReason)
	}

	// Assert the normalised request routed to the provider has a merged system
	// message and a tool-role message carrying the flattened tool_result text.
	if len(captured.Messages) < 4 {
		t.Fatalf("captured messages count = %d, want >=4, got=%+v", len(captured.Messages), captured.Messages)
	}
	if captured.Messages[0].Role != "system" {
		t.Fatalf("messages[0].role = %q, want system", captured.Messages[0].Role)
	}
	if !strings.Contains(captured.Messages[0].Content, "Act as a helpful assistant.") ||
		!strings.Contains(captured.Messages[0].Content, "Answer briefly.") {
		t.Fatalf("system message content = %q, want merged system blocks", captured.Messages[0].Content)
	}
	// Find the tool message.
	var toolMsg *types.Message
	for i := range captured.Messages {
		if captured.Messages[i].Role == "tool" {
			toolMsg = &captured.Messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatalf("no tool-role message in captured messages: %+v", captured.Messages)
	}
	if toolMsg.ToolCallID != "toolu_1" {
		t.Fatalf("tool_call_id = %q, want toolu_1", toolMsg.ToolCallID)
	}
	if !strings.Contains(toolMsg.Content, "4") {
		t.Fatalf("tool content = %q, want to contain 4", toolMsg.Content)
	}
}

func TestMessagesRejectsMissingMaxTokens(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{name: "openai", response: &types.ChatResponse{}}
	handler := newTestHTTPHandler(logger, provider)

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	recorder := performRequest(t, handler, http.MethodPost, "/v1/messages", body)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestTranslateOpenAIToAnthropicSSE(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`data: {"id":"chatcmpl-x","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-x","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"Hel"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-x","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-x","model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var buf bytes.Buffer
	if err := translateOpenAIToAnthropicSSE(context.Background(), "gpt-4o-mini", "gpt-4o-mini", strings.NewReader(input), &buf); err != nil {
		t.Fatalf("translateOpenAIToAnthropicSSE() error = %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"event: message_start",
		"event: content_block_start",
		`"type":"text"`,
		"event: content_block_delta",
		`"type":"text_delta"`,
		`"text":"Hel"`,
		`"text":"lo"`,
		"event: content_block_stop",
		"event: message_delta",
		`"stop_reason":"end_turn"`,
		`"output_tokens":2`,
		"event: message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in stream output:\n%s", want, out)
		}
	}
}

// recordingProvider wraps fakeProvider and captures the last request.
type recordingProvider struct {
	fakeProvider
	captured *types.ChatRequest
}

func (p *recordingProvider) Chat(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	if p.captured != nil {
		*p.captured = req
	}
	return p.fakeProvider.Chat(ctx, req)
}

