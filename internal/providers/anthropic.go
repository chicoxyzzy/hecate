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
}

type anthropicMessagesMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
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
	content := anthropicTextContent(wireResp.Content)
	return &types.ChatResponse{
		ID:        wireResp.ID,
		Model:     model,
		CreatedAt: time.Now().UTC(),
		Choices: []types.ChatChoice{{
			Index: 0,
			Message: types.Message{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: wireResp.StopReason,
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
	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		switch role {
		case "system":
			if text := strings.TrimSpace(msg.Content); text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant", "user":
			wire = append(wire, anthropicMessage{
				Role: role,
				Content: []anthropicContentBlock{{
					Type: "text",
					Text: msg.Content,
				}},
			})
		}
	}
	return strings.Join(systemParts, "\n\n"), wire
}

func anthropicTextContent(items []anthropicContentBlock) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n")
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
