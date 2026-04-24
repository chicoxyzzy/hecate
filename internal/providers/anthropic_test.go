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
		Transport: testRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
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
		Transport: testRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
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

func TestAnthropicProviderCapabilitiesSkipsDiscoveryWhenCloudProviderUnconfigured(t *testing.T) {
	t.Parallel()

	var calls int
	provider := NewAnthropicProvider(config.OpenAICompatibleProviderConfig{
		Name:       "anthropic",
		Kind:       "cloud",
		Protocol:   "anthropic",
		BaseURL:    "https://api.anthropic.com",
		APIVersion: "2023-06-01",
		Timeout:    5 * time.Second,
	}, nil)
	provider.httpClient = &http.Client{
		Transport: testRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"claude-sonnet-4-20250514"}]}`)),
			}, nil
		}),
	}

	caps, err := provider.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("discovery call count = %d, want 0 for unconfigured cloud provider", calls)
	}
	if caps.DiscoverySource != "config_unconfigured" {
		t.Fatalf("discovery source = %q, want config_unconfigured", caps.DiscoverySource)
	}
}

// TestAnthropicMessagesFromTypesPreservesCacheControl verifies that ContentBlocks with
// cache_control survive the conversion to the Anthropic wire format.
func TestAnthropicMessagesFromTypesPreservesCacheControl(t *testing.T) {
	t.Parallel()

	cc := json.RawMessage(`{"type":"ephemeral"}`)
	messages := []types.Message{
		{
			Role:    "system",
			Content: "You are helpful. Be concise.",
			ContentBlocks: []types.ContentBlock{
				{Type: "text", Text: "You are helpful.", CacheControl: cc},
				{Type: "text", Text: "Be concise."},
			},
		},
		{
			Role:    "user",
			Content: "What is 2+2?",
			ContentBlocks: []types.ContentBlock{
				{Type: "text", Text: "What is 2+2?", CacheControl: cc},
			},
		},
	}

	systemRaw, wire := anthropicMessagesFromTypes(messages)

	// System should be a JSON array (has cache_control).
	var sysBlocks []map[string]any
	if err := json.Unmarshal(systemRaw, &sysBlocks); err != nil {
		t.Fatalf("system is not a JSON array: %v, raw=%s", err, systemRaw)
	}
	if len(sysBlocks) != 2 {
		t.Fatalf("system blocks count = %d, want 2", len(sysBlocks))
	}
	// First block has cache_control.
	if sysBlocks[0]["cache_control"] == nil {
		t.Fatalf("system[0] missing cache_control")
	}
	// Second block has no cache_control.
	if sysBlocks[1]["cache_control"] != nil {
		t.Fatalf("system[1] unexpected cache_control")
	}

	// Messages should have one user message with cache_control on the text block.
	if len(wire) != 1 {
		t.Fatalf("wire messages count = %d, want 1", len(wire))
	}
	if wire[0].Role != "user" {
		t.Fatalf("wire[0].role = %q, want user", wire[0].Role)
	}
	if len(wire[0].Content) != 1 {
		t.Fatalf("wire[0] content blocks = %d, want 1", len(wire[0].Content))
	}
	if wire[0].Content[0].Type != "text" {
		t.Fatalf("wire[0].content[0].type = %q, want text", wire[0].Content[0].Type)
	}
	if len(wire[0].Content[0].CacheControl) == 0 {
		t.Fatal("wire[0].content[0] missing cache_control")
	}
}

func TestAnthropicMessagesFromTypesSingleBlockNoArrayWrap(t *testing.T) {
	t.Parallel()

	// System with a single text block and no cache_control → plain string, not array.
	messages := []types.Message{
		{
			Role:          "system",
			Content:       "You are helpful.",
			ContentBlocks: []types.ContentBlock{{Type: "text", Text: "You are helpful."}},
		},
		{Role: "user", Content: "Hi", ContentBlocks: []types.ContentBlock{{Type: "text", Text: "Hi"}}},
	}
	systemRaw, _ := anthropicMessagesFromTypes(messages)

	var s string
	if err := json.Unmarshal(systemRaw, &s); err != nil {
		t.Fatalf("system should be a plain JSON string, got: %s", systemRaw)
	}
	if s != "You are helpful." {
		t.Fatalf("system = %q, want plain text", s)
	}
}

func TestAnthropicChatUpstreamSendsCacheControlBlocks(t *testing.T) {
	t.Parallel()

	cc := json.RawMessage(`{"type":"ephemeral"}`)
	var capturedBody map[string]any

	provider := NewAnthropicProvider(config.OpenAICompatibleProviderConfig{
		Name:         "anthropic",
		Kind:         "cloud",
		Protocol:     "anthropic",
		BaseURL:      "https://api.anthropic.test",
		APIKey:       "secret",
		APIVersion:   "2023-06-01",
		Timeout:      5 * time.Second,
		DefaultModel: "claude-opus-4-5",
	}, nil)
	provider.cachedCaps = Capabilities{
		Name:         "anthropic",
		Kind:         KindCloud,
		DefaultModel: "claude-opus-4-5",
		Models:       []string{"claude-opus-4-5"},
	}
	provider.capsExpiry = time.Now().Add(time.Minute)
	provider.httpClient = &http.Client{
		Transport: testRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			json.NewDecoder(r.Body).Decode(&capturedBody) //nolint:errcheck
			body, _ := json.Marshal(map[string]any{
				"id":          "msg_cc",
				"model":       "claude-opus-4-5",
				"role":        "assistant",
				"stop_reason": "end_turn",
				"content":     []map[string]any{{"type": "text", "text": "4"}},
				"usage":       map[string]any{"input_tokens": 20, "output_tokens": 1},
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	}

	_, err := provider.Chat(context.Background(), types.ChatRequest{
		Model:     "claude-opus-4-5",
		MaxTokens: 32,
		Messages: []types.Message{
			{
				Role:    "system",
				Content: "Big system prompt.",
				ContentBlocks: []types.ContentBlock{
					{Type: "text", Text: "Big system prompt.", CacheControl: cc},
				},
			},
			{
				Role:    "user",
				Content: "What is 2+2?",
				ContentBlocks: []types.ContentBlock{
					{Type: "text", Text: "What is 2+2?", CacheControl: cc},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	// Verify the captured upstream body has cache_control in system and messages.
	systemRaw, ok := capturedBody["system"]
	if !ok {
		t.Fatal("upstream body missing system field")
	}
	sysBytes, _ := json.Marshal(systemRaw)
	if !strings.Contains(string(sysBytes), "ephemeral") {
		t.Fatalf("system missing cache_control, got: %s", sysBytes)
	}

	msgs, _ := capturedBody["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatal("upstream body missing messages")
	}
	msgBytes, _ := json.Marshal(msgs[0])
	if !strings.Contains(string(msgBytes), "ephemeral") {
		t.Fatalf("messages[0] missing cache_control, got: %s", msgBytes)
	}
}
