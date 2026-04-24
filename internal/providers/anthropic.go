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
	Model         string                     `json:"model"`
	System        json.RawMessage            `json:"system,omitempty"` // string or [{type,text,cache_control}]
	Messages      []anthropicMessage         `json:"messages"`
	MaxTokens     int                        `json:"max_tokens"`
	Temperature   float64                    `json:"temperature,omitempty"`
	TopP          float64                    `json:"top_p,omitempty"`
	TopK          int                        `json:"top_k,omitempty"`
	StopSequences []string                   `json:"stop_sequences,omitempty"`
	Metadata      *anthropicMessagesMetadata `json:"metadata,omitempty"`
	Tools         []anthropicTool            `json:"tools,omitempty"`
	ToolChoice    json.RawMessage            `json:"tool_choice,omitempty"`
	Stream        bool                       `json:"stream,omitempty"`
	// Extended thinking: {"type":"enabled","budget_tokens":N}
	Thinking json.RawMessage `json:"thinking,omitempty"`
}

type anthropicMessagesMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

// anthropicContentBlock covers all block variants (text, tool_use, tool_result, thinking).
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
	// prompt caching
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
	// extended thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"` // redacted_thinking opaque data
}

type anthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
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
	return p.config.DefaultModel != "" && p.config.DefaultModel == model
}

func (p *AnthropicProvider) supportsResolvedModel(ctx context.Context, model string) bool {
	if model == "" {
		return p.config.DefaultModel != ""
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
	if discoveryUnconfigured(p.Kind(), p.config.APIKey) {
		cached := p.staticCapabilities("config_unconfigured")
		p.cachedCaps = cached
		p.capsExpiry = time.Now().Add(capabilitiesUnconfiguredTTL)
		p.mu.Unlock()
		return cached, nil
	}
	if !p.capsExpiry.IsZero() && time.Now().Before(p.capsExpiry) {
		cached := p.cachedCaps
		p.mu.Unlock()
		return cached, nil
	}
	p.mu.Unlock()

	discovered, err := p.discoverCapabilities(ctx)
	if err != nil {
		retryAfter := discoveryFailureTTL(p.Kind(), err)
		telemetry.Info(p.logger, ctx, "gateway.providers.capabilities.discovery_degraded",
			slog.String("event.name", "gateway.providers.capabilities.discovery_degraded"),
			slog.String("gen_ai.provider.name", p.Name()),
			slog.Duration("hecate.providers.capabilities.retry_after", retryAfter),
			slog.Any("error", err),
		)
		cached := p.staticCapabilities("config_fallback")
		p.mu.Lock()
		p.cachedCaps = cached
		p.capsExpiry = time.Now().Add(retryAfter)
		p.mu.Unlock()
		return cached, nil
	}

	p.mu.Lock()
	p.cachedCaps = discovered
	p.capsExpiry = time.Now().Add(capabilitiesSuccessTTL)
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
	models := make([]string, 0, 1)
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

func (p *AnthropicProvider) Validate() error {
	if p.config.APIKey == "" && p.Kind() != KindLocal && !p.config.StubMode {
		return fmt.Errorf("api key is required for cloud provider %s when stub mode is disabled", p.Name())
	}
	return nil
}

func (p *AnthropicProvider) chatUpstream(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	systemRaw, messages := anthropicMessagesFromTypes(req.Messages)
	if len(messages) == 0 {
		return nil, fmt.Errorf("anthropic messages request requires at least one non-system message")
	}
	wireReq := anthropicMessagesRequest{
		Model:         req.Model,
		System:        systemRaw,
		Messages:      messages,
		MaxTokens:     req.MaxTokens,
		TopP:          req.TopP,
		TopK:          req.TopK,
		StopSequences: append([]string(nil), req.StopSequences...),
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
	if len(req.Thinking) > 0 {
		wireReq.Thinking = req.Thinking
	}
	if len(req.Tools) > 0 {
		wireReq.Tools = make([]anthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Function.Parameters
			if len(schema) == 0 {
				schema = json.RawMessage(`{}`)
			}
			wireReq.Tools = append(wireReq.Tools, anthropicTool{
				Name:         t.Function.Name,
				Description:  t.Function.Description,
				InputSchema:  schema,
				CacheControl: t.CacheControl,
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
	if len(req.Betas) > 0 {
		httpReq.Header.Set("anthropic-beta", strings.Join(req.Betas, ","))
	}

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

// anthropicMessagesFromTypes converts internal messages to Anthropic wire format.
// Returns (system, messages) where system is a json.RawMessage (either a JSON string or
// a JSON array of text blocks, preserving cache_control) and messages is the conversation.
func anthropicMessagesFromTypes(messages []types.Message) (json.RawMessage, []anthropicMessage) {
	var systemRaw json.RawMessage
	wire := make([]anthropicMessage, 0, len(messages))

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		role := strings.TrimSpace(msg.Role)

		switch role {
		case "system":
			systemRaw = buildAnthropicSystemRaw(msg)

		case "assistant":
			if len(msg.ContentBlocks) > 0 {
				wire = append(wire, anthropicMessage{
					Role:    "assistant",
					Content: contentBlocksToAnthropicBlocks(msg.ContentBlocks),
				})
			} else if len(msg.ToolCalls) > 0 {
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
			blocks := []anthropicContentBlock{toolResultBlock(msg)}
			for i+1 < len(messages) && strings.TrimSpace(messages[i+1].Role) == "tool" {
				i++
				blocks = append(blocks, toolResultBlock(messages[i]))
			}
			wire = append(wire, anthropicMessage{Role: "user", Content: blocks})

		case "user":
			if len(msg.ContentBlocks) > 0 {
				wire = append(wire, anthropicMessage{
					Role:    "user",
					Content: contentBlocksToAnthropicBlocks(msg.ContentBlocks),
				})
			} else {
				wire = append(wire, anthropicMessage{
					Role:    "user",
					Content: []anthropicContentBlock{{Type: "text", Text: msg.Content}},
				})
			}
		}
	}
	return systemRaw, wire
}

// buildAnthropicSystemRaw marshals the system message into the Anthropic wire form:
// - a plain JSON string when there is a single un-cached text block
// - a JSON array of text blocks (with optional cache_control) otherwise
func buildAnthropicSystemRaw(msg types.Message) json.RawMessage {
	if len(msg.ContentBlocks) == 0 {
		if text := strings.TrimSpace(msg.Content); text != "" {
			b, _ := json.Marshal(text)
			return b
		}
		return nil
	}
	// Check whether any block has cache_control — if not and there is only one
	// text block, send a plain string (avoids unnecessary array wrapping).
	hasCacheControl := false
	for _, cb := range msg.ContentBlocks {
		if len(cb.CacheControl) > 0 {
			hasCacheControl = true
			break
		}
	}
	if !hasCacheControl && len(msg.ContentBlocks) == 1 && msg.ContentBlocks[0].Type == "text" {
		b, _ := json.Marshal(msg.ContentBlocks[0].Text)
		return b
	}
	type sysBlock struct {
		Type         string          `json:"type"`
		Text         string          `json:"text"`
		CacheControl json.RawMessage `json:"cache_control,omitempty"`
	}
	blocks := make([]sysBlock, 0, len(msg.ContentBlocks))
	for _, cb := range msg.ContentBlocks {
		if cb.Type == "" || cb.Type == "text" {
			blocks = append(blocks, sysBlock{
				Type:         "text",
				Text:         cb.Text,
				CacheControl: cb.CacheControl,
			})
		}
	}
	if len(blocks) == 0 {
		return nil
	}
	b, _ := json.Marshal(blocks)
	return b
}

// contentBlocksToAnthropicBlocks converts types.ContentBlock slice to the provider wire type.
func contentBlocksToAnthropicBlocks(cbs []types.ContentBlock) []anthropicContentBlock {
	out := make([]anthropicContentBlock, 0, len(cbs))
	for _, cb := range cbs {
		switch cb.Type {
		case "text", "":
			out = append(out, anthropicContentBlock{
				Type:         "text",
				Text:         cb.Text,
				CacheControl: cb.CacheControl,
			})
		case "tool_use":
			input := cb.Input
			if !json.Valid(input) || len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			out = append(out, anthropicContentBlock{
				Type:         "tool_use",
				ID:           cb.ID,
				Name:         cb.Name,
				Input:        input,
				CacheControl: cb.CacheControl,
			})
		case "thinking":
			out = append(out, anthropicContentBlock{
				Type:      "thinking",
				Thinking:  cb.Thinking,
				Signature: cb.Signature,
			})
		case "redacted_thinking":
			out = append(out, anthropicContentBlock{
				Type: "redacted_thinking",
				Data: cb.Data,
			})
		// tool_result is handled via the "tool" role path, not content blocks
		default:
			// pass unknown block types through verbatim so they reach the upstream
			out = append(out, anthropicContentBlock{
				Type:         cb.Type,
				CacheControl: cb.CacheControl,
			})
		}
	}
	return out
}

// toolResultBlock converts a tool-role message into a tool_result content block.
func toolResultBlock(msg types.Message) anthropicContentBlock {
	// If the message carries ContentBlocks for the result content, inline them
	// as a structured content array on the tool_result (Anthropic supports this).
	// For simplicity we just use the flattened Content string here; the SDK
	// typically sends plain text results.
	return anthropicContentBlock{
		Type:          "tool_result",
		ToolUseID:     msg.ToolCallID,
		ResultContent: msg.Content,
	}
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
			msg.ContentBlocks = append(msg.ContentBlocks, types.ContentBlock{
				Type: "text",
				Text: b.Text,
			})
		case "thinking":
			msg.ContentBlocks = append(msg.ContentBlocks, types.ContentBlock{
				Type:      "thinking",
				Thinking:  b.Thinking,
				Signature: b.Signature,
			})
		case "redacted_thinking":
			msg.ContentBlocks = append(msg.ContentBlocks, types.ContentBlock{
				Type: "redacted_thinking",
				Data: b.Data,
			})
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
			msg.ContentBlocks = append(msg.ContentBlocks, types.ContentBlock{
				Type:  "tool_use",
				ID:    b.ID,
				Name:  b.Name,
				Input: b.Input,
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
	if err := p.Validate(); err != nil {
		return err
	}

	systemRaw, messages := anthropicMessagesFromTypes(req.Messages)
	if len(messages) == 0 {
		return fmt.Errorf("anthropic messages request requires at least one non-system message")
	}
	wireReq := anthropicMessagesRequest{
		Model:         req.Model,
		System:        systemRaw,
		Messages:      messages,
		MaxTokens:     req.MaxTokens,
		TopP:          req.TopP,
		TopK:          req.TopK,
		StopSequences: append([]string(nil), req.StopSequences...),
		Stream:        true,
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
	if len(req.Thinking) > 0 {
		wireReq.Thinking = req.Thinking
	}
	if len(req.Tools) > 0 {
		wireReq.Tools = make([]anthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Function.Parameters
			if len(schema) == 0 {
				schema = json.RawMessage(`{}`)
			}
			wireReq.Tools = append(wireReq.Tools, anthropicTool{
				Name:         t.Function.Name,
				Description:  t.Function.Description,
				InputSchema:  schema,
				CacheControl: t.CacheControl,
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
	if len(req.Betas) > 0 {
		httpReq.Header.Set("anthropic-beta", strings.Join(req.Betas, ","))
	}

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
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	}

	var (
		completionID string
		// track open tool_use blocks by index
		toolBlocks = make(map[int]struct{ id, name string })
		// track open thinking blocks by index (value = true once opened)
		thinkingBlocks = make(map[int]bool)
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
		if err := ctx.Err(); err != nil {
			return err
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
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "thinking" {
				thinkingBlocks[ev.Index] = true
				// No OpenAI equivalent for thinking_start — the thinking content
				// is forwarded as x_thinking extension deltas below.
			}
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
			case "thinking_delta":
				if thinkingBlocks[ev.Index] {
					if err := writeChunk(map[string]any{
						"id":      completionID,
						"object":  "chat.completion.chunk",
						"created": 0,
						"model":   model,
						"choices": []map[string]any{{
							"index":         0,
							"delta":         map[string]any{"x_thinking": delta.Thinking},
							"finish_reason": nil,
						}},
					}); err != nil {
						return err
					}
				}
			case "signature_delta":
				if thinkingBlocks[ev.Index] {
					if err := writeChunk(map[string]any{
						"id":      completionID,
						"object":  "chat.completion.chunk",
						"created": 0,
						"model":   model,
						"choices": []map[string]any{{
							"index":         0,
							"delta":         map[string]any{"x_thinking_signature": delta.Signature},
							"finish_reason": nil,
						}},
					}); err != nil {
						return err
					}
				}
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

	// Prefer the context error when the scanner stopped due to an I/O error
	// caused by context cancellation (Go HTTP transport closes the response
	// body when the request context is done).
	if err := ctx.Err(); err != nil {
		return err
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
