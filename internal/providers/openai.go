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

type OpenAICompatibleProvider struct {
	config     config.OpenAICompatibleProviderConfig
	logger     *slog.Logger
	httpClient *http.Client
	mu         sync.Mutex
	cachedCaps Capabilities
	capsExpiry time.Time
}

type openAIChatCompletionRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
	TopP        float64             `json:"top_p,omitempty"`
	Stop        []string            `json:"stop,omitempty"`
	User        string              `json:"user,omitempty"`
	Tools       []openAITool        `json:"tools,omitempty"`
	ToolChoice  json.RawMessage     `json:"tool_choice,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type openAIToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIChatCompletionResponse struct {
	ID      string                       `json:"id"`
	Created int64                        `json:"created"`
	Model   string                       `json:"model"`
	Choices []openAIChatCompletionChoice `json:"choices"`
	Usage   openAIUsage                  `json:"usage"`
}

type openAIChatCompletionChoice struct {
	Index        int               `json:"index"`
	Message      openAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIErrorEnvelope struct {
	Error openAIErrorDetail `json:"error"`
}

type openAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

type openAIModelsResponse struct {
	Data []openAIModel `json:"data"`
}

type openAIModel struct {
	ID string `json:"id"`
}

type UpstreamError struct {
	StatusCode int
	Message    string
	Type       string
}

func (e *UpstreamError) Error() string {
	if e == nil {
		return ""
	}
	if e.Type == "" {
		return fmt.Sprintf("upstream error (%d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("upstream error (%d/%s): %s", e.StatusCode, e.Type, e.Message)
}

func NewOpenAICompatibleProvider(cfg config.OpenAICompatibleProviderConfig, logger *slog.Logger) *OpenAICompatibleProvider {
	return &OpenAICompatibleProvider{
		config: cfg,
		logger: logger,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func NewOpenAIProvider(cfg config.OpenAICompatibleProviderConfig, logger *slog.Logger) *OpenAICompatibleProvider {
	return NewOpenAICompatibleProvider(cfg, logger)
}

func (p *OpenAICompatibleProvider) Name() string {
	return p.config.Name
}

func (p *OpenAICompatibleProvider) Kind() Kind {
	if p.config.Kind == string(KindLocal) {
		return KindLocal
	}
	return KindCloud
}

func (p *OpenAICompatibleProvider) DefaultModel() string {
	return p.config.DefaultModel
}

func (p *OpenAICompatibleProvider) Capabilities(ctx context.Context) (Capabilities, error) {
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

func (p *OpenAICompatibleProvider) Supports(model string) bool {
	if model == "" {
		return p.config.DefaultModel != ""
	}
	return p.config.DefaultModel != "" && p.config.DefaultModel == model
}

func (p *OpenAICompatibleProvider) supportsResolvedModel(ctx context.Context, model string) bool {
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

func (p *OpenAICompatibleProvider) Chat(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	if !p.supportsResolvedModel(ctx, req.Model) {
		return nil, fmt.Errorf("model %q is not supported by provider %s", req.Model, p.Name())
	}

	if !p.config.StubMode {
		return p.chatUpstream(ctx, req)
	}

	content := p.config.StubResponse
	if last := lastUserMessage(req.Messages); last != "" {
		content = fmt.Sprintf("%s Echo: %s", p.config.StubResponse, last)
	}

	promptTokens := estimatePromptTokens(req.Messages)
	completionTokens := max(16, len(content)/4)
	now := time.Now().UTC()

	return &types.ChatResponse{
		ID:        "chatcmpl-stub",
		Model:     req.Model,
		CreatedAt: now,
		Choices: []types.ChatChoice{
			{
				Index: 0,
				Message: types.Message{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: types.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}, nil
}

func (p *OpenAICompatibleProvider) staticCapabilities(source string) Capabilities {
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

func (p *OpenAICompatibleProvider) discoverCapabilities(ctx context.Context) (Capabilities, error) {
	endpoint := buildModelsURL(p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Capabilities{}, fmt.Errorf("build models request: %w", err)
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return Capabilities{}, fmt.Errorf("send models request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return Capabilities{}, decodeUpstreamError(resp)
	}

	var payload openAIModelsResponse
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

func (p *OpenAICompatibleProvider) Validate() error {
	if p.config.APIKey == "" && p.Kind() != KindLocal && !p.config.StubMode {
		return fmt.Errorf("api key is required for cloud provider %s when stub mode is disabled", p.Name())
	}
	return nil
}

func (p *OpenAICompatibleProvider) chatUpstream(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	wireReq := openAIChatCompletionRequest{
		Model:       req.Model,
		Messages:    make([]openAIChatMessage, 0, len(req.Messages)),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        append([]string(nil), req.StopSequences...),
		User:        requestscope.Normalize(req.Scope).User,
		ToolChoice:  req.ToolChoice,
	}
	for _, msg := range req.Messages {
		wireMsg := openAIChatMessage{
			Role:       msg.Role,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			wireMsg.ToolCalls = make([]openAIToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				wireMsg.ToolCalls = append(wireMsg.ToolCalls, openAIToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: openAIToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		} else {
			c := msg.Content
			wireMsg.Content = &c
		}
		wireReq.Messages = append(wireReq.Messages, wireMsg)
	}
	if len(req.Tools) > 0 {
		wireReq.Tools = make([]openAITool, 0, len(req.Tools))
		for _, t := range req.Tools {
			wireReq.Tools = append(wireReq.Tools, openAITool{
				Type: t.Type,
				Function: openAIToolFunction{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
					Strict:      t.Function.Strict,
				},
			})
		}
	}

	payload, err := json.Marshal(wireReq)
	if err != nil {
		return nil, fmt.Errorf("marshal upstream request: %w", err)
	}

	endpoint := buildChatCompletionsURL(p.config.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send upstream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, decodeUpstreamError(resp)
	}

	var wireResp openAIChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&wireResp); err != nil {
		return nil, fmt.Errorf("decode upstream response: %w", err)
	}

	choices := make([]types.ChatChoice, 0, len(wireResp.Choices))
	for _, choice := range wireResp.Choices {
		content := ""
		if choice.Message.Content != nil {
			content = *choice.Message.Content
		}
		m := types.Message{
			Role:       choice.Message.Role,
			Content:    content,
			Name:       choice.Message.Name,
			ToolCallID: choice.Message.ToolCallID,
		}
		if len(choice.Message.ToolCalls) > 0 {
			m.ToolCalls = make([]types.ToolCall, 0, len(choice.Message.ToolCalls))
			for _, tc := range choice.Message.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, types.ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: types.ToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}
		choices = append(choices, types.ChatChoice{
			Index:        choice.Index,
			Message:      m,
			FinishReason: choice.FinishReason,
		})
	}

	createdAt := time.Now().UTC()
	if wireResp.Created > 0 {
		createdAt = time.Unix(wireResp.Created, 0).UTC()
	}

	model := wireResp.Model
	if model == "" {
		model = req.Model
	}

	return &types.ChatResponse{
		ID:        wireResp.ID,
		Model:     model,
		CreatedAt: createdAt,
		Choices:   choices,
		Usage: types.Usage{
			PromptTokens:     wireResp.Usage.PromptTokens,
			CompletionTokens: wireResp.Usage.CompletionTokens,
			TotalTokens:      wireResp.Usage.TotalTokens,
		},
	}, nil
}

func (p *OpenAICompatibleProvider) ChatStream(ctx context.Context, req types.ChatRequest, w io.Writer) error {
	if err := p.Validate(); err != nil {
		return err
	}

	wireReq := openAIChatCompletionRequest{
		Model:       req.Model,
		Messages:    make([]openAIChatMessage, 0, len(req.Messages)),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        append([]string(nil), req.StopSequences...),
		User:        requestscope.Normalize(req.Scope).User,
		ToolChoice:  req.ToolChoice,
		Stream:      true,
	}
	for _, msg := range req.Messages {
		wireMsg := openAIChatMessage{
			Role:       msg.Role,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			wireMsg.ToolCalls = make([]openAIToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				wireMsg.ToolCalls = append(wireMsg.ToolCalls, openAIToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: openAIToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		} else {
			c := msg.Content
			wireMsg.Content = &c
		}
		wireReq.Messages = append(wireReq.Messages, wireMsg)
	}
	if len(req.Tools) > 0 {
		wireReq.Tools = make([]openAITool, 0, len(req.Tools))
		for _, t := range req.Tools {
			wireReq.Tools = append(wireReq.Tools, openAITool{
				Type: t.Type,
				Function: openAIToolFunction{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
					Strict:      t.Function.Strict,
				},
			})
		}
	}

	payload, err := json.Marshal(wireReq)
	if err != nil {
		return fmt.Errorf("marshal upstream request: %w", err)
	}

	endpoint := buildChatCompletionsURL(p.config.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build upstream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send upstream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return decodeUpstreamError(resp)
	}

	return proxySSE(ctx, resp.Body, w)
}

func buildChatCompletionsURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/chat/completions"
	}
	return trimmed + "/v1/chat/completions"
}

func buildModelsURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/models"
	}
	return trimmed + "/v1/models"
}

func decodeUpstreamError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read upstream error body: %w", err)
	}

	var envelope openAIErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return &UpstreamError{
			StatusCode: resp.StatusCode,
			Message:    envelope.Error.Message,
			Type:       envelope.Error.Type,
		}
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}

	return &UpstreamError{
		StatusCode: resp.StatusCode,
		Message:    message,
	}
}

func lastUserMessage(messages []types.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func estimatePromptTokens(messages []types.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 4
	}
	return max(1, total)
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// proxySSE reads OpenAI-format SSE from src and writes it verbatim to dst,
// flushing after each blank-line boundary. It returns when [DONE] is seen,
// src is exhausted, or the context is cancelled.
func proxySSE(ctx context.Context, src io.Reader, dst io.Writer) error {
	scanner := bufio.NewScanner(src)
	for scanner.Scan() {
		// Check context cancellation before each write so we stop producing
		// output as soon as the client disconnects or the deadline expires.
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		if _, err := fmt.Fprintf(dst, "%s\n", line); err != nil {
			return err
		}
		// SSE events are separated by blank lines; flush after each blank line.
		if line == "" {
			if f, ok := dst.(interface{ Flush() }); ok {
				f.Flush()
			}
		}
		if line == "data: [DONE]" {
			// Write the trailing blank line and flush.
			fmt.Fprintf(dst, "\n") //nolint:errcheck
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
	return scanner.Err()
}
