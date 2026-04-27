package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

// TestChatCompletionsRejectsMalformedJSON locks in the JSON-decode boundary:
// a body the standard library can't parse should produce a 400 with the
// invalid_request error code, never a 500. decodeJSON is shared with
// every other JSON endpoint, so this also documents the contract for them.
func TestChatCompletionsRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{name: "openai", response: &types.ChatResponse{}}
	handler := newTestHTTPHandler(logger, provider)

	rec := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","messages":[`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Error.Type != "invalid_request" {
		t.Errorf("error.type = %q, want invalid_request", payload.Error.Type)
	}
}

// TestChatCompletionsDeniedReturns403WithUserFacingMessage exercises the
// errDenied → 403 mapping and verifies the body has the "request denied: "
// classification prefix stripped (UserFacingMessage). Without this, the
// chat UI shows operators a confusing "request denied: requests are
// disabled by policy" string where the prefix is internal noise.
func TestChatCompletionsDeniedReturns403WithUserFacingMessage(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{name: "openai", response: &types.ChatResponse{}}

	// DenyAll trips the governor.Check path that wraps with errDenied.
	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Governor: config.GovernorConfig{DenyAll: true},
	})

	rec := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if strings.HasPrefix(payload.Error.Message, "request denied: ") {
		t.Errorf("error.message = %q, want classification prefix stripped", payload.Error.Message)
	}
	if !strings.Contains(payload.Error.Message, "disabled by policy") {
		t.Errorf("error.message = %q, want underlying reason to be visible", payload.Error.Message)
	}
}

// TestChatCompletionsRateLimitedReturns429 covers the rate_limit path
// from the chat endpoint end-to-end. The unit test for checkRateLimit
// only exercises the helper in isolation; this proves the limiter is
// actually wired into the handler chain in front of HandleChat.
func TestChatCompletionsRateLimitedReturns429(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID: "chatcmpl-rl", Model: "gpt-4o-mini",
			Choices: []types.ChatChoice{{Message: types.Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		},
	}
	// burst=1, RPM=60 → first request consumes the token, second returns 429.
	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Server: config.ServerConfig{
			RateLimit: config.RateLimitConfig{Enabled: true, RequestsPerMinute: 60, BurstSize: 1},
		},
	})

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	first := performJSONRequest(t, handler, body)
	if first.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200; body=%s", first.Code, first.Body.String())
	}
	second := performJSONRequest(t, handler, body)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429; body=%s", second.Code, second.Body.String())
	}
	if got := second.Header().Get("X-RateLimit-Limit"); got != "1" {
		t.Errorf("X-RateLimit-Limit = %q, want \"1\" (the bucket capacity)", got)
	}
}

// TestChatCompletionsStreamRouteRejectsBudgetExceeded is the streaming
// counterpart to TestHandleChatReturns402OnBudgetExceeded. RouteForStream
// is a separate code path from non-stream HandleChat, with its own
// error-classification block. Crucially: when budget is exceeded, the
// handler must NOT commit to SSE — it returns a JSON 402 instead. A
// regression that flips the order (writing SSE headers before the
// budget check) would break the operator-facing error UX.
func TestChatCompletionsStreamRouteRejectsBudgetExceeded(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &sseStreamingProvider{fakeProvider: fakeProvider{
		name: "openai", defaultModel: "gpt-4o-mini",
		response: &types.ChatResponse{ID: "chatcmpl-stream", Model: "gpt-4o-mini",
			Choices: []types.ChatChoice{{Message: types.Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		},
	}}

	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Governor: config.GovernorConfig{
			MaxTotalBudgetMicros:    1,
			MaxPromptTokens:         100_000,
			BudgetWarningThresholds: []int{50, 80, 95},
			BudgetHistoryLimit:      20,
		},
	})

	rec := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","stream":true,"max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json (route-time error must not commit to SSE)", ct)
	}
	if strings.Contains(rec.Body.String(), "data: ") {
		t.Errorf("body contains SSE framing; want plain JSON. body=%s", rec.Body.String())
	}
}

// failingStreamProvider implements ChatStream by returning a fixed error
// without writing any bytes — used to drive the mid-stream error path.
type failingStreamProvider struct {
	fakeProvider
	streamErr error
}

func (p *failingStreamProvider) ChatStream(_ context.Context, _ types.ChatRequest, _ io.Writer) error {
	return p.streamErr
}

// Compile-time assertions match the existing sseStreamingProvider pattern.
var (
	_ providers.Streamer = (*failingStreamProvider)(nil)
	_ providers.Provider = (*failingStreamProvider)(nil)
)

// TestChatCompletionsStreamMidStreamErrorEmitsSSEErrorEvent locks in the
// terminal SSE error format the handler writes when ChatStream errors
// after headers have already been committed. Headers are 200 OK and SSE
// Content-Type — the operator's SDK has no way to read a status code
// at this point, so the only signal is the embedded `data:` event with
// a JSON error and a final `data: [DONE]` to close the stream.
func TestChatCompletionsStreamMidStreamErrorEmitsSSEErrorEvent(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &failingStreamProvider{
		fakeProvider: fakeProvider{name: "openai", defaultModel: "gpt-4o-mini"},
		streamErr: &providers.UpstreamError{
			StatusCode: http.StatusBadGateway,
			Message:    "upstream connection reset",
			Type:       "server_error",
		},
	}
	handler := newTestHTTPHandler(logger, provider)

	rec := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (mid-stream errors keep the headers we already sent); body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"error"`) || !strings.Contains(body, "upstream connection reset") {
		t.Errorf("body missing SSE error event; got=%q", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("body missing terminal [DONE]; got=%q", body)
	}
}

// TestChatCompletionsRequiresAuthWhenConfigured guards the auth gate.
// Without an AuthToken the handler is open by default (test fixtures
// rely on this); flipping AuthToken on must produce a 401 for an
// unauthenticated request, so an operator who configures auth doesn't
// silently get an open gateway.
func TestChatCompletionsRequiresAuthWhenConfigured(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{name: "openai", response: &types.ChatResponse{}}
	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Server: config.ServerConfig{AuthToken: "admin-secret"},
	})

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	rec := performJSONRequest(t, handler, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Error.Type != "unauthorized" {
		t.Errorf("error.type = %q, want unauthorized", payload.Error.Type)
	}
}
