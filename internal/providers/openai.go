package providers

import (
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
	User        string              `json:"user,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
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
		fallback := p.staticCapabilities("config_fallback")
		return fallback, err
	}

	p.mu.Lock()
	p.cachedCaps = discovered
	p.capsExpiry = time.Now().Add(time.Minute)
	p.mu.Unlock()
	return discovered, nil
}

func (p *OpenAICompatibleProvider) Supports(model string) bool {
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

func (p *OpenAICompatibleProvider) Chat(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	if !p.Supports(req.Model) {
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

func (p *OpenAICompatibleProvider) chatUpstream(ctx context.Context, req types.ChatRequest) (*types.ChatResponse, error) {
	if p.config.APIKey == "" && p.Kind() != KindLocal {
		return nil, fmt.Errorf("api key is required for cloud provider %s when stub mode is disabled", p.Name())
	}

	wireReq := openAIChatCompletionRequest{
		Model:       req.Model,
		Messages:    make([]openAIChatMessage, 0, len(req.Messages)),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		User:        requestscope.Normalize(req.Scope).User,
	}
	for _, msg := range req.Messages {
		wireReq.Messages = append(wireReq.Messages, openAIChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		})
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
		choices = append(choices, types.ChatChoice{
			Index: choice.Index,
			Message: types.Message{
				Role:    choice.Message.Role,
				Content: choice.Message.Content,
				Name:    choice.Message.Name,
			},
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
