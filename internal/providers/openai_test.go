package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestOpenAIProviderChatUpstream(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			return nil, fmt.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			return nil, fmt.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			return nil, fmt.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("ReadAll() error = %w", err)
		}

		var wireReq openAIChatCompletionRequest
		if err := json.Unmarshal(body, &wireReq); err != nil {
			return nil, fmt.Errorf("Unmarshal() error = %w", err)
		}
		if wireReq.Model != "gpt-4o-mini" {
			return nil, fmt.Errorf("model = %q, want %q", wireReq.Model, "gpt-4o-mini")
		}
		if len(wireReq.Messages) != 1 || wireReq.Messages[0].Content != "hello" {
			return nil, fmt.Errorf("messages = %#v, want one hello message", wireReq.Messages)
		}

		responseBody, err := json.Marshal(openAIChatCompletionResponse{
			ID:      "chatcmpl-123",
			Created: 1_700_000_000,
			Model:   "gpt-4o-mini",
			Choices: []openAIChatCompletionChoice{
				{
					Index: 0,
					Message: openAIChatMessage{
						Role:    "assistant",
						Content: "world",
					},
					FinishReason: "stop",
				},
			},
			Usage: openAIUsage{
				PromptTokens:     10,
				CompletionTokens: 4,
				TotalTokens:      14,
			},
		})
		if err != nil {
			return nil, err
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
		}, nil
	})

	provider := NewOpenAIProvider(config.OpenAICompatibleProviderConfig{
		Name:          "openai",
		Kind:          "cloud",
		BaseURL:       "https://example.test",
		APIKey:        "test-key",
		Timeout:       time.Second,
		StubMode:      false,
		AllowAnyModel: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.httpClient.Transport = transport

	resp, err := provider.Chat(context.Background(), types.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if resp.ID != "chatcmpl-123" {
		t.Fatalf("response id = %q, want %q", resp.ID, "chatcmpl-123")
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "world" {
		t.Fatalf("choices = %#v, want assistant world", resp.Choices)
	}
	if resp.Usage.TotalTokens != 14 {
		t.Fatalf("total tokens = %d, want 14", resp.Usage.TotalTokens)
	}
}

func TestOpenAIProviderChatUpstreamError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		responseBody, err := json.Marshal(openAIErrorEnvelope{
			Error: openAIErrorDetail{
				Message: "invalid api key",
				Type:    "invalid_request_error",
			},
		})
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
		}, nil
	})

	provider := NewOpenAIProvider(config.OpenAICompatibleProviderConfig{
		Name:          "openai",
		Kind:          "cloud",
		BaseURL:       "https://example.test",
		APIKey:        "bad-key",
		Timeout:       time.Second,
		StubMode:      false,
		AllowAnyModel: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.httpClient.Transport = transport

	_, err := provider.Chat(context.Background(), types.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err == nil {
		t.Fatal("Chat() error = nil, want upstream error")
	}

	upstreamErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("error type = %T, want *UpstreamError", err)
	}
	if upstreamErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", upstreamErr.StatusCode, http.StatusUnauthorized)
	}
}

func TestOpenAIProviderCapabilitiesDiscovery(t *testing.T) {
	t.Parallel()

	var calls int
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet {
			return nil, fmt.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/models" {
			return nil, fmt.Errorf("path = %s, want /v1/models", r.URL.Path)
		}

		responseBody, err := json.Marshal(openAIModelsResponse{
			Data: []openAIModel{
				{ID: "llama3.1:8b"},
				{ID: "qwen2.5:7b"},
			},
		})
		if err != nil {
			return nil, err
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
		}, nil
	})

	provider := NewOpenAICompatibleProvider(config.OpenAICompatibleProviderConfig{
		Name:          "ollama",
		Kind:          "local",
		BaseURL:       "http://127.0.0.1:11434",
		Timeout:       time.Second,
		DefaultModel:  "configured-default",
		AllowAnyModel: false,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.httpClient.Transport = transport

	caps, err := provider.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if caps.DefaultModel != "configured-default" {
		t.Fatalf("default model = %q, want configured-default", caps.DefaultModel)
	}
	if len(caps.Models) != 2 || caps.Models[0] != "llama3.1:8b" {
		t.Fatalf("models = %#v, want discovered model list", caps.Models)
	}
	if caps.DiscoverySource != "upstream_v1_models" {
		t.Fatalf("discovery source = %q, want upstream_v1_models", caps.DiscoverySource)
	}

	_, err = provider.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() cached error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("discovery call count = %d, want 1 due to cache", calls)
	}
}

func TestOpenAIProviderCapabilitiesFallbackToConfig(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network unavailable")
	})

	provider := NewOpenAICompatibleProvider(config.OpenAICompatibleProviderConfig{
		Name:          "localai",
		Kind:          "local",
		BaseURL:       "http://127.0.0.1:8080/v1",
		Timeout:       time.Second,
		DefaultModel:  "llama3",
		Models:        []string{"llama3", "mistral"},
		AllowAnyModel: false,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.httpClient.Transport = transport

	caps, err := provider.Capabilities(context.Background())
	if err == nil {
		t.Fatal("Capabilities() error = nil, want discovery failure")
	}
	if caps.DefaultModel != "llama3" {
		t.Fatalf("default model = %q, want llama3", caps.DefaultModel)
	}
	if len(caps.Models) != 2 {
		t.Fatalf("models len = %d, want 2", len(caps.Models))
	}
	if caps.DiscoverySource != "config_fallback" {
		t.Fatalf("discovery source = %q, want config_fallback", caps.DiscoverySource)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
