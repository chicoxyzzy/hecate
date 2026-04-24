package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	"github.com/hecate/agent-runtime/internal/ratelimit"
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
	if response.Choices[0].Message.Content == nil || *response.Choices[0].Message.Content != "Hello!" {
		t.Fatalf("response content = %v, want Hello!", response.Choices[0].Message.Content)
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
	admin := newAPITestClient(t, handler).withBearerToken("admin-secret")

	admin.mustRequest(http.MethodPost, "/admin/retention/run", `{"subsystems":["trace_snapshots"]}`)
	response := mustRequestJSON[RetentionRunsResponse](admin, http.MethodGet, "/admin/retention/runs?limit=5", "")
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
			DefaultModel: "gpt-4o-mini",
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
	client := newAPITestClient(t, handler)
	payload := mustRequestJSON[TraceResponse](client, http.MethodGet, "/v1/traces?request_id="+chat.Header().Get("X-Request-Id"), "")
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
			DefaultModel: "gpt-4o-mini",
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
	if got := response.Header().Get("X-Runtime-Route-Reason"); got != "provider_default_model_failover" {
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
			DefaultModel: "gpt-4o-mini",
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
	if got := second.Header().Get("X-Runtime-Route-Reason"); got != "provider_default_model" {
		t.Fatalf("second X-Runtime-Route-Reason = %q, want provider_default_model", got)
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
			{Role: "user", Content: strPtr("hello")},
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
			{Role: "user", Content: strPtr("hello")},
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
			{Role: "user", Content: strPtr("hello")},
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
		Router:    router.NewRuleRouter("gpt-4o-mini", providerCatalog),
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
	handler := NewServer(logger, NewHandler(config.Config{}, logger, service, nil, nil))
	client := newAPITestClient(t, handler)
	response := mustRequestJSON[OpenAIModelsResponse](client, http.MethodGet, "/v1/models", "")
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
		Router:    router.NewRuleRouter("gpt-4o-mini", providerCatalog),
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
	handler := NewServer(logger, NewHandler(config.Config{}, logger, service, nil, nil))
	client := newAPITestClient(t, handler)
	response := mustRequestJSON[ProviderStatusResponse](client, http.MethodGet, "/admin/providers", "")
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
	handler := NewServer(logger, NewHandler(config.Config{}, logger, nil, nil, nil))
	client := newAPITestClient(t, handler)
	response := mustRequestJSON[ProviderPresetResponse](client, http.MethodGet, "/v1/provider-presets", "")
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
	cpStore, err := controlplane.NewFileStore(filepath.Join(t.TempDir(), "control-plane.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if _, err := cpStore.UpsertTenant(context.Background(), controlplane.Tenant{ID: "team-a", Name: "Team A", Enabled: true}); err != nil {
		t.Fatalf("UpsertTenant() error = %v", err)
	}
	if _, err := cpStore.UpsertAPIKey(context.Background(), controlplane.APIKey{
		ID:      "team-a",
		Name:    "team-a",
		Key:     "tenant-secret",
		Tenant:  "team-a",
		Role:    "tenant",
		Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertAPIKey() error = %v", err)
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
	}, governor.NewMemoryBudgetStore(), cpStore)
	tenantClient := newAPITestClient(t, handler).withBearerToken("tenant-secret")
	tenantClient.mustRequestStatus(http.StatusUnauthorized, http.MethodGet, "/admin/budget", "")
}

func TestChatCompletionAPIKeyRejectsTenantImpersonation(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{name: "openai"}
	cpStore, err := controlplane.NewFileStore(filepath.Join(t.TempDir(), "control-plane.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if _, err := cpStore.UpsertTenant(context.Background(), controlplane.Tenant{ID: "team-a", Name: "Team A", Enabled: true}); err != nil {
		t.Fatalf("UpsertTenant() error = %v", err)
	}
	if _, err := cpStore.UpsertAPIKey(context.Background(), controlplane.APIKey{
		ID:      "team-a",
		Name:    "team-a",
		Key:     "tenant-secret",
		Tenant:  "team-a",
		Role:    "tenant",
		Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertAPIKey() error = %v", err)
	}
	handler := newTestHTTPHandlerWithControlPlane(logger, []providers.Provider{provider}, config.Config{}, cpStore)
	tenantClient := newAPITestClient(t, handler).withBearerToken("tenant-secret")
	tenantClient.mustRequestStatus(http.StatusForbidden, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-4o-mini","user":"team-b","messages":[{"role":"user","content":"hello"}]}`)
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
	cpStore, err := controlplane.NewFileStore(filepath.Join(t.TempDir(), "control-plane.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if _, err := cpStore.UpsertTenant(context.Background(), controlplane.Tenant{ID: "team-a", Name: "Team A", Enabled: true}); err != nil {
		t.Fatalf("UpsertTenant() error = %v", err)
	}
	if _, err := cpStore.UpsertAPIKey(context.Background(), controlplane.APIKey{
		ID:               "team-a",
		Name:             "team-a",
		Key:              "tenant-secret",
		Tenant:           "team-a",
		Role:             "tenant",
		AllowedProviders: []string{"ollama"},
		AllowedModels:    []string{"llama3.1:8b"},
		Enabled:          true,
	}); err != nil {
		t.Fatalf("UpsertAPIKey() error = %v", err)
	}
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(time.Minute),
		Router:    router.NewRuleRouter("gpt-4o-mini", providerCatalog),
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
	handler := NewServer(logger, NewHandler(config.Config{}, logger, service, cpStore, nil))
	tenantClient := newAPITestClient(t, handler).withBearerToken("tenant-secret")
	response := mustRequestJSON[OpenAIModelsResponse](tenantClient, http.MethodGet, "/v1/models", "")
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
			client := newAPITestClient(t, handler)
			if tc.token != "" {
				client = client.withBearerToken(tc.token)
			}
			response := mustRequestJSONStatus[SessionResponse](client, tc.wantStatus, http.MethodGet, "/v1/whoami", "")
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
	admin := newAPITestClient(t, handler).withBearerToken("admin-secret")
	admin.mustRequest(http.MethodPost, "/admin/control-plane/tenants", `{"name":"Team A","description":"Primary tenant","allowed_providers":["ollama"],"enabled":true}`)
	admin.mustRequest(http.MethodPost, "/admin/control-plane/api-keys", `{"name":"Team A Dev","key":"hecate-team-a-dev","tenant":"team-a","role":"tenant","allowed_models":["llama3.1:8b"],"enabled":true}`)
	response := mustRequestJSON[ControlPlaneResponse](admin, http.MethodGet, "/admin/control-plane", "")
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

func TestControlPlanePolicyAndPricebookCRUD(t *testing.T) {
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
	}, governor.NewMemoryBudgetStore(), store)
	admin := newAPITestClient(t, handler).withBearerToken("admin-secret")
	admin.mustRequest(http.MethodPost, "/admin/control-plane/policy-rules", `{"id":"deny-cloud","action":"deny","reason":"cloud denied","provider_kinds":["cloud"]}`)
	admin.mustRequest(http.MethodPost, "/admin/control-plane/pricebook", `{"provider":"openai","model":"custom-model","input_micros_usd_per_million_tokens":100000,"output_micros_usd_per_million_tokens":200000}`)
	response := mustRequestJSON[ControlPlaneResponse](admin, http.MethodGet, "/admin/control-plane", "")
	if len(response.Data.PolicyRules) != 1 {
		t.Fatalf("policy rule count = %d, want 1", len(response.Data.PolicyRules))
	}
	if response.Data.PolicyRules[0].ID != "deny-cloud" {
		t.Fatalf("policy rule id = %q, want deny-cloud", response.Data.PolicyRules[0].ID)
	}
	if len(response.Data.Pricebook) != 1 {
		t.Fatalf("pricebook count = %d, want 1", len(response.Data.Pricebook))
	}
	if response.Data.Pricebook[0].Provider != "openai" || response.Data.Pricebook[0].Model != "custom-model" {
		t.Fatalf("pricebook entry = %s/%s, want openai/custom-model", response.Data.Pricebook[0].Provider, response.Data.Pricebook[0].Model)
	}

	admin.mustRequest(http.MethodPost, "/admin/control-plane/policy-rules/delete", `{"id":"deny-cloud"}`)
	admin.mustRequest(http.MethodPost, "/admin/control-plane/pricebook/delete", `{"provider":"openai","model":"custom-model"}`)
}

func TestControlPlaneStatusIncludesProviderPresetInheritanceMetadata(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	store, err := controlplane.NewFileStore(filepath.Join(t.TempDir(), "control-plane.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if _, err := store.UpsertProvider(context.Background(), controlplane.Provider{
		Name:           "groq",
		PresetID:       "groq",
		Kind:           "cloud",
		Protocol:       "openai",
		BaseURL:        "https://api.groq.com/openai/v1",
		DefaultModel:   "openai/gpt-oss-20b",
		ExplicitFields: []string{"default_model"},
		Enabled:        true,
	}, &controlplane.ProviderSecret{
		ProviderID:      "groq",
		APIKeyEncrypted: "encrypted",
		APIKeyPreview:   "gr...ret",
	}); err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}

	handler := newBudgetTestHandlerWithConfig(logger, config.Config{
		Server: config.ServerConfig{
			AuthToken: "admin-secret",
		},
	}, governor.NewMemoryBudgetStore(), store)
	admin := newAPITestClient(t, handler).withBearerToken("admin-secret")
	response := mustRequestJSON[ControlPlaneResponse](admin, http.MethodGet, "/admin/control-plane", "")
	if len(response.Data.Providers) != 1 {
		t.Fatalf("provider count = %d, want 1", len(response.Data.Providers))
	}
	got := response.Data.Providers[0]
	if got.PresetID != "groq" {
		t.Fatalf("preset_id = %q, want groq", got.PresetID)
	}
	if len(got.ExplicitFields) != 1 || got.ExplicitFields[0] != "default_model" {
		t.Fatalf("explicit_fields = %#v, want [default_model]", got.ExplicitFields)
	}
	if len(got.InheritedFields) == 0 {
		t.Fatal("inherited_fields = empty, want inherited built-in defaults")
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
	admin := newAPITestClient(t, handler).withBearerToken("admin-secret")
	admin.mustRequest(http.MethodPost, "/admin/control-plane/tenants", `{"name":"Team A","enabled":true}`)
	admin.mustRequest(http.MethodPost, "/admin/control-plane/api-keys", `{"name":"Team A Dev","key":"secret","tenant":"team-a","role":"tenant","enabled":true}`)
	admin.mustRequest(http.MethodPost, "/admin/control-plane/tenants/enabled", `{"id":"team-a","enabled":false}`)
	admin.mustRequest(http.MethodPost, "/admin/control-plane/api-keys/enabled", `{"id":"team-a-dev","enabled":false}`)
	admin.mustRequest(http.MethodPost, "/admin/control-plane/api-keys/rotate", `{"id":"team-a-dev","key":"new-secret"}`)
	admin.mustRequestStatus(http.StatusBadRequest, http.MethodPost, "/admin/control-plane/tenants/delete", `{"id":"team-a"}`)
	admin.mustRequest(http.MethodPost, "/admin/control-plane/api-keys/delete", `{"id":"team-a-dev"}`)
	admin.mustRequest(http.MethodPost, "/admin/control-plane/tenants/delete", `{"id":"team-a"}`)
	response := mustRequestJSON[ControlPlaneResponse](admin, http.MethodGet, "/admin/control-plane", "")
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

	client := newAPITestClient(t, handler)
	response := mustRequestJSON[BudgetStatusResponse](client, http.MethodGet, "/admin/budget", "")
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

	client := newAPITestClient(t, handler)
	response := mustRequestJSON[BudgetStatusResponse](client, http.MethodPost, "/admin/budget/reset", `{"key":"team-a"}`)
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

	client := newAPITestClient(t, handler)
	response := mustRequestJSON[BudgetStatusResponse](client, http.MethodGet, "/admin/budget?scope=tenant_provider&tenant=team-a&provider=ollama", "")
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

	client := newAPITestClient(t, handler)
	topUpResponse := mustRequestJSON[BudgetStatusResponse](client, http.MethodPost, "/admin/budget/topup", `{"scope":"tenant_provider","tenant":"team-a","provider":"ollama","amount_micros_usd":2000000}`)
	if topUpResponse.Data.BalanceMicrosUSD != 2_000_000 {
		t.Fatalf("topup balance_micros_usd = %d, want 2000000", topUpResponse.Data.BalanceMicrosUSD)
	}
	if topUpResponse.Data.BalanceSource != "store" {
		t.Fatalf("topup balance_source = %q, want store", topUpResponse.Data.BalanceSource)
	}

	limitResponse := mustRequestJSON[BudgetStatusResponse](client, http.MethodPost, "/admin/budget/limit", `{"scope":"tenant_provider","tenant":"team-a","provider":"ollama","balance_micros_usd":500000}`)
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

	client := newAPITestClient(t, handler)
	response := mustRequestJSON[AccountSummaryResponse](client, http.MethodGet, "/admin/accounts/summary", "")
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

	client := newAPITestClient(t, handler)
	response := mustRequestJSON[RequestLedgerResponse](client, http.MethodGet, "/admin/requests?limit=1", "")
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
			DefaultModel: "claude-sonnet-4-20250514",
		},
	})
	client := newAPITestClient(t, handler)
	created := mustRequestJSON[ChatSessionResponse](client, http.MethodPost, "/v1/chat/sessions", `{"title":"Claude debugging"}`)
	if created.Data.ID == "" {
		t.Fatal("session id = empty, want session id")
	}

	chatBody := fmt.Sprintf(`{"model":"claude-sonnet-4-20250514","provider":"anthropic","session_id":"%s","messages":[{"role":"user","content":"Say hello."}]}`, created.Data.ID)
	chatRecorder := performJSONRequest(t, handler, chatBody)
	if chatRecorder.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want %d, body=%s", chatRecorder.Code, http.StatusOK, chatRecorder.Body.String())
	}

	session := mustRequestJSON[ChatSessionResponse](client, http.MethodGet, "/v1/chat/sessions/"+created.Data.ID, "")
	if len(session.Data.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(session.Data.Turns))
	}
	if session.Data.Turns[0].Provider != "anthropic" {
		t.Fatalf("provider = %q, want anthropic", session.Data.Turns[0].Provider)
	}
	if session.Data.Turns[0].Model != "claude-sonnet-4-20250514" {
		t.Fatalf("model = %q, want Claude model", session.Data.Turns[0].Model)
	}
	if session.Data.Turns[0].UserMessage.Content == nil || *session.Data.Turns[0].UserMessage.Content != "Say hello." {
		t.Fatalf("user content = %v, want original prompt", session.Data.Turns[0].UserMessage.Content)
	}
}

func TestTasksCreateListAndGet(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", `{"title":"Upgrade TypeScript","prompt":"Upgrade the UI workspace to TypeScript 7 beta.","repo":"hecate","base_branch":"main","workspace_mode":"ephemeral","requested_model":"gpt-5.4-mini","requested_provider":"openai","budget_micros_usd":500000}`)
	if created.Object != "task" {
		t.Fatalf("object = %q, want task", created.Object)
	}
	if created.Data.ID == "" {
		t.Fatal("task id = empty, want task id")
	}
	if created.Data.Status != "queued" {
		t.Fatalf("status = %q, want queued", created.Data.Status)
	}
	if created.Data.Repo != "hecate" {
		t.Fatalf("repo = %q, want hecate", created.Data.Repo)
	}

	listed := mustTaskRequestJSON[TasksResponse](tasks, http.MethodGet, "/v1/tasks?limit=10", "")
	if listed.Object != "tasks" {
		t.Fatalf("object = %q, want tasks", listed.Object)
	}
	if len(listed.Data) != 1 {
		t.Fatalf("tasks = %d, want 1", len(listed.Data))
	}
	if listed.Data[0].ID != created.Data.ID {
		t.Fatalf("listed task id = %q, want %q", listed.Data[0].ID, created.Data.ID)
	}

	fetched := mustTaskRequestJSON[TaskResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID, "")
	if fetched.Data.ID != created.Data.ID {
		t.Fatalf("fetched task id = %q, want %q", fetched.Data.ID, created.Data.ID)
	}
	if fetched.Data.Prompt != "Upgrade the UI workspace to TypeScript 7 beta." {
		t.Fatalf("prompt = %q, want original prompt", fetched.Data.Prompt)
	}

	startRecorder := tasks.mustRequest(http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	if got := startRecorder.Header().Get("X-Trace-Id"); got == "" {
		t.Fatal("X-Trace-Id = empty, want trace id")
	}
	if got := startRecorder.Header().Get("X-Span-Id"); got == "" {
		t.Fatal("X-Span-Id = empty, want span id")
	}

	started := decodeRecorder[TaskRunResponse](t, startRecorder)
	if started.Object != "task_run" {
		t.Fatalf("object = %q, want task_run", started.Object)
	}
	if started.Data.ID == "" {
		t.Fatal("run id = empty, want run id")
	}
	if started.Data.Status != "queued" {
		t.Fatalf("run status = %q, want queued", started.Data.Status)
	}
	completedRun := waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "completed")

	runs := mustTaskRequestJSON[TaskRunsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs", "")
	if len(runs.Data) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs.Data))
	}
	if runs.Data[0].ID != started.Data.ID {
		t.Fatalf("run id = %q, want %q", runs.Data[0].ID, started.Data.ID)
	}

	fetchedRun := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID, "")
	if fetchedRun.Data.ID != started.Data.ID {
		t.Fatalf("fetched run id = %q, want %q", fetchedRun.Data.ID, started.Data.ID)
	}
	if fetchedRun.Data.Status != "completed" {
		t.Fatalf("fetched run status = %q, want completed", fetchedRun.Data.Status)
	}

	steps := mustTaskRequestJSON[TaskStepsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/steps", "")
	if len(steps.Data) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps.Data))
	}
	if steps.Data[0].Kind != "model" {
		t.Fatalf("step kind = %q, want model", steps.Data[0].Kind)
	}

	step := mustTaskRequestJSON[TaskStepResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/steps/"+steps.Data[0].ID, "")
	if step.Data.ID != steps.Data[0].ID {
		t.Fatalf("step id = %q, want %q", step.Data.ID, steps.Data[0].ID)
	}

	artifacts := mustTaskRequestJSON[TaskArtifactsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/artifacts", "")
	if len(artifacts.Data) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(artifacts.Data))
	}
	if artifacts.Data[0].Kind != "summary" {
		t.Fatalf("artifact kind = %q, want summary", artifacts.Data[0].Kind)
	}

	runArtifacts := mustTaskRequestJSON[TaskArtifactsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/artifacts", "")
	if len(runArtifacts.Data) != 1 {
		t.Fatalf("run artifacts = %d, want 1", len(runArtifacts.Data))
	}
	if runArtifacts.Data[0].ID != artifacts.Data[0].ID {
		t.Fatalf("run artifact id = %q, want %q", runArtifacts.Data[0].ID, artifacts.Data[0].ID)
	}

	fetchedAfterStart := waitForTaskStatus(t, handler, created.Data.ID, "completed")
	if fetchedAfterStart.Data.LatestRunID != started.Data.ID {
		t.Fatalf("latest_run_id = %q, want %q", fetchedAfterStart.Data.LatestRunID, started.Data.ID)
	}
	if fetchedAfterStart.Data.StepCount != 1 {
		t.Fatalf("task step_count = %d, want 1", fetchedAfterStart.Data.StepCount)
	}
	if fetchedAfterStart.Data.ArtifactCount != 1 {
		t.Fatalf("task artifact_count = %d, want 1", fetchedAfterStart.Data.ArtifactCount)
	}
	if completedRun.Data.StepCount != 1 {
		t.Fatalf("step_count = %d, want 1", completedRun.Data.StepCount)
	}
	if completedRun.Data.ArtifactCount != 1 {
		t.Fatalf("artifact_count = %d, want 1", completedRun.Data.ArtifactCount)
	}
}

func TestTaskStartShellExecutor(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", `{"title":"Run shell","prompt":"Run a shell command.","execution_kind":"shell","shell_command":"printf 'hello '; sleep 0.2; printf 'from shell\n'","working_directory":".","timeout_ms":2000}`)
	if created.Data.ExecutionKind != "shell" {
		t.Fatalf("execution_kind = %q, want shell", created.Data.ExecutionKind)
	}

	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	if started.Data.Status != "awaiting_approval" {
		t.Fatalf("run status = %q, want awaiting_approval", started.Data.Status)
	}
	if started.Data.ApprovalCount != 1 {
		t.Fatalf("approval_count = %d, want 1", started.Data.ApprovalCount)
	}

	approvals := mustTaskRequestJSON[TaskApprovalsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/approvals", "")
	if len(approvals.Data) != 1 {
		t.Fatalf("approvals = %d, want 1", len(approvals.Data))
	}
	if approvals.Data[0].Status != "pending" {
		t.Fatalf("approval status = %q, want pending", approvals.Data[0].Status)
	}
	if approvals.Data[0].Kind != "shell_command" {
		t.Fatalf("approval kind = %q, want shell_command", approvals.Data[0].Kind)
	}

	approval := mustTaskRequestJSON[TaskApprovalResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/approvals/"+approvals.Data[0].ID, "")
	if approval.Data.ID != approvals.Data[0].ID {
		t.Fatalf("approval id = %q, want %q", approval.Data.ID, approvals.Data[0].ID)
	}

	resolved := mustTaskRequestJSON[TaskApprovalResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/approvals/"+approvals.Data[0].ID+"/resolve", `{"decision":"approve","note":"looks safe"}`)
	if resolved.Data.Status != "approved" {
		t.Fatalf("approval status = %q, want approved", resolved.Data.Status)
	}

	partialArtifacts := waitForRunArtifactsContaining(t, handler, created.Data.ID, started.Data.ID, "stdout", "hello ")
	foundPartial := false
	for _, artifact := range partialArtifacts.Data {
		if artifact.Kind == "stdout" && strings.Contains(artifact.ContentText, "hello ") {
			foundPartial = true
		}
	}
	if !foundPartial {
		t.Fatal("stdout artifact missing streamed partial output")
	}

	completedRun := waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "completed")
	if completedRun.Data.WorkspacePath == "" {
		t.Fatal("workspace_path is empty")
	}
	if completedRun.Data.ArtifactCount != 2 {
		t.Fatalf("artifact_count = %d, want 2", completedRun.Data.ArtifactCount)
	}

	steps := mustTaskRequestJSON[TaskStepsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/steps", "")
	if len(steps.Data) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps.Data))
	}
	if steps.Data[0].Kind != "shell" {
		t.Fatalf("step kind = %q, want shell", steps.Data[0].Kind)
	}
	if steps.Data[0].ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", steps.Data[0].ExitCode)
	}

	runArtifacts := mustTaskRequestJSON[TaskArtifactsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/artifacts", "")
	if len(runArtifacts.Data) != 2 {
		t.Fatalf("run artifacts = %d, want 2", len(runArtifacts.Data))
	}
	foundStdout := false
	for _, artifact := range runArtifacts.Data {
		if artifact.Kind == "stdout" && strings.Contains(artifact.ContentText, "hello from shell") {
			foundStdout = true
		}
	}
	if !foundStdout {
		t.Fatal("stdout artifact missing shell output")
	}
}

func TestTaskRejectApprovalCancelsRun(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", `{"title":"Reject shell","prompt":"Reject a shell command.","execution_kind":"shell","shell_command":"printf 'should not run\n'","working_directory":".","timeout_ms":2000}`)
	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	if started.Data.Status != "awaiting_approval" {
		t.Fatalf("run status = %q, want awaiting_approval", started.Data.Status)
	}

	approvals := mustTaskRequestJSON[TaskApprovalsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/approvals", "")
	if len(approvals.Data) != 1 {
		t.Fatalf("approvals = %d, want 1", len(approvals.Data))
	}

	resolveRecorder := tasks.mustRequest(http.MethodPost, "/v1/tasks/"+created.Data.ID+"/approvals/"+approvals.Data[0].ID+"/resolve", `{"decision":"reject","note":"not safe"}`)
	if got := resolveRecorder.Header().Get("X-Trace-Id"); got == "" {
		t.Fatal("X-Trace-Id = empty, want trace id")
	}
	if got := resolveRecorder.Header().Get("X-Span-Id"); got == "" {
		t.Fatal("X-Span-Id = empty, want span id")
	}

	resolved := decodeRecorder[TaskApprovalResponse](t, resolveRecorder)
	if resolved.Data.Status != "rejected" {
		t.Fatalf("approval status = %q, want rejected", resolved.Data.Status)
	}
	if resolved.Data.ResolutionNote != "not safe" {
		t.Fatalf("resolution_note = %q, want not safe", resolved.Data.ResolutionNote)
	}

	cancelledRun := waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "cancelled")
	if cancelledRun.Data.LastError != "approval rejected" {
		t.Fatalf("run last_error = %q, want approval rejected", cancelledRun.Data.LastError)
	}

	cancelledTask := waitForTaskStatus(t, handler, created.Data.ID, "cancelled")
	if cancelledTask.Data.LastError != "approval rejected" {
		t.Fatalf("task last_error = %q, want approval rejected", cancelledTask.Data.LastError)
	}
	if cancelledTask.Data.LatestRunID != started.Data.ID {
		t.Fatalf("latest_run_id = %q, want %q", cancelledTask.Data.LatestRunID, started.Data.ID)
	}

	steps := mustTaskRequestJSON[TaskStepsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/steps", "")
	if len(steps.Data) != 0 {
		t.Fatalf("steps = %d, want 0", len(steps.Data))
	}
}

func TestTaskStartFileExecutor(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tempDir := t.TempDir()
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", fmt.Sprintf(`{"title":"Write file","prompt":"Write a file.","execution_kind":"file","file_operation":"write","file_path":"note.txt","file_content":"hello file","working_directory":%q}`, tempDir))
	if created.Data.ExecutionKind != "file" {
		t.Fatalf("execution_kind = %q, want file", created.Data.ExecutionKind)
	}

	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	if started.Data.Status != "queued" {
		t.Fatalf("run status = %q, want queued", started.Data.Status)
	}
	if started.Data.WorkspacePath == "" {
		t.Fatal("workspace_path is empty")
	}
	waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "completed")

	steps := mustTaskRequestJSON[TaskStepsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/steps", "")
	if len(steps.Data) != 1 || steps.Data[0].Kind != "file" {
		t.Fatalf("steps = %#v, want one file step", steps.Data)
	}

	content, err := os.ReadFile(filepath.Join(started.Data.WorkspacePath, "note.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "hello file" {
		t.Fatalf("file contents = %q, want hello file", string(content))
	}
}

func TestTaskStartGitExecutor(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tempDir := t.TempDir()
	tasks := newTaskTestClient(t, handler)

	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v, output=%s", err, string(output))
	}

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", fmt.Sprintf(`{"title":"Run git","prompt":"Run a git command.","execution_kind":"git","git_command":"status --short","working_directory":%q,"timeout_ms":2000}`, tempDir))
	if created.Data.ExecutionKind != "git" {
		t.Fatalf("execution_kind = %q, want git", created.Data.ExecutionKind)
	}

	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	if started.Data.Status != "queued" {
		t.Fatalf("run status = %q, want queued", started.Data.Status)
	}
	if started.Data.WorkspacePath == "" {
		t.Fatal("workspace_path is empty")
	}
	waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "completed")

	steps := mustTaskRequestJSON[TaskStepsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/steps", "")
	if len(steps.Data) != 1 || steps.Data[0].Kind != "git" {
		t.Fatalf("steps = %#v, want one git step", steps.Data)
	}

	artifacts := mustTaskRequestJSON[TaskArtifactsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/artifacts", "")
	if len(artifacts.Data) != 2 {
		t.Fatalf("artifacts = %d, want 2", len(artifacts.Data))
	}
}

func TestTaskApprovedShellExecutorRespectsReadOnlyPolicy(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", `{"title":"Denied shell","prompt":"Attempt a write.","execution_kind":"shell","shell_command":"touch denied.txt","working_directory":".","sandbox_read_only":true,"timeout_ms":2000}`)
	if !created.Data.SandboxReadOnly {
		t.Fatal("sandbox_read_only = false, want true")
	}

	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	if started.Data.Status != "awaiting_approval" {
		t.Fatalf("run status = %q, want awaiting_approval", started.Data.Status)
	}

	approvals := mustTaskRequestJSON[TaskApprovalsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/approvals", "")
	if len(approvals.Data) != 1 {
		t.Fatalf("approvals = %d, want 1", len(approvals.Data))
	}

	tasks.mustRequest(http.MethodPost, "/v1/tasks/"+created.Data.ID+"/approvals/"+approvals.Data[0].ID+"/resolve", `{"decision":"approve"}`)

	failedRun := waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "failed")
	if failedRun.Data.Status != "failed" {
		t.Fatalf("run status = %q, want failed", failedRun.Data.Status)
	}

	steps := mustTaskRequestJSON[TaskStepsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/steps", "")
	if len(steps.Data) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps.Data))
	}
	if steps.Data[0].ErrorKind != "sandbox_policy_denied" {
		t.Fatalf("error_kind = %q, want sandbox_policy_denied", steps.Data[0].ErrorKind)
	}
}

func TestTaskStartFileExecutorRespectsAllowedRoot(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tempDir := t.TempDir()
	workingDirectory := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workingDirectory, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", fmt.Sprintf(`{"title":"Escape root","prompt":"Try escaping allowed root.","execution_kind":"file","file_operation":"write","file_path":"../outside.txt","file_content":"blocked","working_directory":%q,"sandbox_allowed_root":%q}`, workingDirectory, workingDirectory))
	if created.Data.SandboxAllowedRoot != workingDirectory {
		t.Fatalf("sandbox_allowed_root = %q, want %q", created.Data.SandboxAllowedRoot, workingDirectory)
	}

	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	if started.Data.Status != "queued" {
		t.Fatalf("run status = %q, want queued", started.Data.Status)
	}
	waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "failed")

	steps := mustTaskRequestJSON[TaskStepsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/steps", "")
	if len(steps.Data) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps.Data))
	}
	if steps.Data[0].ErrorKind != "sandbox_policy_denied" {
		t.Fatalf("error_kind = %q, want sandbox_policy_denied", steps.Data[0].ErrorKind)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "outside.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside.txt exists or unexpected stat error: %v", err)
	}
}

func TestTaskRunCancellation(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", `{"title":"Cancel shell","prompt":"Cancel a long shell run.","execution_kind":"shell","shell_command":"printf 'starting\n'; sleep 5; printf 'done\n'","working_directory":".","timeout_ms":10000}`)
	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	approvals := mustTaskRequestJSON[TaskApprovalsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/approvals", "")
	tasks.mustRequest(http.MethodPost, "/v1/tasks/"+created.Data.ID+"/approvals/"+approvals.Data[0].ID+"/resolve", `{"decision":"approve"}`)

	waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "running")

	tasks.mustRequest(http.MethodPost, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/cancel", "")

	cancelledRun := waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "cancelled")
	if cancelledRun.Data.Status != "cancelled" {
		t.Fatalf("run status = %q, want cancelled", cancelledRun.Data.Status)
	}

	steps := waitForRunStepStatus(t, handler, created.Data.ID, started.Data.ID, "cancelled")
	if len(steps.Data) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps.Data))
	}
	if steps.Data[0].Status != "cancelled" {
		t.Fatalf("step status = %q, want cancelled", steps.Data[0].Status)
	}
}

func TestTaskRunStreamSSE(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	server := httptest.NewServer(handler)
	defer server.Close()

	createResp := postJSONToURL(t, server.URL+"/v1/tasks", `{"title":"Stream shell","prompt":"Stream a shell command.","execution_kind":"shell","shell_command":"printf 'hello '; sleep 0.3; printf 'stream\n'","working_directory":".","timeout_ms":3000}`)
	if createResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("create status = %d, want %d, body=%s", createResp.StatusCode, http.StatusOK, string(body))
	}
	var created TaskResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	createResp.Body.Close()

	startResp := postJSONToURL(t, server.URL+"/v1/tasks/"+created.Data.ID+"/start", "")
	if startResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(startResp.Body)
		t.Fatalf("start status = %d, want %d, body=%s", startResp.StatusCode, http.StatusOK, string(body))
	}
	var started TaskRunResponse
	if err := json.NewDecoder(startResp.Body).Decode(&started); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	startResp.Body.Close()

	approvalsResp, err := http.Get(server.URL + "/v1/tasks/" + created.Data.ID + "/approvals")
	if err != nil {
		t.Fatalf("Get approvals error = %v", err)
	}
	if approvalsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(approvalsResp.Body)
		t.Fatalf("approvals status = %d, want %d, body=%s", approvalsResp.StatusCode, http.StatusOK, string(body))
	}
	var approvals TaskApprovalsResponse
	if err := json.NewDecoder(approvalsResp.Body).Decode(&approvals); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	approvalsResp.Body.Close()

	streamReq, err := http.NewRequest(http.MethodGet, server.URL+"/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/stream", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	streamCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	streamReq = streamReq.WithContext(streamCtx)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("stream request error = %v", err)
	}
	defer streamResp.Body.Close()
	if got := streamResp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}

	resolveErrCh := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		resolveResp, err := http.Post(server.URL+"/v1/tasks/"+created.Data.ID+"/approvals/"+approvals.Data[0].ID+"/resolve", "application/json", strings.NewReader(`{"decision":"approve"}`))
		if err != nil {
			resolveErrCh <- err
			return
		}
		defer resolveResp.Body.Close()
		io.Copy(io.Discard, resolveResp.Body)
		if resolveResp.StatusCode != http.StatusOK {
			resolveErrCh <- fmt.Errorf("resolve status = %d", resolveResp.StatusCode)
			return
		}
		resolveErrCh <- nil
	}()

	var sawAwaitingApproval bool
	var sawPartialStdout bool
	var sawTerminal bool
	for event := range readSSEEvents(t, streamResp.Body) {
		if event.Event != "snapshot" && event.Event != "done" {
			continue
		}
		var payload TaskRunStreamEventResponse
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if payload.Data.Run.Status == "awaiting_approval" {
			sawAwaitingApproval = true
		}
		for _, artifact := range payload.Data.Artifacts {
			if artifact.Kind == "stdout" && strings.Contains(artifact.ContentText, "hello ") && !strings.Contains(artifact.ContentText, "stream\n") {
				sawPartialStdout = true
			}
		}
		if payload.Data.Terminal || types.IsTerminalTaskRunStatus(payload.Data.Run.Status) {
			sawTerminal = true
		}
		if event.Event == "done" {
			break
		}
	}

	if !sawAwaitingApproval {
		t.Fatal("did not observe awaiting_approval stream snapshot")
	}
	if !sawPartialStdout {
		t.Fatal("did not observe partial stdout in stream snapshot")
	}
	if !sawTerminal {
		t.Fatal("did not observe terminal stream snapshot")
	}
	if err := <-resolveErrCh; err != nil {
		t.Fatalf("approval resolve error = %v", err)
	}
}

func TestTaskRunStreamResumeWithAfterSequence(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	server := httptest.NewServer(handler)
	defer server.Close()

	createResp := postJSONToURL(t, server.URL+"/v1/tasks", `{"title":"Resume stream","prompt":"Create resumable stream task."}`)
	var created TaskResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	createResp.Body.Close()

	started := mustRequestJSON[TaskRunResponse](newAPITestClient(t, handler), http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "completed")

	events := mustRequestJSON[TaskRunEventsResponse](newAPITestClient(t, handler), http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/events", "")
	if len(events.Data) == 0 {
		t.Fatal("events = 0, want at least one")
	}
	afterSequence := events.Data[len(events.Data)-1].Sequence

	streamReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v1/tasks/%s/runs/%s/stream?after_sequence=%d", server.URL, created.Data.ID, started.Data.ID, afterSequence), nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	streamCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	streamReq = streamReq.WithContext(streamCtx)

	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("stream request error = %v", err)
	}
	defer streamResp.Body.Close()

	var sawDone bool
	for event := range readSSEEvents(t, streamResp.Body) {
		if event.Event != "snapshot" && event.Event != "done" {
			continue
		}
		var payload TaskRunStreamEventResponse
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if payload.Data.Sequence <= int(afterSequence) {
			t.Fatalf("sequence = %d, want > %d", payload.Data.Sequence, afterSequence)
		}
		if event.Event == "done" {
			sawDone = true
			if !payload.Data.Terminal {
				t.Fatal("done payload terminal = false, want true")
			}
			break
		}
	}
	if !sawDone {
		t.Fatal("did not observe done event after stream resume")
	}
}

func TestTaskRunStreamResumeWithLastEventID(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	server := httptest.NewServer(handler)
	defer server.Close()

	createResp := postJSONToURL(t, server.URL+"/v1/tasks", `{"title":"Resume stream header","prompt":"Use Last-Event-ID."}`)
	var created TaskResponse
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	createResp.Body.Close()

	started := mustRequestJSON[TaskRunResponse](newAPITestClient(t, handler), http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "completed")

	events := mustRequestJSON[TaskRunEventsResponse](newAPITestClient(t, handler), http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/events", "")
	if len(events.Data) == 0 {
		t.Fatal("events = 0, want at least one")
	}
	last := events.Data[len(events.Data)-1].Sequence

	streamReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v1/tasks/%s/runs/%s/stream", server.URL, created.Data.ID, started.Data.ID), nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	streamReq.Header.Set("Last-Event-ID", strconv.FormatInt(last, 10))
	streamCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	streamReq = streamReq.WithContext(streamCtx)

	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("stream request error = %v", err)
	}
	defer streamResp.Body.Close()

	for event := range readSSEEvents(t, streamResp.Body) {
		if event.Event != "snapshot" && event.Event != "done" {
			continue
		}
		var payload TaskRunStreamEventResponse
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if payload.Data.Sequence <= int(last) {
			t.Fatalf("sequence = %d, want > %d", payload.Data.Sequence, last)
		}
		if event.Event == "done" {
			return
		}
	}
	t.Fatal("did not observe done event for Last-Event-ID resume")
}

func TestTaskRunEventsAppendAndList(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", `{"title":"Event run","prompt":"Run with events."}`)
	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "completed")

	initial := mustTaskRequestJSON[TaskRunEventsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/events", "")
	if len(initial.Data) == 0 {
		t.Fatal("events = 0, want at least one event")
	}
	baseSequence := initial.Data[len(initial.Data)-1].Sequence

	appendRecorder := tasks.mustRequest(
		http.MethodPost,
		"/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/events",
		`{"event_type":"external.tool_result","step_id":"step_external","status":"ok","note":"client injected event","data":{"tool":"lint","result":"ok"}}`,
	)
	var appended map[string]any
	if err := json.NewDecoder(appendRecorder.Body).Decode(&appended); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	after := mustTaskRequestJSON[TaskRunEventsResponse](tasks, http.MethodGet, fmt.Sprintf("/v1/tasks/%s/runs/%s/events?after_sequence=%d", created.Data.ID, started.Data.ID, baseSequence), "")
	foundExternal := false
	for _, event := range after.Data {
		if event.Sequence <= baseSequence {
			t.Fatalf("event sequence = %d, want > %d", event.Sequence, baseSequence)
		}
		if event.EventType == "external.tool_result" {
			foundExternal = true
		}
	}
	if !foundExternal {
		t.Fatal("missing appended external.tool_result event")
	}
}

func TestTaskRunRetryCreatesNewAttempt(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", `{"title":"Retry run","prompt":"Trigger retry flow."}`)
	first := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	waitForRunStatus(t, handler, created.Data.ID, first.Data.ID, "completed")

	retried := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/runs/"+first.Data.ID+"/retry", `{}`)
	if retried.Data.ID == first.Data.ID {
		t.Fatal("retry run id matches original run id")
	}
	waitForRunStatus(t, handler, created.Data.ID, retried.Data.ID, "completed")

	runs := mustTaskRequestJSON[TaskRunsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs", "")
	if len(runs.Data) < 2 {
		t.Fatalf("runs = %d, want at least 2", len(runs.Data))
	}
}

func TestTaskRunResumeFromCancelledRun(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", `{"title":"Resume shell","prompt":"Resume cancelled shell run.","execution_kind":"shell","shell_command":"printf 'resume'\n","working_directory":".","timeout_ms":1000}`)
	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	approvals := mustTaskRequestJSON[TaskApprovalsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/approvals", "")
	tasks.mustRequest(http.MethodPost, "/v1/tasks/"+created.Data.ID+"/approvals/"+approvals.Data[0].ID+"/resolve", `{"decision":"reject","note":"force cancellation for resume test"}`)
	waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "cancelled")

	resumed := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/resume", `{}`)
	if resumed.Data.ID == started.Data.ID {
		t.Fatal("resume returned original run id, want new run id")
	}
	if resumed.Data.Status != "awaiting_approval" && resumed.Data.Status != "queued" {
		t.Fatalf("resume status = %q, want awaiting_approval or queued", resumed.Data.Status)
	}
}

func TestTaskRunArtifactFetchByID(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	tasks := newTaskTestClient(t, handler)

	created := mustTaskRequestJSON[TaskResponse](tasks, http.MethodPost, "/v1/tasks", `{"title":"Artifact fetch","prompt":"Produce an artifact."}`)
	started := mustTaskRequestJSON[TaskRunResponse](tasks, http.MethodPost, "/v1/tasks/"+created.Data.ID+"/start", "")
	waitForRunStatus(t, handler, created.Data.ID, started.Data.ID, "completed")

	runArtifacts := mustTaskRequestJSON[TaskArtifactsResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/artifacts", "")
	if len(runArtifacts.Data) == 0 {
		t.Fatal("run artifacts = 0, want at least one")
	}
	artifactID := runArtifacts.Data[0].ID
	fetched := mustTaskRequestJSON[TaskArtifactResponse](tasks, http.MethodGet, "/v1/tasks/"+created.Data.ID+"/runs/"+started.Data.ID+"/artifacts/"+artifactID, "")
	if fetched.Data.ID != artifactID {
		t.Fatalf("artifact id = %q, want %q", fetched.Data.ID, artifactID)
	}
}

type apiTestClient struct {
	t       *testing.T
	handler http.Handler
	headers map[string]string
}

func newAPITestClient(t *testing.T, handler http.Handler) apiTestClient {
	t.Helper()
	return apiTestClient{t: t, handler: handler}
}

func (c apiTestClient) withBearerToken(token string) apiTestClient {
	c.t.Helper()
	if strings.TrimSpace(token) == "" {
		return c
	}
	if c.headers == nil {
		c.headers = make(map[string]string, 1)
	}
	c.headers["Authorization"] = "Bearer " + token
	return c
}

func (c apiTestClient) mustRequest(method, path, body string) *httptest.ResponseRecorder {
	return c.mustRequestStatus(http.StatusOK, method, path, body)
}

func (c apiTestClient) mustRequestStatus(status int, method, path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	recorder := performRequestWithHeaders(c.t, c.handler, method, path, body, c.headers)
	if recorder.Code != status {
		c.t.Fatalf("%s %s status = %d, want %d, body=%s", method, path, recorder.Code, status, recorder.Body.String())
	}
	return recorder
}

func mustRequestJSON[T any](client apiTestClient, method, path, body string) T {
	client.t.Helper()
	return decodeRecorder[T](client.t, client.mustRequest(method, path, body))
}

func mustRequestJSONStatus[T any](client apiTestClient, status int, method, path, body string) T {
	client.t.Helper()
	return decodeRecorder[T](client.t, client.mustRequestStatus(status, method, path, body))
}

type taskTestClient = apiTestClient

func newTaskTestClient(t *testing.T, handler http.Handler) taskTestClient {
	t.Helper()
	return newAPITestClient(t, handler)
}

func mustTaskRequestJSON[T any](client taskTestClient, method, path, body string) T {
	client.t.Helper()
	return mustRequestJSON[T](client, method, path, body)
}

func waitForRunStatus(t *testing.T, handler http.Handler, taskID, runID string, statuses ...string) TaskRunResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		recorder := performRequest(t, handler, http.MethodGet, "/v1/tasks/"+taskID+"/runs/"+runID, "")
		if recorder.Code == http.StatusOK {
			run, ok := tryDecodeRecorder[TaskRunResponse](recorder)
			if ok && containsStatus(run.Data.Status, statuses...) {
				return run
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for run %s to reach one of %v", runID, statuses)
	return TaskRunResponse{}
}

func waitForTaskStatus(t *testing.T, handler http.Handler, taskID string, statuses ...string) TaskResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		recorder := performRequest(t, handler, http.MethodGet, "/v1/tasks/"+taskID, "")
		if recorder.Code == http.StatusOK {
			task, ok := tryDecodeRecorder[TaskResponse](recorder)
			if ok && containsStatus(task.Data.Status, statuses...) {
				return task
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for task %s to reach one of %v", taskID, statuses)
	return TaskResponse{}
}

func waitForRunArtifactsContaining(t *testing.T, handler http.Handler, taskID, runID, kind, contains string) TaskArtifactsResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		recorder := performRequest(t, handler, http.MethodGet, "/v1/tasks/"+taskID+"/runs/"+runID+"/artifacts", "")
		if recorder.Code == http.StatusOK {
			artifacts, ok := tryDecodeRecorder[TaskArtifactsResponse](recorder)
			if ok {
				for _, artifact := range artifacts.Data {
					if artifact.Kind == kind && strings.Contains(artifact.ContentText, contains) {
						return artifacts
					}
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s artifact to contain %q", kind, contains)
	return TaskArtifactsResponse{}
}

func waitForRunStepStatus(t *testing.T, handler http.Handler, taskID, runID string, status string) TaskStepsResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		recorder := performRequest(t, handler, http.MethodGet, "/v1/tasks/"+taskID+"/runs/"+runID+"/steps", "")
		if recorder.Code == http.StatusOK {
			steps, ok := tryDecodeRecorder[TaskStepsResponse](recorder)
			if ok && len(steps.Data) > 0 && steps.Data[0].Status == status {
				return steps
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for run %s step to reach status %q", runID, status)
	return TaskStepsResponse{}
}

func containsStatus(status string, statuses ...string) bool {
	for _, candidate := range statuses {
		if status == candidate {
			return true
		}
	}
	return false
}

type sseEvent struct {
	ID    string
	Event string
	Data  string
}

func readSSEEvents(t *testing.T, body io.Reader) <-chan sseEvent {
	t.Helper()
	events := make(chan sseEvent)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
		var current sseEvent
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if current.Event != "" || current.Data != "" {
					events <- current
					current = sseEvent{}
				}
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				current.Event = strings.TrimPrefix(line, "event: ")
				continue
			}
			if strings.HasPrefix(line, "id: ") {
				current.ID = strings.TrimPrefix(line, "id: ")
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				if current.Data != "" {
					current.Data += "\n"
				}
				current.Data += strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return events
}

func postJSONToURL(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Post(%s) error = %v", url, err)
	}
	return resp
}

func newTestHTTPHandler(logger *slog.Logger, provider providers.Provider) http.Handler {
	return newTestHTTPHandlerWithConfig(logger, provider, config.Config{})
}

func newTestHTTPHandlerWithConfig(logger *slog.Logger, provider providers.Provider, cfg config.Config) http.Handler {
	return newTestHTTPHandlerForProviders(logger, []providers.Provider{provider}, cfg)
}

func newTestHTTPHandlerForProviders(logger *slog.Logger, items []providers.Provider, cfg config.Config) http.Handler {
	return newTestHTTPHandlerWithControlPlane(logger, items, cfg, nil)
}

func newTestHTTPHandlerWithControlPlane(logger *slog.Logger, items []providers.Provider, cfg config.Config, cpStore controlplane.Store) http.Handler {
	registry := providers.NewRegistry(items...)
	healthTracker := providers.NewMemoryHealthTracker(cfg.Provider.HealthThreshold, cfg.Provider.HealthCooldown)
	providerCatalog := catalog.NewRegistryCatalog(registry, healthTracker)
	budgetStore := governor.NewMemoryBudgetStore()
	governorCfg := mergeGovernorDefaults(cfg.Governor)
	routerCfg := cfg.Router
	if routerCfg.DefaultModel == "" && len(items) > 0 {
		routerCfg.DefaultModel = items[0].DefaultModel()
	}
	routerEngine := router.NewRuleRouter(routerCfg.DefaultModel, providerCatalog)
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
	handler := NewHandler(cfg, logger, service, cpStore, nil)
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
		Router:    router.NewRuleRouter("gpt-4o-mini", providerCatalog),
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

	handler := NewHandler(cfg, logger, service, cpStore, nil)
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
	return performRequest(t, handler, http.MethodPost, "/v1/chat/completions", body)
}

func performRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return performRequestWithHeaders(t, handler, method, path, body, nil)
}

func performRequestWithHeaders(t *testing.T, handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var requestBody io.Reader
	if body != "" {
		requestBody = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, requestBody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeRecorder[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	payload, ok := tryDecodeRecorder[T](recorder)
	if !ok {
		t.Fatalf("Decode() error for body %q", recorder.Body.String())
	}
	return payload
}

func tryDecodeRecorder[T any](recorder *httptest.ResponseRecorder) (T, bool) {
	var payload T
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&payload); err != nil {
		return payload, false
	}
	return payload, true
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

func strPtr(s string) *string { return &s }

func TestRateLimitHeadersSetOnSuccess(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID:    "chatcmpl-rl1",
			Model: "gpt-4o-mini",
			Choices: []types.ChatChoice{
				{Message: types.Message{Role: "assistant", Content: "hi"}, FinishReason: "stop"},
			},
			Usage: types.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		},
	}
	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Server: config.ServerConfig{
			RateLimit: config.RateLimitConfig{
				Enabled:           true,
				RequestsPerMinute: 10,
				BurstSize:         10,
			},
		},
	})

	rec := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if h := rec.Header().Get("X-RateLimit-Limit"); h != "10" {
		t.Errorf("X-RateLimit-Limit = %q, want \"10\"", h)
	}
	remaining, err := strconv.Atoi(rec.Header().Get("X-RateLimit-Remaining"))
	if err != nil {
		t.Fatalf("X-RateLimit-Remaining not numeric: %v", err)
	}
	if remaining < 0 || remaining > 9 {
		t.Errorf("X-RateLimit-Remaining = %d, want 0-9", remaining)
	}
	if rec.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("X-RateLimit-Reset header missing")
	}
}

func TestRateLimitReturns429WhenExhausted(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID:    "chatcmpl-rl2",
			Model: "gpt-4o-mini",
			Choices: []types.ChatChoice{
				{Message: types.Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"},
			},
			Usage: types.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
		},
	}
	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Server: config.ServerConfig{
			RateLimit: config.RateLimitConfig{
				Enabled:           true,
				RequestsPerMinute: 2,
				BurstSize:         2,
			},
		},
	})

	body := `{"model":"gpt-4o-mini","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`
	// Drain the bucket.
	for i := 0; i < 2; i++ {
		rec := performJSONRequest(t, handler, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d, want 200", i+1, rec.Code)
		}
	}
	// Third call should be rate-limited.
	rec := performJSONRequest(t, handler, body)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
}

func TestRateLimitDisabledByDefault(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{
		name: "openai",
		response: &types.ChatResponse{
			ID:    "chatcmpl-rl3",
			Model: "gpt-4o-mini",
			Choices: []types.ChatChoice{
				{Message: types.Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"},
			},
			Usage: types.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
		},
	}
	// No rate limit config — RateLimit.Enabled defaults to false.
	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{})

	body := `{"model":"gpt-4o-mini","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`
	for i := 0; i < 5; i++ {
		rec := performJSONRequest(t, handler, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d, want 200 (rate limit should be disabled)", i+1, rec.Code)
		}
	}
}

func TestHandleChatReturns402OnBudgetExceeded(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	provider := &fakeProvider{name: "openai", defaultModel: "gpt-4o-mini"}

	// 1 µUSD budget — any real request estimate will exceed it immediately.
	handler := newTestHTTPHandlerWithConfig(logger, provider, config.Config{
		Governor: config.GovernorConfig{
			MaxTotalBudgetMicros:    1,
			MaxPromptTokens:         100_000,
			BudgetWarningThresholds: []int{50, 80, 95},
			BudgetHistoryLimit:      20,
		},
	})

	// max_tokens drives the cost estimate; without it the estimate is ~0 µUSD and
	// wouldn't exceed the 1 µUSD budget.
	rec := performJSONRequest(t, handler, `{"model":"gpt-4o-mini","max_tokens":1024,"messages":[{"role":"user","content":"hello"}]}`)
	if rec.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want 402\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestCheckRateLimitSetsHeaders(t *testing.T) {
	t.Parallel()
	h := &Handler{
		rateLimiter: ratelimit.NewStore(5, 5),
	}
	w := httptest.NewRecorder()
	ok := h.checkRateLimit(w, "test-key")
	if !ok {
		t.Fatal("checkRateLimit returned false on first call")
	}
	if w.Header().Get("X-RateLimit-Limit") != "5" {
		t.Errorf("X-RateLimit-Limit = %q, want \"5\"", w.Header().Get("X-RateLimit-Limit"))
	}
	rem, err := strconv.Atoi(w.Header().Get("X-RateLimit-Remaining"))
	if err != nil {
		t.Fatalf("X-RateLimit-Remaining not numeric: %s", w.Header().Get("X-RateLimit-Remaining"))
	}
	if rem != 4 {
		t.Errorf("X-RateLimit-Remaining = %d, want 4", rem)
	}
}

func TestCheckRateLimitReturns429WhenExhausted(t *testing.T) {
	t.Parallel()
	h := &Handler{rateLimiter: ratelimit.NewStore(1, 60)}
	// Consume the single token.
	w1 := httptest.NewRecorder()
	h.checkRateLimit(w1, "k")
	// Second call should be rejected.
	w2 := httptest.NewRecorder()
	ok := h.checkRateLimit(w2, "k")
	if ok {
		t.Fatal("checkRateLimit should return false when bucket is empty")
	}
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w2.Code)
	}
}

func TestCheckRateLimitNilLimiterAlwaysAllows(t *testing.T) {
	t.Parallel()
	h := &Handler{rateLimiter: nil}
	w := httptest.NewRecorder()
	if !h.checkRateLimit(w, "anything") {
		t.Error("nil rateLimiter should always allow")
	}
}
