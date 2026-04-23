package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestAnthropicProviderChatMapsMessagesAPI(t *testing.T) {
	t.Parallel()

	provider := NewAnthropicProvider(config.OpenAICompatibleProviderConfig{
		Name:         "anthropic",
		Kind:         "cloud",
		Protocol:     "anthropic",
		BaseURL:      "https://api.anthropic.test",
		APIKey:       "secret",
		APIVersion:   "2023-06-01",
		Timeout:      5 * time.Second,
		DefaultModel: "claude-sonnet-4-20250514",
	}, nil)
	provider.cachedCaps = Capabilities{
		Name:         "anthropic",
		Kind:         KindCloud,
		DefaultModel: "claude-sonnet-4-20250514",
		Models:       []string{"claude-sonnet-4-20250514"},
	}
	provider.capsExpiry = time.Now().Add(time.Minute)
	provider.httpClient = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/v1/messages" {
				t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
			}
			if got := r.Header.Get("x-api-key"); got != "secret" {
				t.Fatalf("x-api-key = %q, want secret", got)
			}
			if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
				t.Fatalf("anthropic-version = %q, want 2023-06-01", got)
			}

			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if payload["model"] != "claude-sonnet-4-20250514" {
				t.Fatalf("model = %#v, want claude-sonnet-4-20250514", payload["model"])
			}
			if payload["max_tokens"] != float64(1024) {
				t.Fatalf("max_tokens = %#v, want 1024", payload["max_tokens"])
			}

			body, err := json.Marshal(map[string]any{
				"id":          "msg_123",
				"model":       "claude-sonnet-4-20250514",
				"role":        "assistant",
				"stop_reason": "end_turn",
				"content": []map[string]any{
					{"type": "text", "text": "Hello from Claude."},
				},
				"usage": map[string]any{
					"input_tokens":  14,
					"output_tokens": 5,
				},
			})
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	}

	resp, err := provider.Chat(context.Background(), types.ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.ID != "msg_123" {
		t.Fatalf("id = %q, want msg_123", resp.ID)
	}
	if resp.Choices[0].Message.Content != "Hello from Claude." {
		t.Fatalf("content = %q, want Claude response", resp.Choices[0].Message.Content)
	}
	if resp.Usage.TotalTokens != 19 {
		t.Fatalf("total_tokens = %d, want 19", resp.Usage.TotalTokens)
	}
}

func TestAnthropicProviderCapabilitiesUsesModelsEndpoint(t *testing.T) {
	t.Parallel()

	provider := NewAnthropicProvider(config.OpenAICompatibleProviderConfig{
		Name:       "anthropic",
		Kind:       "cloud",
		Protocol:   "anthropic",
		BaseURL:    "https://api.anthropic.test",
		APIKey:     "secret",
		APIVersion: "2023-06-01",
		Timeout:    5 * time.Second,
	}, nil)
	provider.httpClient = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/v1/models" {
				t.Fatalf("path = %q, want /v1/models", r.URL.Path)
			}
			body, err := json.Marshal(map[string]any{
				"data": []map[string]any{
					{"id": "claude-sonnet-4-20250514"},
					{"id": "claude-haiku-3-5-20241022"},
				},
			})
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	}

	caps, err := provider.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if caps.DefaultModel != "claude-sonnet-4-20250514" {
		t.Fatalf("default_model = %q, want first discovered model", caps.DefaultModel)
	}
	if len(caps.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(caps.Models))
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
