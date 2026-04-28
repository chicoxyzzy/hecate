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

// TestAnthropicProviderCapturesCacheReadTokens pins the prompt-cache
// usage path: when the upstream returns cache_read_input_tokens, it
// must land in Usage.CachedPromptTokens (so the pricebook applies
// the cache rate). The prior adapter dropped the field entirely,
// which made cache hits silently bill at the full input rate.
func TestAnthropicProviderCapturesCacheReadTokens(t *testing.T) {
	t.Parallel()
	provider := newAnthropicTestProvider(t, func(r *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{
			"id":          "msg_c1",
			"model":       "claude-opus-4-5",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"usage": map[string]any{
				"input_tokens":            10,
				"output_tokens":           3,
				"cache_read_input_tokens": 1000,
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})

	resp, err := provider.Chat(context.Background(), types.ChatRequest{
		Model:    "claude-opus-4-5",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if got := resp.Usage.PromptTokens; got != 10 {
		t.Errorf("PromptTokens = %d, want 10 (input_tokens only)", got)
	}
	if got := resp.Usage.CachedPromptTokens; got != 1000 {
		t.Errorf("CachedPromptTokens = %d, want 1000 (mapped from cache_read_input_tokens)", got)
	}
	if got := resp.Usage.CompletionTokens; got != 3 {
		t.Errorf("CompletionTokens = %d, want 3", got)
	}
	// Total includes everything billed: fresh input + cache reads + output.
	if got := resp.Usage.TotalTokens; got != 1013 {
		t.Errorf("TotalTokens = %d, want 1013 (10 + 1000 + 3)", got)
	}
}

// TestAnthropicProviderFoldsCacheCreationIntoPromptTokens verifies
// the second cache bucket — cache writes — gets counted (folded
// into PromptTokens at the fresh rate). The prior adapter dropped
// these too. The fold trade-off is documented on anthropicUsage:
// when the pricebook gains a cache-write rate, this becomes a
// dedicated Usage field.
func TestAnthropicProviderFoldsCacheCreationIntoPromptTokens(t *testing.T) {
	t.Parallel()
	provider := newAnthropicTestProvider(t, func(r *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{
			"id":          "msg_c2",
			"model":       "claude-opus-4-5",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"usage": map[string]any{
				"input_tokens":                100,
				"output_tokens":               5,
				"cache_creation_input_tokens": 500,
			},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})

	resp, err := provider.Chat(context.Background(), types.ChatRequest{
		Model:    "claude-opus-4-5",
		Messages: []types.Message{{Role: "user", Content: "write to cache"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if got := resp.Usage.PromptTokens; got != 600 {
		t.Errorf("PromptTokens = %d, want 600 (100 fresh + 500 cache_creation)", got)
	}
	if got := resp.Usage.CachedPromptTokens; got != 0 {
		t.Errorf("CachedPromptTokens = %d, want 0 (cache_creation is NOT a cache read)", got)
	}
	if got := resp.Usage.TotalTokens; got != 605 {
		t.Errorf("TotalTokens = %d, want 605 (600 input-side + 5 output)", got)
	}
}

// TestAnthropicProviderUsageBackwardCompat — a response with
// neither cache field present (the common case before prompt
// caching is enabled) must produce the same Usage shape as before
// the cache-fields change. Guards against accidentally requiring
// the new fields or shifting behavior on un-cached requests.
func TestAnthropicProviderUsageBackwardCompat(t *testing.T) {
	t.Parallel()
	provider := newAnthropicTestProvider(t, func(r *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{
			"id":          "msg_b1",
			"model":       "claude-opus-4-5",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"usage":       map[string]any{"input_tokens": 14, "output_tokens": 5},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(body))),
		}, nil
	})
	resp, err := provider.Chat(context.Background(), types.ChatRequest{
		Model:    "claude-opus-4-5",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Usage.PromptTokens != 14 || resp.Usage.CompletionTokens != 5 || resp.Usage.TotalTokens != 19 {
		t.Errorf("Usage = %+v, want {PromptTokens:14 CompletionTokens:5 TotalTokens:19}", resp.Usage)
	}
	if resp.Usage.CachedPromptTokens != 0 {
		t.Errorf("CachedPromptTokens = %d, want 0 (no cache fields present)", resp.Usage.CachedPromptTokens)
	}
}

// TestAnthropicProviderStreamForwardsCacheUsage verifies the
// streaming path carries cache token counts through to the final
// usage chunk. The prior adapter dropped the message_start usage
// entirely and emitted prompt_tokens=0 for every streamed
// response — invisible billing bug.
//
// We feed a synthetic SSE stream into translateAnthropicSSE
// directly (rather than wiring up an HTTP server) so the test
// stays focused on the translation contract.
func TestAnthropicProviderStreamForwardsCacheUsage(t *testing.T) {
	t.Parallel()

	// Anthropic SSE: message_start carries the input/cache buckets;
	// message_delta carries running output_tokens; message_stop
	// closes the stream.
	src := strings.NewReader(strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_s1","model":"claude-opus-4-5","usage":{"input_tokens":50,"output_tokens":0,"cache_read_input_tokens":2000,"cache_creation_input_tokens":300}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n"))

	var dst strings.Builder
	if err := translateAnthropicSSE(context.Background(), "claude-opus-4-5", src, &dst); err != nil {
		t.Fatalf("translateAnthropicSSE: %v", err)
	}

	// Find the usage chunk emitted on message_delta. It's the only
	// chunk with a non-empty `usage` object.
	var lastUsage map[string]any
	for _, line := range strings.Split(dst.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		body := strings.TrimPrefix(line, "data: ")
		if body == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(body), &chunk); err != nil {
			continue
		}
		if u, ok := chunk["usage"].(map[string]any); ok && len(u) > 0 {
			lastUsage = u
		}
	}
	if lastUsage == nil {
		t.Fatalf("no usage chunk found in stream output: %s", dst.String())
	}

	// PromptTokens = input_tokens(50) + cache_creation(300) = 350.
	if got := lastUsage["prompt_tokens"]; got != float64(350) {
		t.Errorf("prompt_tokens = %v, want 350", got)
	}
	if got := lastUsage["completion_tokens"]; got != float64(12) {
		t.Errorf("completion_tokens = %v, want 12", got)
	}
	// Total = 350 prompt + 2000 cache reads + 12 output = 2362.
	if got := lastUsage["total_tokens"]; got != float64(2362) {
		t.Errorf("total_tokens = %v, want 2362", got)
	}
	// cached_tokens lives under prompt_tokens_details, mirroring
	// OpenAI's prompt-cache shape so downstream consumers don't
	// need a provider-specific accessor.
	details, ok := lastUsage["prompt_tokens_details"].(map[string]any)
	if !ok {
		t.Fatalf("prompt_tokens_details missing/not an object; got: %v", lastUsage["prompt_tokens_details"])
	}
	if got := details["cached_tokens"]; got != float64(2000) {
		t.Errorf("prompt_tokens_details.cached_tokens = %v, want 2000", got)
	}
}
