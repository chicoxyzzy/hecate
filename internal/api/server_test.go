package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/auth"
	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/cache"
	"github.com/hecate/agent-runtime/internal/catalog"
	"github.com/hecate/agent-runtime/internal/chatstate"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/retention"
	"github.com/hecate/agent-runtime/internal/router"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestChatCompletionsCachesResponsesAndReturnsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	provider := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID:        "chatcmpl-123",
			Model:     "gpt-4o-mini-2024-07-18",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices: []types.ChatChoice{
				{
					Index: 0,
					Message: types.Message{
						Role:    "assistant",
						Content: "Hello!",
					},
					FinishReason: "stop",
				},
			},
			Usage: types.Usage{
				PromptTokens:     14,
				CompletionTokens: 2,
				TotalTokens:      16,
			},
		},
	}

	handler := newTestHTTPHandler(logger, provider)
	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Say hello in one short sentence."}]}`

	first := performJSONRequest(t, handler, body)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d, body=%s", first.Code, http.StatusOK, first.Body.String())
	}
	if got := first.Header().Get("X-Runtime-Cache"); got != "false" {
		t.Fatalf("first X-Runtime-Cache = %q, want false", got)
	}
	if got := first.Header().Get("X-Runtime-Provider"); got != "openai" {
		t.Fatalf("X-Runtime-Provider = %q, want openai", got)
	}
	if got := first.Header().Get("X-Runtime-Provider-Kind"); got != "cloud" {
		t.Fatalf("X-Runtime-Provider-Kind = %q, want cloud", got)
	}
	if got := first.Header().Get("X-Runtime-Requested-Model"); got != "gpt-4o-mini" {
		t.Fatalf("X-Runtime-Requested-Model = %q, want gpt-4o-mini", got)
	}
	if got := first.Header().Get("X-Runtime-Requested-Model-Canonical"); got != "gpt-4o-mini" {
		t.Fatalf("X-Runtime-Requested-Model-Canonical = %q, want gpt-4o-mini", got)
	}
	if got := first.Header().Get("X-Runtime-Model"); got != "gpt-4o-mini-2024-07-18" {
		t.Fatalf("X-Runtime-Model = %q, want dated model", got)
	}
	if got := first.Header().Get("X-Runtime-Model-Canonical"); got != "gpt-4o-mini" {
		t.Fatalf("X-Runtime-Model-Canonical = %q, want gpt-4o-mini", got)
	}
	if got := first.Header().Get("X-Runtime-Cost-USD"); got != "0.000003" {
		t.Fatalf("X-Runtime-Cost-USD = %q, want 0.000003", got)
	}
	if got := first.Header().Get("X-Request-Id"); got == "" {
		t.Fatal("X-Request-Id = empty, want generated request id")
	}
	if got := first.Header().Get("X-Trace-Id"); got == "" {
		t.Fatal("X-Trace-Id = empty, want trace id")
	}
	if got := first.Header().Get("X-Span-Id"); got == "" {
		t.Fatal("X-Span-Id = empty, want span id")
	}

	second := performJSONRequest(t, handler, body)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d, body=%s", second.Code, http.StatusOK, second.Body.String())
	}
	if got := second.Header().Get("X-Runtime-Cache"); got != "true" {
		t.Fatalf("second X-Runtime-Cache = %q, want true", got)
	}
	if got := second.Header().Get("X-Runtime-Cache-Type"); got != "exact" {
		t.Fatalf("second X-Runtime-Cache-Type = %q, want exact", got)
	}

	if provider.CallCount() != 1 {
		t.Fatalf("provider call count = %d, want 1 due to cache hit", provider.CallCount())
	}

	var response OpenAIChatCompletionResponse
	if err := json.NewDecoder(bytes.NewReader(second.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Model != "gpt-4o-mini-2024-07-18" {
		t.Fatalf("response model = %q, want resolved model", response.Model)
	}
	if response.Choices[0].Message.Content != "Hello!" {
		t.Fatalf("response content = %q, want Hello!", response.Choices[0].Message.Content)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, `"msg":"gen_ai.gateway.request"`) {
		t.Fatalf("log output missing gen_ai.gateway.request entry: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"gen_ai.request.model":"gpt-4o-mini"`) {
		t.Fatalf("log output missing gen_ai.request.model: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"hecate.model.requested_canonical":"gpt-4o-mini"`) {
		t.Fatalf("log output missing hecate.model.requested_canonical: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"gen_ai.response.model":"gpt-4o-mini-2024-07-18"`) {
		t.Fatalf("log output missing gen_ai.response.model: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"hecate.model.resolved_canonical":"gpt-4o-mini"`) {
		t.Fatalf("log output missing hecate.model.resolved_canonical: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"hecate.cache.hit":true`) {
		t.Fatalf("log output missing hecate.cache.hit true entry: %s", logOutput)
	}
}

func TestChatCompletionsSemanticCacheHitsSimilarPrompt(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "ollama",
		capabilities: providers.Capabilities{
			Name:         "ollama",
			Kind:         providers.KindLocal,
			DefaultModel: "llama3.1:8b",
			Models:       []string{"llama3.1:8b"},
		},
		response: &types.ChatResponse{
			ID:        "chatcmpl-local-1",
			Model:     "llama3.1:8b",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices: []types.ChatChoice{{
				Index: 0,
				Message: types.Message{
					Role:    "assistant",
					Content: "Channels coordinate goroutines.",
				},
				FinishReason: "stop",
			}},
			Usage: types.Usage{PromptTokens: 20, CompletionTokens: 4, TotalTokens: 24},
		},
	}

	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Cache: config.CacheConfig{
			Semantic: config.SemanticCacheConfig{
				Enabled:       true,
				Backend:       "memory",
				DefaultTTL:    time.Hour,
				MinSimilarity: 0.6,
				MaxEntries:    100,
				MaxTextChars:  2048,
			},
		},
	})

	first := performJSONRequest(t, handler, `{"model":"llama3.1:8b","messages":[{"role":"user","content":"Explain Go channels and goroutines."}]}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d, body=%s", first.Code, http.StatusOK, first.Body.String())
	}
	second := performJSONRequest(t, handler, `{"model":"llama3.1:8b","messages":[{"role":"user","content":"Explain goroutines and channels in Go."}]}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d, body=%s", second.Code, http.StatusOK, second.Body.String())
	}
	if got := second.Header().Get("X-Runtime-Cache"); got != "true" {
		t.Fatalf("second X-Runtime-Cache = %q, want true", got)
	}
	if got := second.Header().Get("X-Runtime-Cache-Type"); got != "semantic" {
		t.Fatalf("second X-Runtime-Cache-Type = %q, want semantic", got)
	}
	if got := second.Header().Get("X-Runtime-Semantic-Strategy"); got != "memory_scan" {
		t.Fatalf("second X-Runtime-Semantic-Strategy = %q, want memory_scan", got)
	}
	if got := second.Header().Get("X-Runtime-Semantic-Similarity"); got == "" {
		t.Fatal("second X-Runtime-Semantic-Similarity = empty, want value")
	}
	if provider.CallCount() != 1 {
		t.Fatalf("provider call count = %d, want 1 due to semantic cache hit", provider.CallCount())
	}
}

func TestChatCompletionsExactCacheIsolatedByUserScope(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID:        "chatcmpl-tenant",
			Model:     "gpt-4o-mini",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices:   []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "Hello!"}, FinishReason: "stop"}},
			Usage:     types.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		},
	}

	handler := newTestHTTPHandler(logger, provider)

	first := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","user":"team-a","messages":[{"role":"user","content":"Say hello."}]}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d, body=%s", first.Code, http.StatusOK, first.Body.String())
	}
	second := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","user":"team-b","messages":[{"role":"user","content":"Say hello."}]}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d, body=%s", second.Code, http.StatusOK, second.Body.String())
	}
	if got := second.Header().Get("X-Runtime-Cache"); got != "false" {
		t.Fatalf("second X-Runtime-Cache = %q, want false due to user isolation", got)
	}
	if provider.CallCount() != 2 {
		t.Fatalf("provider call count = %d, want 2 due to isolated cache scope", provider.CallCount())
	}
}

func TestRetentionRunAndListEndpointsPersistHistory(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID:        "chatcmpl-123",
			Model:     "gpt-4o-mini",
			CreatedAt: time.Now().UTC(),
			Choices:   []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "Hello!"}, FinishReason: "stop"}},
			Usage:     types.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		},
	}

	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-secret",
		},
	})

	runRecorder := httptest.NewRecorder()
	runRequest := httptest.NewRequest(http.MethodPost, "/admin/retention/run", strings.NewReader(`{"subsystems":["trace_snapshots"]}`))
	runRequest.Header.Set("Content-Type", "application/json")
	runRequest.Header.Set("Authorization", "Bearer admin-secret")
	handler.ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run status = %d, want %d, body=%s", runRecorder.Code, http.StatusOK, runRecorder.Body.String())
	}

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/admin/retention/runs?limit=5", nil)
	listRequest.Header.Set("Authorization", "Bearer admin-secret")
	handler.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body=%s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}

	var response RetentionRunsResponse
	if err := json.NewDecoder(bytes.NewReader(listRecorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(response.Data) != 1 {
		t.Fatalf("retention runs = %d, want 1", len(response.Data))
	}
	if response.Data[0].Trigger != "manual" {
		t.Fatalf("trigger = %q, want manual", response.Data[0].Trigger)
	}
	if response.Data[0].Actor == "" {
		t.Fatal("actor = empty, want populated admin actor")
	}
}

func TestChatCompletionsExactCacheIsolatedByExplicitProvider(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	openAI := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID:        "chatcmpl-openai",
			Model:     "gpt-4o-mini",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices:   []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "cloud"}, FinishReason: "stop"}},
			Usage:     types.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		},
	}
	anthropic := &fakeProvider{
		name: "anthropic",
		response: &types.ChatResponse{
			ID:        "chatcmpl-anthropic",
			Model:     "gpt-4o-mini",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices:   []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "other cloud"}, FinishReason: "stop"}},
			Usage:     types.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		},
	}

	handler := newTestHTTPHandlerForProviders(logger, []providers.Provider{openAI, anthropic}, config.Config{
		Router: config.RouterConfig{
			DefaultProvider: "openai",
			DefaultModel:    "gpt-4o-mini",
			Strategy:        "explicit_or_default",
		},
	})

	first := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","provider":"openai","messages":[{"role":"user","content":"Say hello."}]}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d, body=%s", first.Code, http.StatusOK, first.Body.String())
	}
	second := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","provider":"anthropic","messages":[{"role":"user","content":"Say hello."}]}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d, body=%s", second.Code, http.StatusOK, second.Body.String())
	}
	if got := second.Header().Get("X-Runtime-Cache"); got != "false" {
		t.Fatalf("second X-Runtime-Cache = %q, want false due to provider isolation", got)
	}
	if got := second.Header().Get("X-Runtime-Provider"); got != "anthropic" {
		t.Fatalf("second X-Runtime-Provider = %q, want anthropic", got)
	}
	if openAI.CallCount() != 1 {
		t.Fatalf("openai call count = %d, want 1", openAI.CallCount())
	}
	if anthropic.CallCount() != 1 {
		t.Fatalf("anthropic call count = %d, want 1", anthropic.CallCount())
	}
}

func TestChatCompletionsMapsUpstreamErrors(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		err: &providers.UpstreamError{
			StatusCode: http.StatusTooManyRequests,
			Message:    "rate limit exceeded",
			Type:       "rate_limit_error",
		},
	}

	handler := newTestHTTPHandler(logger, provider)
	response := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`)

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusTooManyRequests, response.Body.String())
	}

	var payload map[string]map[string]any
	if err := json.NewDecoder(bytes.NewReader(response.Body.Bytes())).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload["error"]["type"] != "upstream_error" {
		t.Fatalf("error type = %#v, want upstream_error", payload["error"]["type"])
	}
	if payload["error"]["message"] != "rate limit exceeded" {
		t.Fatalf("error message = %#v, want rate limit exceeded", payload["error"]["message"])
	}
}

func TestTraceEndpointReturnsRecordedRequestTimeline(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID:        "chatcmpl-123",
			Model:     "gpt-4o-mini",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices: []types.ChatChoice{{
				Index: 0,
				Message: types.Message{
					Role:    "assistant",
					Content: "Hello!",
				},
				FinishReason: "stop",
			}},
			Usage: types.Usage{
				PromptTokens:     14,
				CompletionTokens: 2,
				TotalTokens:      16,
			},
		},
	}

	handler := newTestHTTPHandler(logger, provider)
	chat := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`)
	if chat.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want %d, body=%s", chat.Code, http.StatusOK, chat.Body.String())
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/v1/traces?request_id="+chat.Header().Get("X-Request-Id"), nil)
	traceResp := httptest.NewRecorder()
	handler.ServeHTTP(traceResp, traceReq)

	if traceResp.Code != http.StatusOK {
		t.Fatalf("trace status = %d, want %d, body=%s", traceResp.Code, http.StatusOK, traceResp.Body.String())
	}

	var payload TraceResponse
	if err := json.NewDecoder(bytes.NewReader(traceResp.Body.Bytes())).Decode(&payload); err != nil {
		t.Fatalf("trace Decode() error = %v", err)
	}
	if payload.Object != "trace" {
		t.Fatalf("object = %q, want trace", payload.Object)
	}
	if payload.Data.RequestID == "" {
		t.Fatal("request_id = empty, want request id")
	}
	if payload.Data.TraceID == "" {
		t.Fatal("trace_id = empty, want trace id")
	}
	if len(payload.Data.Spans) == 0 {
		t.Fatal("spans = empty, want span list")
	}
	if payload.Data.Spans[0].Name != "gateway.request" {
		t.Fatalf("first span = %q, want gateway.request", payload.Data.Spans[0].Name)
	}
	if payload.Data.Spans[0].Attributes[telemetry.AttrServiceName] != "hecate-gateway" {
		t.Fatalf("root span %s = %#v, want hecate-gateway", telemetry.AttrServiceName, payload.Data.Spans[0].Attributes[telemetry.AttrServiceName])
	}
	foundResponseSpan := false
	for _, span := range payload.Data.Spans {
		if span.Name == "gateway.response" {
			foundResponseSpan = true
			break
		}
	}
	if !foundResponseSpan {
		t.Fatalf("missing gateway.response span: %#v", payload.Data.Spans)
	}
}

func TestChatCompletionsRetriesTransientProviderFailure(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		errSequence: []error{
			&providers.UpstreamError{StatusCode: http.StatusServiceUnavailable, Message: "temporary outage", Type: "server_error"},
			nil,
		},
		response: &types.ChatResponse{
			ID:        "chatcmpl-retry",
			Model:     "gpt-4o-mini",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices: []types.ChatChoice{{
				Index: 0,
				Message: types.Message{
					Role:    "assistant",
					Content: "Recovered after retry.",
				},
				FinishReason: "stop",
			}},
			Usage: types.Usage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14},
		},
	}

	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Provider: config.ProviderConfig{
			MaxAttempts:     2,
			RetryBackoff:    time.Millisecond,
			FailoverEnabled: true,
		},
	})
	response := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("X-Runtime-Attempts"); got != "2" {
		t.Fatalf("X-Runtime-Attempts = %q, want 2", got)
	}
	if got := response.Header().Get("X-Runtime-Retries"); got != "1" {
		t.Fatalf("X-Runtime-Retries = %q, want 1", got)
	}
	if got := response.Header().Get("X-Runtime-Fallback-From"); got != "" {
		t.Fatalf("X-Runtime-Fallback-From = %q, want empty", got)
	}
	if provider.CallCount() != 2 {
		t.Fatalf("provider call count = %d, want 2", provider.CallCount())
	}
}

func TestChatCompletionsFailsOverToConfiguredProvider(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	localProvider := &fakeProvider{
		name:         "ollama",
		defaultModel: "llama3.1:8b",
		capabilities: providers.Capabilities{
			Name:         "ollama",
			Kind:         providers.KindLocal,
			DefaultModel: "llama3.1:8b",
			Models:       []string{"llama3.1:8b"},
		},
		errSequence: []error{
			&providers.UpstreamError{StatusCode: http.StatusBadGateway, Message: "local runtime unavailable", Type: "server_error"},
		},
		response: &types.ChatResponse{
			ID:        "chatcmpl-local",
			Model:     "llama3.1:8b",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices:   []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "local"}, FinishReason: "stop"}},
			Usage:     types.Usage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14},
		},
	}
	cloudProvider := &fakeProvider{
		name:         "openai",
		defaultModel: "gpt-4o-mini",
		capabilities: providers.Capabilities{
			Name:         "openai",
			Kind:         providers.KindCloud,
			DefaultModel: "gpt-4o-mini",
			Models:       []string{"gpt-4o-mini"},
		},
		response: &types.ChatResponse{
			ID:        "chatcmpl-cloud",
			Model:     "gpt-4o-mini",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices:   []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "cloud fallback"}, FinishReason: "stop"}},
			Usage:     types.Usage{PromptTokens: 12, CompletionTokens: 5, TotalTokens: 17},
		},
	}

	handler := newTestHTTPHandlerForProviders(logger, []providers.Provider{localProvider, cloudProvider}, config.Config{
		Provider: config.ProviderConfig{
			MaxAttempts:     1,
			RetryBackoff:    time.Millisecond,
			FailoverEnabled: true,
		},
		Router: config.RouterConfig{
			DefaultProvider:  "openai",
			DefaultModel:     "gpt-4o-mini",
			Strategy:         "local_first",
			FallbackProvider: "openai",
		},
	})
	response := performJSONRequest(t, handler, `{"messages":[{"role":"user","content":"hello"}]}`)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("X-Runtime-Provider"); got != "openai" {
		t.Fatalf("X-Runtime-Provider = %q, want openai", got)
	}
	if got := response.Header().Get("X-Runtime-Provider-Kind"); got != "cloud" {
		t.Fatalf("X-Runtime-Provider-Kind = %q, want cloud", got)
	}
	if got := response.Header().Get("X-Runtime-Fallback-From"); got != "ollama" {
		t.Fatalf("X-Runtime-Fallback-From = %q, want ollama", got)
	}
	if got := response.Header().Get("X-Runtime-Attempts"); got != "2" {
		t.Fatalf("X-Runtime-Attempts = %q, want 2", got)
	}
	if got := response.Header().Get("X-Runtime-Retries"); got != "0" {
		t.Fatalf("X-Runtime-Retries = %q, want 0", got)
	}
	if got := response.Header().Get("X-Runtime-Route-Reason"); got != "default_model_local_first_failover" {
		t.Fatalf("X-Runtime-Route-Reason = %q, want failover reason", got)
	}
	if localProvider.CallCount() != 1 {
		t.Fatalf("local provider call count = %d, want 1", localProvider.CallCount())
	}
	if cloudProvider.CallCount() != 1 {
		t.Fatalf("cloud provider call count = %d, want 1", cloudProvider.CallCount())
	}
}

func TestChatCompletionsSkipsDegradedProviderAfterTransientFailures(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	localProvider := &fakeProvider{
		name:         "ollama",
		defaultModel: "llama3.1:8b",
		capabilities: providers.Capabilities{
			Name:         "ollama",
			Kind:         providers.KindLocal,
			DefaultModel: "llama3.1:8b",
			Models:       []string{"llama3.1:8b"},
		},
		errSequence: []error{
			&providers.UpstreamError{StatusCode: http.StatusBadGateway, Message: "local runtime unavailable", Type: "server_error"},
		},
		response: &types.ChatResponse{
			ID:        "chatcmpl-local",
			Model:     "llama3.1:8b",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices:   []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "local"}, FinishReason: "stop"}},
			Usage:     types.Usage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14},
		},
	}
	cloudProvider := &fakeProvider{
		name:         "openai",
		defaultModel: "gpt-4o-mini",
		capabilities: providers.Capabilities{
			Name:         "openai",
			Kind:         providers.KindCloud,
			DefaultModel: "gpt-4o-mini",
			Models:       []string{"gpt-4o-mini"},
		},
		response: &types.ChatResponse{
			ID:        "chatcmpl-cloud",
			Model:     "gpt-4o-mini",
			CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
			Choices:   []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "cloud fallback"}, FinishReason: "stop"}},
			Usage:     types.Usage{PromptTokens: 12, CompletionTokens: 5, TotalTokens: 17},
		},
	}

	handler := newTestHTTPHandlerForProviders(logger, []providers.Provider{localProvider, cloudProvider}, config.Config{
		Provider: config.ProviderConfig{
			MaxAttempts:     1,
			RetryBackoff:    time.Millisecond,
			FailoverEnabled: true,
			HealthThreshold: 1,
			HealthCooldown:  time.Hour,
		},
		Router: config.RouterConfig{
			DefaultProvider:  "openai",
			DefaultModel:     "gpt-4o-mini",
			Strategy:         "local_first",
			FallbackProvider: "openai",
		},
	})

	first := performJSONRequest(t, handler, `{"messages":[{"role":"user","content":"hello"}]}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d, body=%s", first.Code, http.StatusOK, first.Body.String())
	}
	if got := first.Header().Get("X-Runtime-Fallback-From"); got != "ollama" {
		t.Fatalf("first X-Runtime-Fallback-From = %q, want ollama", got)
	}

	second := performJSONRequest(t, handler, `{"messages":[{"role":"user","content":"hello again"}]}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d, body=%s", second.Code, http.StatusOK, second.Body.String())
	}
	if got := second.Header().Get("X-Runtime-Provider"); got != "openai" {
		t.Fatalf("second X-Runtime-Provider = %q, want openai", got)
	}
	if got := second.Header().Get("X-Runtime-Route-Reason"); got != "default_model_fallback_local_unavailable" && got != "default_model_fallback_unhealthy_local" && got != "default_model_fallback_degraded_provider" {
		t.Fatalf("second X-Runtime-Route-Reason = %q, want degraded fallback reason", got)
	}
	if localProvider.CallCount() != 1 {
		t.Fatalf("local provider call count = %d, want 1 because degraded provider should be skipped", localProvider.CallCount())
	}
	if cloudProvider.CallCount() != 2 {
		t.Fatalf("cloud provider call count = %d, want 2", cloudProvider.CallCount())
	}
}

func TestNormalizeChatRequestCarriesProviderHint(t *testing.T) {
	t.Parallel()

	req := OpenAIChatCompletionRequest{
		Model:    "llama3.1:8b",
		Provider: "ollama",
		User:     "team-a",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: "hello"},
		},
	}

	got, err := normalizeChatRequest(req, "req-123", auth.Principal{})
	if err != nil {
		t.Fatalf("normalizeChatRequest() error = %v", err)
	}
	if got.Scope.ProviderHint != "ollama" {
		t.Fatalf("provider hint = %q, want ollama", got.Scope.ProviderHint)
	}
	if got.Scope.User != "team-a" {
		t.Fatalf("scope user = %q, want team-a", got.Scope.User)
	}
}

func TestNormalizeChatRequestBindsTenantFromPrincipal(t *testing.T) {
	t.Parallel()

	got, err := normalizeChatRequest(OpenAIChatCompletionRequest{
		Model: "gpt-4o-mini",
		User:  "team-a",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: "hello"},
		},
	}, "req-123", auth.Principal{
		Role:   "tenant",
		Tenant: "team-a",
	})
	if err != nil {
		t.Fatalf("normalizeChatRequest() error = %v", err)
	}
	if got.Scope.Tenant != "team-a" {
		t.Fatalf("scope tenant = %q, want team-a", got.Scope.Tenant)
	}
}

func TestNormalizeChatRequestRejectsTenantImpersonation(t *testing.T) {
	t.Parallel()

	_, err := normalizeChatRequest(OpenAIChatCompletionRequest{
		Model: "gpt-4o-mini",
		User:  "team-b",
		Messages: []OpenAIChatMessage{
			{Role: "user", Content: "hello"},
		},
	}, "req-123", auth.Principal{
		Role:   "tenant",
		Tenant: "team-a",
	})
	if err == nil {
		t.Fatal("normalizeChatRequest() error = nil, want tenant mismatch error")
	}
}

func TestModelsReturnsAggregatedProviderCapabilities(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cloudProvider := &fakeProvider{name: "openai"}
	localProvider := &fakeProvider{
		name: "ollama",
		capabilities: providers.Capabilities{
			Name:            "ollama",
			Kind:            providers.KindLocal,
			DefaultModel:    "llama3.1:8b",
			Models:          []string{"llama3.1:8b", "qwen2.5:7b"},
			DiscoverySource: "upstream_v1_models",
		},
	}

	registry := providers.NewRegistry(cloudProvider, localProvider)
	providerCatalog := catalog.NewRegistryCatalog(registry, nil)
	budgetStore := governor.NewMemoryBudgetStore()
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(time.Minute),
		Router:    router.NewRuleRouter("openai", "gpt-4o-mini", "explicit_or_default", "", providerCatalog),
		Catalog:   providerCatalog,
		Governor:  governor.NewStaticGovernor(config.GovernorConfig{MaxPromptTokens: 64_000}, budgetStore, budgetStore),
		Providers: registry,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{Name: "openai", Kind: "cloud"},
				{Name: "ollama", Kind: "local"},
			},
		}, defaultPricebookForTests()),
		Tracer: profiler.NewInMemoryTracer(nil),
	})
	handler := NewServer(logger, NewHandler(config.Config{}, logger, service, nil))

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response OpenAIModelsResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Object != "list" {
		t.Fatalf("object = %q, want list", response.Object)
	}
	if len(response.Data) < 3 {
		t.Fatalf("model count = %d, want at least 3", len(response.Data))
	}

	foundLocalDefault := false
	foundCloud := false
	for _, item := range response.Data {
		if item.ID == "llama3.1:8b" && item.Metadata["provider_kind"] == "local" && item.Metadata["default"] == true {
			foundLocalDefault = true
		}
		if item.ID == "gpt-4o-mini" && item.Metadata["provider"] == "openai" {
			foundCloud = true
		}
	}
	if !foundLocalDefault {
		t.Fatalf("missing local default model in response: %#v", response.Data)
	}
	if !foundCloud {
		t.Fatalf("missing cloud model in response: %#v", response.Data)
	}
}

func TestProviderStatusReturnsHealthAndDiscoveryFreshness(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	healthyProvider := &fakeProvider{
		name: "openai",
		capabilities: providers.Capabilities{
			Name:            "openai",
			Kind:            providers.KindCloud,
			DefaultModel:    "gpt-4o-mini",
			Models:          []string{"gpt-4o-mini"},
			DiscoverySource: "upstream_v1_models",
			RefreshedAt:     time.Unix(1_700_000_000, 0).UTC(),
		},
	}
	degradedProvider := &fakeProvider{
		name:         "ollama",
		capsErr:      io.EOF,
		defaultModel: "llama3.1:8b",
		capabilities: providers.Capabilities{
			Name:            "ollama",
			Kind:            providers.KindLocal,
			DefaultModel:    "llama3.1:8b",
			Models:          []string{"llama3.1:8b"},
			DiscoverySource: "config_fallback",
		},
	}

	registry := providers.NewRegistry(healthyProvider, degradedProvider)
	providerCatalog := catalog.NewRegistryCatalog(registry, nil)
	budgetStore := governor.NewMemoryBudgetStore()
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(time.Minute),
		Router:    router.NewRuleRouter("openai", "gpt-4o-mini", "explicit_or_default", "", providerCatalog),
		Catalog:   providerCatalog,
		Governor:  governor.NewStaticGovernor(config.GovernorConfig{MaxPromptTokens: 64_000}, budgetStore, budgetStore),
		Providers: registry,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{Name: "openai", Kind: "cloud"},
				{Name: "ollama", Kind: "local"},
			},
		}, defaultPricebookForTests()),
		Tracer: profiler.NewInMemoryTracer(nil),
	})
	handler := NewServer(logger, NewHandler(config.Config{}, logger, service, nil))

	req := httptest.NewRequest(http.MethodGet, "/admin/providers", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response ProviderStatusResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Object != "provider_status" {
		t.Fatalf("object = %q, want provider_status", response.Object)
	}
	if len(response.Data) != 2 {
		t.Fatalf("provider count = %d, want 2", len(response.Data))
	}

	foundHealthy := false
	foundDegraded := false
	for _, item := range response.Data {
		if item.Name == "openai" && item.Healthy && item.RefreshedAt != "" {
			foundHealthy = true
		}
		if item.Name == "ollama" && !item.Healthy && item.Status == "degraded" && item.Error != "" {
			foundDegraded = true
		}
	}
	if !foundHealthy {
		t.Fatalf("missing healthy provider entry: %#v", response.Data)
	}
	if !foundDegraded {
		t.Fatalf("missing degraded provider entry: %#v", response.Data)
	}
}

func TestProviderPresetsReturnsCatalog(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := NewServer(logger, NewHandler(config.Config{}, logger, nil, nil))

	req := httptest.NewRequest(http.MethodGet, "/v1/provider-presets", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response ProviderPresetResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Object != "provider_presets" {
		t.Fatalf("object = %q, want provider_presets", response.Object)
	}
	if len(response.Data) < 4 {
		t.Fatalf("preset count = %d, want at least 4", len(response.Data))
	}
	if len(response.Data) != len(config.BuiltInProviders()) {
		t.Fatalf("preset count = %d, want %d built-in presets", len(response.Data), len(config.BuiltInProviders()))
	}

	foundAnthropic := false
	foundOllama := false
	for _, item := range response.Data {
		if item.ID == "anthropic" && item.Protocol == "anthropic" && item.EnvSnippet != "" {
			foundAnthropic = true
		}
		if item.ID == "ollama" && item.Kind == "local" && item.EnvSnippet != "" {
			foundOllama = true
		}
	}
	if !foundAnthropic {
		t.Fatalf("missing anthropic preset: %#v", response.Data)
	}
	if !foundOllama {
		t.Fatalf("missing ollama preset: %#v", response.Data)
	}
}

func TestBudgetEndpointsRequireAdminWhenTenantKeysConfigured(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newBudgetTestHandlerWithConfig(logger, config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-secret",
			APIKeys: []config.APIKeyConfig{
				{Name: "team-a", Key: "tenant-secret", Tenant: "team-a", Role: "tenant"},
			},
		},
		Governor: config.GovernorConfig{
			MaxPromptTokens:      64_000,
			MaxTotalBudgetMicros: 10_000_000,
			BudgetBackend:        "memory",
			BudgetKey:            "global",
			BudgetScope:          "global",
		},
	}, governor.NewMemoryBudgetStore(), nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/budget", nil)
	req.Header.Set("Authorization", "Bearer tenant-secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestChatCompletionAPIKeyRejectsTenantImpersonation(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{name: "openai"}
	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Server: config.ServerConfig{
			APIKeys: []config.APIKeyConfig{
				{Name: "team-a", Key: "tenant-secret", Tenant: "team-a", Role: "tenant"},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","user":"team-b","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer tenant-secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}

func TestModelsFilteredForTenantAPIKeyAllowlist(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cloudProvider := &fakeProvider{name: "openai"}
	localProvider := &fakeProvider{
		name: "ollama",
		capabilities: providers.Capabilities{
			Name:            "ollama",
			Kind:            providers.KindLocal,
			DefaultModel:    "llama3.1:8b",
			Models:          []string{"llama3.1:8b"},
			DiscoverySource: "upstream_v1_models",
		},
	}
	registry := providers.NewRegistry(cloudProvider, localProvider)
	providerCatalog := catalog.NewRegistryCatalog(registry, nil)
	budgetStore := governor.NewMemoryBudgetStore()
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(time.Minute),
		Router:    router.NewRuleRouter("openai", "gpt-4o-mini", "explicit_or_default", "", providerCatalog),
		Catalog:   providerCatalog,
		Governor:  governor.NewStaticGovernor(config.GovernorConfig{MaxPromptTokens: 64_000}, budgetStore, budgetStore),
		Providers: registry,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{Name: "openai", Kind: "cloud"},
				{Name: "ollama", Kind: "local"},
			},
		}, defaultPricebookForTests()),
		Tracer: profiler.NewInMemoryTracer(nil),
	})
	handler := NewServer(logger, NewHandler(config.Config{
		Server: config.ServerConfig{
			APIKeys: []config.APIKeyConfig{
				{
					Name:             "team-a",
					Key:              "tenant-secret",
					Tenant:           "team-a",
					Role:             "tenant",
					AllowedProviders: []string{"ollama"},
					AllowedModels:    []string{"llama3.1:8b"},
				},
			},
		},
	}, logger, service, nil))

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer tenant-secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response OpenAIModelsResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(response.Data) != 1 {
		t.Fatalf("model count = %d, want 1", len(response.Data))
	}
	if response.Data[0].ID != "llama3.1:8b" {
		t.Fatalf("model id = %q, want llama3.1:8b", response.Data[0].ID)
	}
}

func TestSessionEndpointReturnsAnonymousTenantAndAdminStates(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	store, err := controlplane.NewFileStore(filepath.Join(t.TempDir(), "control-plane.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	tenant, err := store.UpsertTenant(context.Background(), controlplane.Tenant{Name: "Team A"})
	if err != nil {
		t.Fatalf("UpsertTenant() error = %v", err)
	}
	if _, err := store.UpsertAPIKey(context.Background(), controlplane.APIKey{
		Name:   "Team A Dev",
		Key:    "tenant-secret",
		Tenant: tenant.ID,
		Role:   "tenant",
	}); err != nil {
		t.Fatalf("UpsertAPIKey() error = %v", err)
	}

	handler := newBudgetTestHandlerWithConfig(logger, config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-secret",
		},
	}, governor.NewMemoryBudgetStore(), store)

	cases := []struct {
		name        string
		token       string
		wantStatus  int
		wantRole    string
		wantTenant  string
		wantSource  string
		wantKeyID   string
		wantAuth    bool
		wantInvalid bool
	}{
		{name: "anonymous", wantStatus: http.StatusOK, wantRole: "anonymous", wantSource: "no_token", wantAuth: false},
		{name: "tenant", token: "tenant-secret", wantStatus: http.StatusOK, wantRole: "tenant", wantTenant: "team-a", wantSource: "control_plane_api_key", wantKeyID: "team-a-dev", wantAuth: true},
		{name: "admin", token: "admin-secret", wantStatus: http.StatusOK, wantRole: "admin", wantSource: "admin_token", wantAuth: true},
		{name: "invalid", token: "bad-secret", wantStatus: http.StatusOK, wantRole: "invalid", wantSource: "invalid_token", wantAuth: false, wantInvalid: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}

			var response SessionResponse
			if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if response.Data.Role != tc.wantRole {
				t.Fatalf("role = %q, want %q", response.Data.Role, tc.wantRole)
			}
			if response.Data.Tenant != tc.wantTenant {
				t.Fatalf("tenant = %q, want %q", response.Data.Tenant, tc.wantTenant)
			}
			if response.Data.Source != tc.wantSource {
				t.Fatalf("source = %q, want %q", response.Data.Source, tc.wantSource)
			}
			if response.Data.KeyID != tc.wantKeyID {
				t.Fatalf("key_id = %q, want %q", response.Data.KeyID, tc.wantKeyID)
			}
			if response.Data.Authenticated != tc.wantAuth {
				t.Fatalf("authenticated = %t, want %t", response.Data.Authenticated, tc.wantAuth)
			}
			if response.Data.InvalidToken != tc.wantInvalid {
				t.Fatalf("invalid_token = %t, want %t", response.Data.InvalidToken, tc.wantInvalid)
			}
		})
	}
}

func TestControlPlaneAdminEndpointsPersistAndListState(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	store, err := controlplane.NewFileStore(filepath.Join(t.TempDir(), "control-plane.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	handler := newBudgetTestHandlerWithConfig(logger, config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-secret",
		},
		Governor: config.GovernorConfig{
			MaxPromptTokens:      64_000,
			MaxTotalBudgetMicros: 10_000_000,
			BudgetBackend:        "memory",
			BudgetKey:            "global",
			BudgetScope:          "global",
		},
	}, governor.NewMemoryBudgetStore(), store)

	tenantReq := httptest.NewRequest(http.MethodPost, "/admin/control-plane/tenants", strings.NewReader(`{"name":"Team A","description":"Primary tenant","allowed_providers":["ollama"],"enabled":true}`))
	tenantReq.Header.Set("Authorization", "Bearer admin-secret")
	tenantReq.Header.Set("Content-Type", "application/json")
	tenantRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tenantRecorder, tenantReq)
	if tenantRecorder.Code != http.StatusOK {
		t.Fatalf("tenant status = %d, want %d, body=%s", tenantRecorder.Code, http.StatusOK, tenantRecorder.Body.String())
	}

	keyReq := httptest.NewRequest(http.MethodPost, "/admin/control-plane/api-keys", strings.NewReader(`{"name":"Team A Dev","key":"hecate-team-a-dev","tenant":"team-a","role":"tenant","allowed_models":["llama3.1:8b"],"enabled":true}`))
	keyReq.Header.Set("Authorization", "Bearer admin-secret")
	keyReq.Header.Set("Content-Type", "application/json")
	keyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(keyRecorder, keyReq)
	if keyRecorder.Code != http.StatusOK {
		t.Fatalf("api key status = %d, want %d, body=%s", keyRecorder.Code, http.StatusOK, keyRecorder.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/admin/control-plane", nil)
	statusReq.Header.Set("Authorization", "Bearer admin-secret")
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusReq)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", statusRecorder.Code, http.StatusOK, statusRecorder.Body.String())
	}

	var response ControlPlaneResponse
	if err := json.NewDecoder(bytes.NewReader(statusRecorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Data.Backend != "file" {
		t.Fatalf("backend = %q, want file", response.Data.Backend)
	}
	if len(response.Data.Tenants) != 1 {
		t.Fatalf("tenant count = %d, want 1", len(response.Data.Tenants))
	}
	if len(response.Data.APIKeys) != 1 {
		t.Fatalf("api key count = %d, want 1", len(response.Data.APIKeys))
	}
	if response.Data.APIKeys[0].KeyPreview == "" {
		t.Fatal("expected redacted key preview")
	}
	if len(response.Data.Events) != 2 {
		t.Fatalf("event count = %d, want 2", len(response.Data.Events))
	}
}

func TestControlPlaneLifecycleEndpoints(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	store, err := controlplane.NewFileStore(filepath.Join(t.TempDir(), "control-plane.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	handler := newBudgetTestHandlerWithConfig(logger, config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-secret",
		},
		Governor: config.GovernorConfig{
			MaxPromptTokens:      64_000,
			MaxTotalBudgetMicros: 10_000_000,
			BudgetBackend:        "memory",
			BudgetKey:            "global",
			BudgetScope:          "global",
		},
	}, governor.NewMemoryBudgetStore(), store)

	postJSON := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer admin-secret")
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := postJSON("/admin/control-plane/tenants", `{"name":"Team A","enabled":true}`); recorder.Code != http.StatusOK {
		t.Fatalf("create tenant status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if recorder := postJSON("/admin/control-plane/api-keys", `{"name":"Team A Dev","key":"secret","tenant":"team-a","role":"tenant","enabled":true}`); recorder.Code != http.StatusOK {
		t.Fatalf("create api key status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if recorder := postJSON("/admin/control-plane/tenants/enabled", `{"id":"team-a","enabled":false}`); recorder.Code != http.StatusOK {
		t.Fatalf("disable tenant status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if recorder := postJSON("/admin/control-plane/api-keys/enabled", `{"id":"team-a-dev","enabled":false}`); recorder.Code != http.StatusOK {
		t.Fatalf("disable api key status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if recorder := postJSON("/admin/control-plane/api-keys/rotate", `{"id":"team-a-dev","key":"new-secret"}`); recorder.Code != http.StatusOK {
		t.Fatalf("rotate api key status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if recorder := postJSON("/admin/control-plane/tenants/delete", `{"id":"team-a"}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("delete tenant while referenced status = %d, want %d, body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if recorder := postJSON("/admin/control-plane/api-keys/delete", `{"id":"team-a-dev"}`); recorder.Code != http.StatusOK {
		t.Fatalf("delete api key status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if recorder := postJSON("/admin/control-plane/tenants/delete", `{"id":"team-a"}`); recorder.Code != http.StatusOK {
		t.Fatalf("delete tenant status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/control-plane", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response ControlPlaneResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(response.Data.Events) != 7 {
		t.Fatalf("event count = %d, want 7", len(response.Data.Events))
	}
	if response.Data.Events[0].Actor == "" {
		t.Fatal("expected control plane audit actor to be populated")
	}
	if response.Data.Events[len(response.Data.Events)-1].Action != "tenant.deleted" {
		t.Fatalf("last event action = %q, want tenant.deleted", response.Data.Events[len(response.Data.Events)-1].Action)
	}
}

func TestBudgetStatusReturnsCurrentBalance(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	budgetStore := governor.NewMemoryBudgetStore()
	if _, err := budgetStore.Credit(context.Background(), "global", 5_000_000); err != nil {
		t.Fatalf("Credit() error = %v", err)
	}
	if _, err := budgetStore.Debit(context.Background(), governor.UsageEvent{BudgetKey: "global", CostMicros: 3_000}); err != nil {
		t.Fatalf("Debit() error = %v", err)
	}

	handler := newBudgetTestHandler(logger, config.GovernorConfig{
		MaxPromptTokens:      64_000,
		MaxTotalBudgetMicros: 5_000_000,
		BudgetBackend:        "memory",
		BudgetKey:            "global",
		BudgetScope:          "global",
	}, budgetStore)

	req := httptest.NewRequest(http.MethodGet, "/admin/budget", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response BudgetStatusResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Object != "budget_status" {
		t.Fatalf("object = %q, want budget_status", response.Object)
	}
	if response.Data.Key != "global" {
		t.Fatalf("key = %q, want global", response.Data.Key)
	}
	if response.Data.BalanceMicrosUSD != 4_997_000 {
		t.Fatalf("balance_micros_usd = %d, want 4997000", response.Data.BalanceMicrosUSD)
	}
	if response.Data.DebitedMicrosUSD != 3_000 {
		t.Fatalf("debited_micros_usd = %d, want 3000", response.Data.DebitedMicrosUSD)
	}
	if len(response.Data.Warnings) == 0 {
		t.Fatal("warnings = empty, want configured default warnings")
	}
}

func TestBudgetResetSupportsExplicitKey(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	budgetStore := governor.NewMemoryBudgetStore()
	if _, err := budgetStore.Credit(context.Background(), "team-a", 20_000); err != nil {
		t.Fatalf("Credit() error = %v", err)
	}
	if _, err := budgetStore.Debit(context.Background(), governor.UsageEvent{BudgetKey: "team-a", CostMicros: 9_999}); err != nil {
		t.Fatalf("Debit() error = %v", err)
	}

	handler := newBudgetTestHandler(logger, config.GovernorConfig{
		MaxPromptTokens:      64_000,
		MaxTotalBudgetMicros: 10_000_000,
		BudgetBackend:        "memory",
		BudgetKey:            "global",
		BudgetScope:          "global",
	}, budgetStore)

	req := httptest.NewRequest(http.MethodPost, "/admin/budget/reset", strings.NewReader(`{"key":"team-a"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response BudgetStatusResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Data.Key != "team-a" {
		t.Fatalf("key = %q, want team-a", response.Data.Key)
	}
	if response.Data.BalanceMicrosUSD != 0 {
		t.Fatalf("balance_micros_usd = %d, want 0", response.Data.BalanceMicrosUSD)
	}
}

func TestBudgetStatusSupportsTenantProviderScope(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	budgetStore := governor.NewMemoryBudgetStore()
	if _, err := budgetStore.Credit(context.Background(), "global:tenant:team-a:provider:ollama", 10_000); err != nil {
		t.Fatalf("Credit() error = %v", err)
	}
	if _, err := budgetStore.Debit(context.Background(), governor.UsageEvent{
		BudgetKey:  "global:tenant:team-a:provider:ollama",
		CostMicros: 7_500,
	}); err != nil {
		t.Fatalf("Debit() error = %v", err)
	}

	handler := newBudgetTestHandler(logger, config.GovernorConfig{
		MaxPromptTokens:      64_000,
		MaxTotalBudgetMicros: 10_000_000,
		BudgetBackend:        "memory",
		BudgetKey:            "global",
		BudgetScope:          "tenant_provider",
		BudgetTenantFallback: "anonymous",
	}, budgetStore)

	req := httptest.NewRequest(http.MethodGet, "/admin/budget?scope=tenant_provider&tenant=team-a&provider=ollama", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response BudgetStatusResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Data.Scope != "tenant_provider" {
		t.Fatalf("scope = %q, want tenant_provider", response.Data.Scope)
	}
	if response.Data.Provider != "ollama" {
		t.Fatalf("provider = %q, want ollama", response.Data.Provider)
	}
	if response.Data.Tenant != "team-a" {
		t.Fatalf("tenant = %q, want team-a", response.Data.Tenant)
	}
	if response.Data.BalanceMicrosUSD != 2_500 {
		t.Fatalf("balance_micros_usd = %d, want 2500", response.Data.BalanceMicrosUSD)
	}
}

func TestBudgetTopUpAndSetLimitEndpoints(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	budgetStore := governor.NewMemoryBudgetStore()

	handler := newBudgetTestHandler(logger, config.GovernorConfig{
		MaxPromptTokens:      64_000,
		MaxTotalBudgetMicros: 5_000_000,
		BudgetBackend:        "memory",
		BudgetKey:            "global",
		BudgetScope:          "tenant_provider",
		BudgetTenantFallback: "anonymous",
	}, budgetStore)

	topUpReq := httptest.NewRequest(http.MethodPost, "/admin/budget/topup", strings.NewReader(`{"scope":"tenant_provider","tenant":"team-a","provider":"ollama","amount_micros_usd":2000000}`))
	topUpReq.Header.Set("Content-Type", "application/json")
	topUpRecorder := httptest.NewRecorder()
	handler.ServeHTTP(topUpRecorder, topUpReq)

	if topUpRecorder.Code != http.StatusOK {
		t.Fatalf("topup status = %d, want %d, body=%s", topUpRecorder.Code, http.StatusOK, topUpRecorder.Body.String())
	}

	var topUpResponse BudgetStatusResponse
	if err := json.NewDecoder(bytes.NewReader(topUpRecorder.Body.Bytes())).Decode(&topUpResponse); err != nil {
		t.Fatalf("topup Decode() error = %v", err)
	}
	if topUpResponse.Data.BalanceMicrosUSD != 2_000_000 {
		t.Fatalf("topup balance_micros_usd = %d, want 2000000", topUpResponse.Data.BalanceMicrosUSD)
	}
	if topUpResponse.Data.BalanceSource != "store" {
		t.Fatalf("topup balance_source = %q, want store", topUpResponse.Data.BalanceSource)
	}

	limitReq := httptest.NewRequest(http.MethodPost, "/admin/budget/limit", strings.NewReader(`{"scope":"tenant_provider","tenant":"team-a","provider":"ollama","balance_micros_usd":500000}`))
	limitReq.Header.Set("Content-Type", "application/json")
	limitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(limitRecorder, limitReq)

	if limitRecorder.Code != http.StatusOK {
		t.Fatalf("limit status = %d, want %d, body=%s", limitRecorder.Code, http.StatusOK, limitRecorder.Body.String())
	}

	var limitResponse BudgetStatusResponse
	if err := json.NewDecoder(bytes.NewReader(limitRecorder.Body.Bytes())).Decode(&limitResponse); err != nil {
		t.Fatalf("limit Decode() error = %v", err)
	}
	if limitResponse.Data.BalanceMicrosUSD != 500_000 {
		t.Fatalf("limit balance_micros_usd = %d, want 500000", limitResponse.Data.BalanceMicrosUSD)
	}
	if len(limitResponse.Data.History) != 2 {
		t.Fatalf("limit history length = %d, want 2", len(limitResponse.Data.History))
	}
	if limitResponse.Data.History[0].Type != "set_balance" {
		t.Fatalf("latest history type = %q, want set_balance", limitResponse.Data.History[0].Type)
	}
	if limitResponse.Data.History[1].Type != "top_up" {
		t.Fatalf("older history type = %q, want top_up", limitResponse.Data.History[1].Type)
	}
}

func TestAccountSummaryReturnsModelEstimates(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	budgetStore := governor.NewMemoryBudgetStore()
	if _, err := budgetStore.Credit(context.Background(), "global", 1_000_000); err != nil {
		t.Fatalf("Credit() error = %v", err)
	}

	handler := newBudgetTestHandler(logger, config.GovernorConfig{
		MaxPromptTokens:      64_000,
		MaxTotalBudgetMicros: 1_000_000,
		BudgetBackend:        "memory",
		BudgetKey:            "global",
		BudgetScope:          "global",
	}, budgetStore)

	req := httptest.NewRequest(http.MethodGet, "/admin/accounts/summary", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response AccountSummaryResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Object != "account_summary" {
		t.Fatalf("object = %q, want account_summary", response.Object)
	}
	if response.Data.Account.BalanceMicrosUSD != 1_000_000 {
		t.Fatalf("balance_micros_usd = %d, want 1000000", response.Data.Account.BalanceMicrosUSD)
	}
	if len(response.Data.Estimates) == 0 {
		t.Fatal("estimates = empty, want model estimates")
	}
}

func TestRequestLedgerReturnsRecentBudgetEvents(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	budgetStore := governor.NewMemoryBudgetStore()
	now := time.Now().UTC()
	if err := budgetStore.AppendEvent(context.Background(), governor.BudgetEvent{
		Key:               "global:tenant:team-a:provider:openai",
		Type:              "debit",
		Scope:             "tenant_provider",
		Provider:          "openai",
		Tenant:            "team-a",
		Model:             "gpt-4o-mini",
		RequestID:         "req-newer",
		AmountMicrosUSD:   3200,
		BalanceMicrosUSD:  996800,
		CreditedMicrosUSD: 1_000_000,
		DebitedMicrosUSD:  3200,
		PromptTokens:      12,
		CompletionTokens:  4,
		TotalTokens:       16,
		OccurredAt:        now,
	}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if err := budgetStore.AppendEvent(context.Background(), governor.BudgetEvent{
		Key:               "global:tenant:team-b:provider:ollama",
		Type:              "debit",
		Scope:             "tenant_provider",
		Provider:          "ollama",
		Tenant:            "team-b",
		Model:             "llama3.1:8b",
		RequestID:         "req-older",
		AmountMicrosUSD:   0,
		BalanceMicrosUSD:  500_000,
		CreditedMicrosUSD: 500_000,
		DebitedMicrosUSD:  0,
		PromptTokens:      20,
		CompletionTokens:  5,
		TotalTokens:       25,
		OccurredAt:        now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	handler := newBudgetTestHandler(logger, config.GovernorConfig{
		MaxPromptTokens:      64_000,
		MaxTotalBudgetMicros: 1_000_000,
		BudgetBackend:        "memory",
		BudgetKey:            "global",
		BudgetScope:          "global",
	}, budgetStore)

	req := httptest.NewRequest(http.MethodGet, "/admin/requests?limit=1", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response RequestLedgerResponse
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Object != "request_ledger" {
		t.Fatalf("object = %q, want request_ledger", response.Object)
	}
	if len(response.Data) != 1 {
		t.Fatalf("entries = %d, want 1", len(response.Data))
	}
	if response.Data[0].RequestID != "req-newer" {
		t.Fatalf("request_id = %q, want req-newer", response.Data[0].RequestID)
	}
	if response.Data[0].Model != "gpt-4o-mini" {
		t.Fatalf("model = %q, want gpt-4o-mini", response.Data[0].Model)
	}
}

func TestChatSessionsPersistTurnsWithRuntimeMetadata(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "anthropic",
		capabilities: providers.Capabilities{
			Name:         "anthropic",
			Kind:         providers.KindCloud,
			DefaultModel: "claude-sonnet-4-20250514",
			Models:       []string{"claude-sonnet-4-20250514"},
		},
		response: &types.ChatResponse{
			ID:        "msg_123",
			Model:     "claude-sonnet-4-20250514",
			CreatedAt: time.Now().UTC(),
			Choices:   []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "Hello from Claude."}, FinishReason: "end_turn"}},
			Usage:     types.Usage{PromptTokens: 12, CompletionTokens: 4, TotalTokens: 16},
		},
	}

	handler := newTestHTTPHandlerForProviders(logger, []providers.Provider{provider}, config.Config{
		Router: config.RouterConfig{
			DefaultProvider: "anthropic",
			DefaultModel:    "claude-sonnet-4-20250514",
			Strategy:        "explicit_or_default",
		},
	})

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/sessions", strings.NewReader(`{"title":"Claude debugging"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d, body=%s", createRecorder.Code, http.StatusOK, createRecorder.Body.String())
	}

	var created ChatSessionResponse
	if err := json.NewDecoder(bytes.NewReader(createRecorder.Body.Bytes())).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.Data.ID == "" {
		t.Fatal("session id = empty, want session id")
	}

	chatBody := fmt.Sprintf(`{"model":"claude-sonnet-4-20250514","provider":"anthropic","session_id":"%s","messages":[{"role":"user","content":"Say hello."}]}`, created.Data.ID)
	chatRecorder := performJSONRequest(t, handler, chatBody)
	if chatRecorder.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want %d, body=%s", chatRecorder.Code, http.StatusOK, chatRecorder.Body.String())
	}

	sessionRecorder := httptest.NewRecorder()
	sessionRequest := httptest.NewRequest(http.MethodGet, "/v1/chat/sessions/"+created.Data.ID, nil)
	handler.ServeHTTP(sessionRecorder, sessionRequest)
	if sessionRecorder.Code != http.StatusOK {
		t.Fatalf("get session status = %d, want %d, body=%s", sessionRecorder.Code, http.StatusOK, sessionRecorder.Body.String())
	}

	var session ChatSessionResponse
	if err := json.NewDecoder(bytes.NewReader(sessionRecorder.Body.Bytes())).Decode(&session); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(session.Data.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(session.Data.Turns))
	}
	if session.Data.Turns[0].Provider != "anthropic" {
		t.Fatalf("provider = %q, want anthropic", session.Data.Turns[0].Provider)
	}
	if session.Data.Turns[0].Model != "claude-sonnet-4-20250514" {
		t.Fatalf("model = %q, want Claude model", session.Data.Turns[0].Model)
	}
	if session.Data.Turns[0].UserMessage.Content != "Say hello." {
		t.Fatalf("user content = %q, want original prompt", session.Data.Turns[0].UserMessage.Content)
	}
}

func newTestHTTPHandler(logger *slog.Logger, provider providers.Provider) http.Handler {
	return newTestHTTPHandlerWithConfig(logger, provider, config.Config{})
}

func newTestHTTPHandlerWithConfig(logger *slog.Logger, provider providers.Provider, cfg config.Config) http.Handler {
	return newTestHTTPHandlerForProviders(logger, []providers.Provider{provider}, cfg)
}

func newTestHTTPHandlerForProviders(logger *slog.Logger, items []providers.Provider, cfg config.Config) http.Handler {
	registry := providers.NewRegistry(items...)
	healthTracker := providers.NewMemoryHealthTracker(cfg.Provider.HealthThreshold, cfg.Provider.HealthCooldown)
	providerCatalog := catalog.NewRegistryCatalog(registry, healthTracker)
	budgetStore := governor.NewMemoryBudgetStore()
	governorCfg := mergeGovernorDefaults(cfg.Governor)
	routerCfg := cfg.Router
	if routerCfg.DefaultProvider == "" && len(items) > 0 {
		routerCfg.DefaultProvider = items[0].Name()
	}
	if routerCfg.DefaultModel == "" && len(items) > 0 {
		routerCfg.DefaultModel = items[0].DefaultModel()
	}
	if routerCfg.Strategy == "" {
		routerCfg.Strategy = "explicit_or_default"
	}
	routerEngine := router.NewRuleRouter(routerCfg.DefaultProvider, routerCfg.DefaultModel, routerCfg.Strategy, routerCfg.FallbackProvider, providerCatalog)
	retentionCfg := cfg.Retention
	if retentionCfg.TraceSnapshots.MaxCount == 0 {
		retentionCfg.TraceSnapshots = config.RetentionPolicy{MaxAge: time.Hour, MaxCount: 2000}
	}
	if retentionCfg.BudgetEvents.MaxCount == 0 {
		retentionCfg.BudgetEvents = config.RetentionPolicy{MaxAge: 30 * 24 * time.Hour, MaxCount: 200}
	}
	if retentionCfg.AuditEvents.MaxCount == 0 {
		retentionCfg.AuditEvents = config.RetentionPolicy{MaxAge: 30 * 24 * time.Hour, MaxCount: 500}
	}
	if retentionCfg.ExactCache.MaxCount == 0 {
		retentionCfg.ExactCache = config.RetentionPolicy{MaxAge: 24 * time.Hour, MaxCount: 10000}
	}
	if retentionCfg.SemanticCache.MaxCount == 0 {
		retentionCfg.SemanticCache = config.RetentionPolicy{MaxAge: 7 * 24 * time.Hour, MaxCount: 10000}
	}
	retentionManager := retention.NewManager(
		logger,
		retentionCfg,
		profiler.NewInMemoryTracer(nil),
		profiler.NewInMemoryTracer(nil),
		budgetStore,
		nil,
		nil,
		nil,
		retention.NewMemoryHistoryStore(),
	)
	service := gateway.NewService(gateway.Dependencies{
		Logger:   logger,
		Cache:    cache.NewMemoryStore(time.Minute),
		Semantic: buildTestSemanticStore(cfg),
		SemanticOptions: gateway.SemanticOptions{
			Enabled:       cfg.Cache.Semantic.Enabled,
			MinSimilarity: cfg.Cache.Semantic.MinSimilarity,
			MaxTextChars:  cfg.Cache.Semantic.MaxTextChars,
		},
		Resilience: gateway.ResilienceOptions{
			MaxAttempts:     cfg.Provider.MaxAttempts,
			RetryBackoff:    cfg.Provider.RetryBackoff,
			FailoverEnabled: cfg.Provider.FailoverEnabled,
		},
		Router:        routerEngine,
		Catalog:       providerCatalog,
		Governor:      governor.NewStaticGovernor(governorCfg, budgetStore, budgetStore),
		Providers:     registry,
		HealthTracker: healthTracker,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: providerConfigsForTests(items),
		}, pricebookConfigForTests(items)),
		Tracer:       profiler.NewInMemoryTracer(nil),
		Metrics:      telemetry.NewMetrics(),
		Retention:    retentionManager,
		ChatSessions: chatstate.NewMemoryStore(),
	})

	cfg.Governor = governorCfg
	handler := NewHandler(cfg, logger, service, nil)
	return NewServer(logger, handler)
}

func providerConfigsForTests(items []providers.Provider) []config.OpenAICompatibleProviderConfig {
	configs := make([]config.OpenAICompatibleProviderConfig, 0, len(items))
	for _, provider := range items {
		configs = append(configs, config.OpenAICompatibleProviderConfig{
			Name:         provider.Name(),
			Kind:         string(provider.Kind()),
			DefaultModel: provider.DefaultModel(),
		})
	}
	return configs
}

func pricebookConfigForTests(items []providers.Provider) config.PricebookConfig {
	entries := make([]config.ModelPriceConfig, 0, len(items)+4)
	for _, provider := range items {
		if provider.Kind() != providers.KindCloud || provider.DefaultModel() == "" {
			continue
		}
		entries = append(entries, config.ModelPriceConfig{
			Provider:                             provider.Name(),
			Model:                                provider.DefaultModel(),
			InputMicrosUSDPerMillionTokens:       150_000,
			OutputMicrosUSDPerMillionTokens:      600_000,
			CachedInputMicrosUSDPerMillionTokens: 75_000,
		})
	}
	entries = append(entries, defaultPricebookForTests().Entries...)
	return config.PricebookConfig{Entries: entries}
}

func defaultPricebookForTests() config.PricebookConfig {
	return config.PricebookConfig{
		Entries: []config.ModelPriceConfig{
			{Provider: "openai", Model: "gpt-4o-mini", InputMicrosUSDPerMillionTokens: 150_000, OutputMicrosUSDPerMillionTokens: 600_000, CachedInputMicrosUSDPerMillionTokens: 75_000},
			{Provider: "openai", Model: "gpt-4.1-mini", InputMicrosUSDPerMillionTokens: 400_000, OutputMicrosUSDPerMillionTokens: 1_600_000, CachedInputMicrosUSDPerMillionTokens: 100_000},
			{Provider: "openai", Model: "omni-moderation", InputMicrosUSDPerMillionTokens: 0, OutputMicrosUSDPerMillionTokens: 0, CachedInputMicrosUSDPerMillionTokens: 0},
			{Provider: "openai", Model: "omni-moderation-latest", InputMicrosUSDPerMillionTokens: 0, OutputMicrosUSDPerMillionTokens: 0, CachedInputMicrosUSDPerMillionTokens: 0},
		},
	}
}

func buildTestSemanticStore(cfg config.Config) cache.SemanticStore {
	if !cfg.Cache.Semantic.Enabled {
		return cache.NoopSemanticStore{}
	}
	return cache.NewMemorySemanticStore(
		cfg.Cache.Semantic.DefaultTTL,
		cfg.Cache.Semantic.MaxEntries,
		cache.LocalSimpleEmbedder{MaxTextChars: cfg.Cache.Semantic.MaxTextChars},
	)
}

func newBudgetTestHandler(logger *slog.Logger, governorCfg config.GovernorConfig, budgetStore governor.BudgetStore) http.Handler {
	return newBudgetTestHandlerWithConfig(logger, config.Config{Governor: governorCfg}, budgetStore, nil)
}

func newBudgetTestHandlerWithConfig(logger *slog.Logger, cfg config.Config, budgetStore governor.BudgetStore, cpStore controlplane.Store) http.Handler {
	provider := &fakeProvider{name: "openai"}
	registry := providers.NewRegistry(provider)
	providerCatalog := catalog.NewRegistryCatalog(registry, nil)
	governorCfg := mergeGovernorDefaults(cfg.Governor)
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(time.Minute),
		Router:    router.NewRuleRouter(provider.Name(), "gpt-4o-mini", "explicit_or_default", "", providerCatalog),
		Catalog:   providerCatalog,
		Governor:  governor.NewStaticGovernor(governorCfg, budgetStore, budgetStore),
		Providers: registry,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{Name: provider.Name(), Kind: string(provider.Kind())},
			},
		}, pricebookConfigForTests([]providers.Provider{provider})),
		Tracer:       profiler.NewInMemoryTracer(nil),
		Metrics:      telemetry.NewMetrics(),
		ChatSessions: chatstate.NewMemoryStore(),
	})

	handler := NewHandler(cfg, logger, service, cpStore)
	return NewServer(logger, handler)
}

func mergeGovernorDefaults(cfg config.GovernorConfig) config.GovernorConfig {
	if cfg.MaxPromptTokens == 0 {
		cfg.MaxPromptTokens = 64_000
	}
	if cfg.BudgetBackend == "" {
		cfg.BudgetBackend = "memory"
	}
	if cfg.BudgetKey == "" {
		cfg.BudgetKey = "global"
	}
	if cfg.BudgetScope == "" {
		cfg.BudgetScope = "global"
	}
	return cfg
}

func performJSONRequest(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

type fakeProvider struct {
	mu           sync.Mutex
	name         string
	defaultModel string
	response     *types.ChatResponse
	err          error
	errSequence  []error
	calls        int
	capabilities providers.Capabilities
	capsErr      error
}

func (p *fakeProvider) Name() string {
	if p.name == "" {
		return "openai"
	}
	return p.name
}

func (p *fakeProvider) Kind() providers.Kind {
	if p.capabilities.Kind != "" {
		return p.capabilities.Kind
	}
	return providers.KindCloud
}

func (p *fakeProvider) DefaultModel() string {
	if p.defaultModel != "" {
		return p.defaultModel
	}
	if p.capabilities.DefaultModel != "" {
		return p.capabilities.DefaultModel
	}
	return "gpt-4o-mini"
}

func (p *fakeProvider) Capabilities(_ context.Context) (providers.Capabilities, error) {
	if p.capsErr != nil {
		return p.capabilities, p.capsErr
	}
	if p.capabilities.Name != "" || len(p.capabilities.Models) > 0 || p.capabilities.DefaultModel != "" {
		return p.capabilities, nil
	}
	return providers.Capabilities{
		Name:         p.Name(),
		Kind:         p.Kind(),
		DefaultModel: p.DefaultModel(),
		Models:       []string{"gpt-4o-mini", "gpt-4o-mini-2024-07-18"},
	}, nil
}

func (p *fakeProvider) Chat(_ context.Context, _ types.ChatRequest) (*types.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls++
	if len(p.errSequence) > 0 {
		err := p.errSequence[0]
		p.errSequence = p.errSequence[1:]
		if err != nil {
			return nil, err
		}
	}
	if p.err != nil {
		return nil, p.err
	}

	cloned := *p.response
	cloned.Choices = append([]types.ChatChoice(nil), p.response.Choices...)
	return &cloned, nil
}

func (p *fakeProvider) Supports(model string) bool {
	if p.capabilities.DefaultModel == model {
		return true
	}
	for _, candidate := range p.capabilities.Models {
		if candidate == model {
			return true
		}
	}
	if p.defaultModel == model {
		return true
	}
	if strings.HasPrefix(model, "gpt-") && p.Kind() == providers.KindCloud {
		return true
	}
	if strings.HasPrefix(model, "llama") && p.Kind() == providers.KindLocal {
		return true
	}
	return false
}

func (p *fakeProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
