package api

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
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
	if payload.Data.Spans[0].Attributes["service.name"] != "hecate-gateway" {
		t.Fatalf("root span service.name = %#v, want hecate-gateway", payload.Data.Spans[0].Attributes["service.name"])
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
	if got := second.Header().Get("X-Runtime-Route-Reason"); got != "default_model_fallback_unhealthy_local" && got != "default_model_fallback_degraded_provider" {
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
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(time.Minute),
		Router:    router.NewRuleRouter("openai", "gpt-4o-mini", "explicit_or_default", "", registry),
		Governor:  governor.NewStaticGovernor(config.GovernorConfig{MaxPromptTokens: 64_000}, governor.NewMemoryBudgetStore()),
		Providers: registry,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{Name: "openai", Kind: "cloud"},
				{Name: "ollama", Kind: "local"},
			},
		}),
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
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(time.Minute),
		Router:    router.NewRuleRouter("openai", "gpt-4o-mini", "explicit_or_default", "", registry),
		Governor:  governor.NewStaticGovernor(config.GovernorConfig{MaxPromptTokens: 64_000}, governor.NewMemoryBudgetStore()),
		Providers: registry,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{Name: "openai", Kind: "cloud"},
				{Name: "ollama", Kind: "local"},
			},
		}),
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
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(time.Minute),
		Router:    router.NewRuleRouter("openai", "gpt-4o-mini", "explicit_or_default", "", registry),
		Governor:  governor.NewStaticGovernor(config.GovernorConfig{MaxPromptTokens: 64_000}, governor.NewMemoryBudgetStore()),
		Providers: registry,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{Name: "openai", Kind: "cloud"},
				{Name: "ollama", Kind: "local"},
			},
		}),
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

func TestMetricsExposeChatCacheCostAndProviderHealth(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	metrics := telemetry.NewMetrics()
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

	registry := providers.NewRegistry(provider)
	service := gateway.NewService(gateway.Dependencies{
		Logger:   logger,
		Cache:    cache.NewMemoryStore(time.Minute),
		Semantic: cache.NoopSemanticStore{},
		SemanticOptions: gateway.SemanticOptions{
			Enabled:       false,
			MinSimilarity: 0.92,
			MaxTextChars:  8_000,
		},
		Router:    router.NewRuleRouter(provider.Name(), "gpt-4o-mini", "explicit_or_default", "", registry),
		Governor:  governor.NewStaticGovernor(config.GovernorConfig{MaxPromptTokens: 64_000}, governor.NewMemoryBudgetStore()),
		Providers: registry,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{Name: provider.Name(), Kind: string(provider.Kind())},
			},
		}),
		Tracer:  profiler.NewInMemoryTracer(nil),
		Metrics: metrics,
	})
	handler := NewServer(logger, NewHandler(config.Config{}, logger, service, nil))

	performJSONRequest(t, handler, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`)
	performJSONRequest(t, handler, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}

	body := recorder.Body.String()
	wantSubstrings := []string{
		"gateway_chat_requests_total 2",
		"gateway_cache_hits_total 1",
		"gateway_cache_misses_total 1",
		`gateway_provider_requests_total{provider="openai"} 2`,
		`gateway_provider_kind_requests_total{provider_kind="cloud"} 2`,
		`gateway_provider_health{status="healthy"} 1`,
		`gateway_provider_health{status="degraded"} 0`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestBudgetStatusReturnsCurrentSpend(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	budgetStore := governor.NewMemoryBudgetStore()
	if err := budgetStore.AddSpent(context.Background(), "global", 3_000); err != nil {
		t.Fatalf("AddSpent() error = %v", err)
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
	if response.Data.CurrentMicrosUSD != 3_000 {
		t.Fatalf("current_micros_usd = %d, want 3000", response.Data.CurrentMicrosUSD)
	}
	if response.Data.RemainingMicrosUSD != 4_997_000 {
		t.Fatalf("remaining_micros_usd = %d, want 4997000", response.Data.RemainingMicrosUSD)
	}
}

func TestBudgetResetSupportsExplicitKey(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	budgetStore := governor.NewMemoryBudgetStore()
	if err := budgetStore.AddSpent(context.Background(), "team-a", 9_999); err != nil {
		t.Fatalf("AddSpent() error = %v", err)
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
	if response.Data.CurrentMicrosUSD != 0 {
		t.Fatalf("current_micros_usd = %d, want 0", response.Data.CurrentMicrosUSD)
	}
}

func TestBudgetStatusSupportsTenantProviderScope(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	budgetStore := governor.NewMemoryBudgetStore()
	if err := budgetStore.AddSpent(context.Background(), "global:tenant:team-a:provider:ollama", 7_500); err != nil {
		t.Fatalf("AddSpent() error = %v", err)
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
	if response.Data.CurrentMicrosUSD != 7_500 {
		t.Fatalf("current_micros_usd = %d, want 7500", response.Data.CurrentMicrosUSD)
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
	if topUpResponse.Data.MaxMicrosUSD != 2_000_000 {
		t.Fatalf("topup max_micros_usd = %d, want 2000000", topUpResponse.Data.MaxMicrosUSD)
	}
	if topUpResponse.Data.LimitSource != "store" {
		t.Fatalf("topup limit_source = %q, want store", topUpResponse.Data.LimitSource)
	}

	limitReq := httptest.NewRequest(http.MethodPost, "/admin/budget/limit", strings.NewReader(`{"scope":"tenant_provider","tenant":"team-a","provider":"ollama","limit_micros_usd":500000}`))
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
	if limitResponse.Data.MaxMicrosUSD != 500_000 {
		t.Fatalf("limit max_micros_usd = %d, want 500000", limitResponse.Data.MaxMicrosUSD)
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
	routerEngine := router.NewRuleRouter(routerCfg.DefaultProvider, routerCfg.DefaultModel, routerCfg.Strategy, routerCfg.FallbackProvider, registry)
	routerEngine.SetHealthTracker(healthTracker)
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
		Governor:      governor.NewStaticGovernor(governorCfg, budgetStore),
		Providers:     registry,
		HealthTracker: healthTracker,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: providerConfigsForTests(items),
		}),
		Tracer:  profiler.NewInMemoryTracer(nil),
		Metrics: telemetry.NewMetrics(),
	})

	cfg.Governor = governorCfg
	handler := NewHandler(cfg, logger, service, nil)
	return NewServer(logger, handler)
}

func providerConfigsForTests(items []providers.Provider) []config.OpenAICompatibleProviderConfig {
	configs := make([]config.OpenAICompatibleProviderConfig, 0, len(items))
	for _, provider := range items {
		cfg := config.OpenAICompatibleProviderConfig{
			Name:         provider.Name(),
			Kind:         string(provider.Kind()),
			DefaultModel: provider.DefaultModel(),
		}
		if provider.Kind() == providers.KindCloud {
			cfg.InputMicrosUSDPerMillionTokens = 150_000
			cfg.OutputMicrosUSDPerMillionTokens = 600_000
			cfg.CachedInputMicrosUSDPerMillionTokens = 75_000
		}
		configs = append(configs, cfg)
	}
	return configs
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
	governorCfg := mergeGovernorDefaults(cfg.Governor)
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(time.Minute),
		Router:    router.NewRuleRouter(provider.Name(), "gpt-4o-mini", "explicit_or_default", "", registry),
		Governor:  governor.NewStaticGovernor(governorCfg, budgetStore),
		Providers: registry,
		Pricebook: billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{Name: provider.Name(), Kind: string(provider.Kind())},
			},
		}),
		Tracer:  profiler.NewInMemoryTracer(nil),
		Metrics: telemetry.NewMetrics(),
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
