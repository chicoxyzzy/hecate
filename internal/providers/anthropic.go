package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/requestscope"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

const defaultAnthropicVersion = "2023-06-01"

type AnthropicProvider struct {
	config     config.OpenAICompatibleProviderConfig
	logger     *slog.Logger
	httpClient *http.Client
	mu         sync.Mutex
	cachedCaps Capabilities
	capsExpiry time.Time
}

type anthropicMessagesRequest struct {
	Model       string                     `json:"model"`
	System      string                     `json:"system,omitempty"`
	Messages    []anthropicMessage         `json:"messages"`
	MaxTokens   int                        `json:"max_tokens"`
	Temperature float64                    `json:"temperature,omitempty"`
	Metadata    *anthropicMessagesMetadata `json:"metadata,omitempty"`
	Tools       []anthropicTool            `json:"tools,omitempty"`
	ToolChoice  json.RawMessage            `json:"tool_choice,omitempty"`
	Stream      bool                       `json:"stream,omitempty"`
}

type anthropicMessagesMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

// anthropicContentBlock covers all block variants (text, tool_use, tool_result).
type anthropicContentBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	// Content reused as tool_result content string (omitted for other types)
	ResultContent string `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicMessagesResponse struct {
	ID         string                  `json:"id"`
	Model      string                  `json:"model"`
	Role       string                  `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      anthropicUsage          `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicModelsResponse struct {
	Data []anthropicModel `json:"data"`
}

type anthropicModel struct {
	ID string `json:"id"`
}

type anthropicErrorEnvelope struct {
	Error anthropicErrorDetail `json:"error"`
}

type anthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func NewAnthropicProvider(cfg config.OpenAICompatibleProviderConfig, logger *slog.Logger) *AnthropicProvider {
	return &AnthropicProvider{
		config: cfg,
		logger: logger,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (p *AnthropicProvider) Name() string {
	return p.config.Name
}

func (p *AnthropicProvider) Kind() Kind {
	if p.config.Kind == string(KindLocal) {
		return KindLocal
	}
	return KindCloud
}

func (p *AnthropicProvider) DefaultModel() string {
	return p.config.DefaultModel
}

func (p *AnthropicProvider) Supports(model string) bool {
	if model == "" {
		return p.config.DefaultModel != ""
	}
	if p.config.AllowAnyModel {
		return true
	}
	for _, candidate := range p.config.Models {
		if candidate == model {
			return true
		}
	}
	return p.config.DefaultModel != "" && p.config.DefaultModel == model
}

func (p *AnthropicProvider) supportsResolvedModel(ctx context.Context, model string) bool {
	if model == "" {
		return p.config.DefaultModel != ""
	}
	if p.config.AllowAnyModel {
		return true
	}
	caps, err := p.Capabilities(ctx)
	if err == nil {
		for _, candidate := range caps.Models {
			if candidate == model {
				return true
			}
		}
		if caps.DefaultModel == model {
			return true
		}
	}
	return p.Supports(model)
}

func (p *AnthropicProvider) Capabilities(ctx context.Context) (Capabilities, error) {
	if p.config.StubMode {
		return p.staticCapabilities("config"), nil
	}

	p.mu.Lock()
	if !p.capsExpiry.IsZero() && time.Now().Before(p.capsExpiry) {
		cached := p.cachedCaps
		p.mu.Unlock()
		return cached, nil
	}
	p.mu.Unlock()

	discovered, err := p.discoverCapabilities(ctx)
	if err != nil {
		telemetry.Warn(p.logger, ctx, "gateway.providers.capabilities.discovery_failed",
			slog.String("event.name", "gateway.providers.capabilities.discovery_failed"),
			slog.String("gen_ai.provider.name", p.Name()),
			slog.Any("error", err),
		)
		return p.staticCapabilities("config_fallback"), err
	}

	p.mu.Lock()
	p.cachedCaps = discovered
	p.capsExpiry = time.Now().Add(time.Minute)
	p.mu.Unlock()
	return discovered, nil
}

func (p *AnthropicProvider) Chat(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	if !p.supportsResolvedModel(ctx, req.Model) {
		return nil, fmt.Errorf("model %q is not supported by provider %s", req.Model, p.Name())
	}
	if p.config.StubMode {
		content := p.config.StubResponse
		if content == "" {
			content = "Stubbed Anthropic response."
		}
		promptTokens := estimatePromptTokens(req.Messages)
		completionTokens := max(16, len(content)/4)
		now := time.Now().UTC()
		return &types.ChatResponse{
			ID:        "msg-stub",
			Model:     req.Model,
			CreatedAt: now,
			Choices: []types.ChatChoice{{
				Index: 0,
				Message: types.Message{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			}},
			Usage: types.Usage{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				TotalTokens:      promptTokens + completionTokens,
			},
		}, nil
	}
	return p.chatUpstream(ctx, req)
}

func (p *AnthropicProvider) staticCapabilities(source string) Capabilities {
	models := append([]string(nil), p.config.Models...)
	if p.config.DefaultModel != "" && !contains(models, p.config.DefaultModel) {
		models = append(models, p.config.DefaultModel)
	}
	return Capabilities{
		Name:            p.Name(),
		Kind:            p.Kind(),
		DefaultModel:    p.config.DefaultModel,
		Models:          models,
		Discoverable:    !p.config.StubMode,
		DiscoverySource: source,
		RefreshedAt:     time.Now().UTC(),
	}
}

func (p *AnthropicProvider) discoverCapabilities(ctx context.Context) (Capabilities, error) {
	endpoint := strings.TrimRight(p.config.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Capabilities{}, fmt.Errorf("build models request: %w", err)
	}
	p.applyHeaders(req)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return Capabilities{}, fmt.Errorf("send models request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return Capabilities{}, decodeAnthropicError(resp)
	}

	var payload anthropicModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Capabilities{}, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.ID != "" && !contains(models, item.ID) {
			models = append(models, item.ID)
		}
	}
	defaultModel := p.config.DefaultModel
	if defaultModel == "" && len(models) > 0 {
		defaultModel = models[0]
	}
	return Capabilities{
		Name:            p.Name(),
		Kind:            p.Kind(),
		DefaultModel:    defaultModel,
		Models:          models,
		Discoverable:    true,
		DiscoverySource: "upstream_v1_models",
		RefreshedAt:     time.Now().UTC(),
	}, nil
}

func (p *AnthropicProvider) chatUpstream(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	if p.config.APIKey == "" && p.Kind() != KindLocal {
		return nil, fmt.Errorf("api key is required for cloud provider %s when stub mode is disabled", p.Name())
	}

	system, messages := anthropicMessagesFromTypes(req.Messages)
	if len(messages) == 0 {
		return nil, fmt.Errorf("anthropic messages request requires at least one non-system message")
	}
	wireReq := anthropicMessagesRequest{
		Model:     req.Model,
		System:    system,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
	}
	if wireReq.MaxTokens <= 0 {
		wireReq.MaxTokens = 1024
	}
	if req.Temperature > 0 {
		wireReq.Temperature = req.Temperature
	}
	if userID := requestscope.Normalize(req.Scope).User; userID != "" {
		wireReq.Metadata = &anthropicMessagesMetadata{UserID: userID}
	}
	if len(req.Tools) > 0 {
		wireReq.Tools = make([]anthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Function.Parameters
			if len(schema) == 0 {
				schema = json.RawMessage(`{}`)
			}
			wireReq.Tools = append(wireReq.Tools, anthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: schema,
			})
		}
		wireReq.ToolChoice = anthropicToolChoice(req.ToolChoice)
	}

	payload, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal upstream request: %w", err)
	}
	endpoint := strings.TrimRight(p.config.BaseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	p.applyHeaders(httpReq)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send upstream request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, decodeAnthropicError(resp)
	}

	var wireResp anthropicMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&wireResp); err != nil {
		return nil, fmt.Errorf("decode upstream response: %w", err)
	}
	model := wireResp.Model
	if model == "" {
		model = req.Model
	}
	msg := anthropicResponseToMessage(wireResp.Content)
	finishReason := wireResp.StopReason
	if finishReason == "tool_use" {
		finishReason = "tool_calls"
	}
	return &types.ChatResponse{
		ID:        wireResp.ID,
		Model:     model,
		CreatedAt: time.Now().UTC(),
		Choices: []types.ChatChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: types.Usage{
			PromptTokens:     wireResp.Usage.InputTokens,
			CompletionTokens: wireResp.Usage.OutputTokens,
			TotalTokens:      wireResp.Usage.InputTokens + wireResp.Usage.OutputTokens,
		},
	}, nil
}

func (p *AnthropicProvider) applyHeaders(req *http.Request) {
	if p.config.APIKey != "" {
		req.Header.Set("x-api-key", p.config.APIKey)
	}
	req.Header.Set("anthropic-version", p.apiVersion())
}

func (p *AnthropicProvider) apiVersion() string {
	if strings.TrimSpace(p.config.APIVersion) != "" {
		return strings.TrimSpace(p.config.APIVersion)
	}
	return defaultAnthropicVersion
}

func anthropicMessagesFromTypes(messages []types.Message) (string, []anthropicMessage) {
	systemParts := make([]string, 0, 1)
	wire := make([]anthropicMessage, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		role := strings.TrimSpace(msg.Role)

		switch role {
		case "system":
			if text := strings.TrimSpace(msg.Content); text != "" {
				systemParts = append(systemParts, text)
			}

		case "assistant":
			if len(msg.ToolCalls) > 0 {
				blocks := make([]anthropicContentBlock, 0, len(msg.ToolCalls)+1)
				if msg.Content != "" {
					blocks = append(blocks, anthropicContentBlock{Type: "text", Text: msg.Content})
				}
				for _, tc := range msg.ToolCalls {
					input := json.RawMessage(tc.Function.Arguments)
					if !json.Valid(input) {
						input = json.RawMessage(`{}`)
					}
					blocks = append(blocks, anthropicContentBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: input,
					})
				}
				wire = append(wire, anthropicMessage{Role: "assistant", Content: blocks})
			} else {
				wire = append(wire, anthropicMessage{
					Role:    "assistant",
					Content: []anthropicContentBlock{{Type: "text", Text: msg.Content}},
				})
			}

		case "tool":
			// Batch consecutive tool-result messages into a single user message.
			blocks := []anthropicContentBlock{{
				Type:          "tool_result",
				ToolUseID:     msg.ToolCallID,
				ResultContent: msg.Content,
			}}
			for i+1 < len(messages) && strings.TrimSpace(messages[i+1].Role) == "tool" {
				i++
				next := messages[i]
				blocks = append(blocks, anthropicContentBlock{
					Type:          "tool_result",
					ToolUseID:     next.ToolCallID,
					ResultContent: next.Content,
				})
			}
			wire = append(wire, anthropicMessage{Role: "user", Content: blocks})

		case "user":
			wire = append(wire, anthropicMessage{
				Role:    "user",
				Content: []anthropicContentBlock{{Type: "text", Text: msg.Content}},
			})
		}
	}
	return strings.Join(systemParts, "\n\n"), wire
}

func anthropicResponseToMessage(blocks []anthropicContentBlock) types.Message {
	msg := types.Message{Role: "assistant"}
	textParts := make([]string, 0)
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				textParts = append(textParts, t)
			}
		case "tool_use":
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			msg.ToolCalls = append(msg.ToolCalls, types.ToolCall{
				ID:   b.ID,
				Type: "function",
				Function: types.ToolCallFunction{
					Name:      b.Name,
					Arguments: args,
				},
			})
		}
	}
	msg.Content = strings.Join(textParts, "\n")
	return msg
}

func anthropicToolChoice(choice json.RawMessage) json.RawMessage {
	if len(choice) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(choice, &s) == nil {
		switch s {
		case "auto":
			return json.RawMessage(`{"type":"auto"}`)
		case "none":
			return nil
		case "required":
			return json.RawMessage(`{"type":"any"}`)
		}
		return nil
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(choice, &obj) == nil && obj.Type == "function" && obj.Function.Name != "" {
		b, _ := json.Marshal(map[string]string{"type": "tool", "name": obj.Function.Name})
		return b
	}
	return nil
}

func (p *AnthropicProvider) ChatStream(ctx context.Context, req types.ChatRequest, w io.Writer) error {
	if p.config.APIKey == "" && p.Kind() != KindLocal {
		return fmt.Errorf("api key is required for cloud provider %s when stub mode is disabled", p.Name())
	}

	system, messages := anthropicMessagesFromTypes(req.Messages)
	if len(messages) == 0 {
		return fmt.Errorf("anthropic messages request requires at least one non-system message")
	}
	wireReq := anthropicMessagesRequest{
		Model:     req.Model,
		System:    system,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
		Stream:    true,
	}
	if wireReq.MaxTokens <= 0 {
		wireReq.MaxTokens = 1024
	}
	if req.Temperature > 0 {
		wireReq.Temperature = req.Temperature
	}
	if userID := requestscope.Normalize(req.Scope).User; userID != "" {
		wireReq.Metadata = &anthropicMessagesMetadata{UserID: userID}
	}
	if len(req.Tools) > 0 {
		wireReq.Tools = make([]anthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Function.Parameters
			if len(schema) == 0 {
				schema = json.RawMessage(`{}`)
			}
			wireReq.Tools = append(wireReq.Tools, anthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: schema,
			})
		}
		wireReq.ToolChoice = anthropicToolChoice(req.ToolChoice)
	}

	payload, err := json.Marshal(wireReq)
	if err != nil {
		return fmt.Errorf("marshal upstream request: %w", err)
	}
	endpoint := strings.TrimRight(p.config.BaseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build upstream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	p.applyHeaders(httpReq)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send upstream request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return decodeAnthropicError(resp)
	}

	return translateAnthropicSSE(ctx, req.Model, resp.Body, w)
}

// translateAnthropicSSE reads Anthropic SSE events and writes OpenAI-format SSE chunks.
func translateAnthropicSSE(ctx context.Context, model string, src io.Reader, dst io.Writer) error {
	type anthropicStreamEvent struct {
		Type  string          `json:"type"`
		Index int             `json:"index"`
		Delta json.RawMessage `json:"delta"`
		// message_start
		Message *struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
		// content_block_start
		ContentBlock *struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
		// message_delta usage
		Usage *struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	type deltaPayload struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	}

	var (
		completionID string
		// track open tool_use blocks by index
		toolBlocks = make(map[int]struct{ id, name string })
	)

	writeChunk := func(data any) error {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(dst, "data: %s\n\n", b); err != nil {
			return err
		}
		if f, ok := dst.(interface{ Flush() }); ok {
			f.Flush()
		}
		return nil
	}

	scanner := bufio.NewScanner(src)
	var eventType string
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		rawData := strings.TrimPrefix(line, "data: ")

		var ev anthropicStreamEvent
		if err := json.Unmarshal([]byte(rawData), &ev); err != nil {
			continue
		}

		switch eventType {
		case "message_start":
			if ev.Message != nil {
				completionID = ev.Message.ID
				if ev.Message.Model != "" {
					model = ev.Message.Model
				}
			}
			// Send role delta
			if err := writeChunk(map[string]any{
				"id":      completionID,
				"object":  "chat.completion.chunk",
				"created": 0,
				"model":   model,
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{"role": "assistant", "content": ""},
					"finish_reason": nil,
				}},
			}); err != nil {
				return err
			}

		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				toolBlocks[ev.Index] = struct{ id, name string }{ev.ContentBlock.ID, ev.ContentBlock.Name}
				if err := writeChunk(map[string]any{
					"id":      completionID,
					"object":  "chat.completion.chunk",
					"created": 0,
					"model":   model,
					"choices": []map[string]any{{
						"index": 0,
						"delta": map[string]any{
							"tool_calls": []map[string]any{{
								"index": ev.Index,
								"id":    ev.ContentBlock.ID,
								"type":  "function",
								"function": map[string]any{
									"name":      ev.ContentBlock.Name,
									"arguments": "",
								},
							}},
						},
						"finish_reason": nil,
					}},
				}); err != nil {
					return err
				}
			}

		case "content_block_delta":
			var delta deltaPayload
			if err := json.Unmarshal(ev.Delta, &delta); err != nil {
				continue
			}
			switch delta.Type {
			case "text_delta":
				if err := writeChunk(map[string]any{
					"id":      completionID,
					"object":  "chat.completion.chunk",
					"created": 0,
					"model":   model,
					"choices": []map[string]any{{
						"index":         0,
						"delta":         map[string]any{"content": delta.Text},
						"finish_reason": nil,
					}},
				}); err != nil {
					return err
				}
			case "input_json_delta":
				if _, ok := toolBlocks[ev.Index]; ok {
					if err := writeChunk(map[string]any{
						"id":      completionID,
						"object":  "chat.completion.chunk",
						"created": 0,
						"model":   model,
						"choices": []map[string]any{{
							"index": 0,
							"delta": map[string]any{
								"tool_calls": []map[string]any{{
									"index":    ev.Index,
									"function": map[string]any{"arguments": delta.PartialJSON},
								}},
							},
							"finish_reason": nil,
						}},
					}); err != nil {
						return err
					}
				}
			}

		case "message_delta":
			var delta deltaPayload
			if err := json.Unmarshal(ev.Delta, &delta); err != nil {
				continue
			}
			finishReason := delta.StopReason
			if finishReason == "tool_use" {
				finishReason = "tool_calls"
			}
			if finishReason == "" {
				finishReason = "stop"
			}
			// Usage chunk
			usage := map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
			if ev.Usage != nil {
				usage["completion_tokens"] = ev.Usage.OutputTokens
				usage["total_tokens"] = ev.Usage.OutputTokens
			}
			if err := writeChunk(map[string]any{
				"id":      completionID,
				"object":  "chat.completion.chunk",
				"created": 0,
				"model":   model,
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{},
					"finish_reason": finishReason,
				}},
				"usage": usage,
			}); err != nil {
				return err
			}

		case "message_stop":
			fmt.Fprintf(dst, "data: [DONE]\n\n") //nolint:errcheck
			if f, ok := dst.(interface{ Flush() }); ok {
				f.Flush()
			}
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	// Send DONE if message_stop was never seen
	fmt.Fprintf(dst, "data: [DONE]\n\n") //nolint:errcheck
	if f, ok := dst.(interface{ Flush() }); ok {
		f.Flush()
	}
	return nil
}

func decodeAnthropicError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var envelope anthropicErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return &UpstreamError{
			StatusCode: resp.StatusCode,
			Message:    envelope.Error.Message,
			Type:       envelope.Error.Type,
		}
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	return &UpstreamError{
		StatusCode: resp.StatusCode,
		Message:    message,
		Type:       "anthropic_error",
	}
}
