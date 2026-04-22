package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/hecate/agent-runtime/internal/auth"
	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/requestscope"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	var wireReq OpenAIChatCompletionRequest
	if !decodeJSON(w, r, &wireReq) {
		return
	}

	internalReq, err := normalizeChatRequest(wireReq, RequestIDFromContext(ctx), principal)
	if err != nil {
		WriteError(w, http.StatusForbidden, errCodeForbidden, err.Error())
		return
	}
	result, err := h.service.HandleChat(ctx, internalReq)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gen_ai.gateway.request.failed",
			slog.String("event.name", "gen_ai.gateway.request.failed"),
			slog.String(telemetry.AttrGenAIRequestModel, internalReq.Model),
			slog.Any("error", err),
		)

		statusCode := http.StatusInternalServerError
		if gateway.IsClientError(err) {
			statusCode = http.StatusBadRequest
		}
		if gateway.IsDeniedError(err) {
			statusCode = http.StatusForbidden
		}
		var upstreamErr *providers.UpstreamError
		if errors.As(err, &upstreamErr) {
			statusCode = mapUpstreamStatus(upstreamErr.StatusCode)
			WriteError(w, statusCode, errCodeUpstreamError, upstreamErr.Message)
			return
		}

		WriteError(w, statusCode, errCodeGatewayError, err.Error())
		return
	}

	wireResp := renderChatCompletionResponse(result.Response)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Runtime-Provider", result.Metadata.Provider)
	w.Header().Set("X-Runtime-Provider-Kind", result.Metadata.ProviderKind)
	w.Header().Set("X-Runtime-Route-Reason", result.Metadata.RouteReason)
	w.Header().Set("X-Runtime-Requested-Model", result.Metadata.RequestedModel)
	w.Header().Set("X-Runtime-Requested-Model-Canonical", result.Metadata.CanonicalRequestedModel)
	w.Header().Set("X-Runtime-Model", result.Metadata.Model)
	w.Header().Set("X-Runtime-Model-Canonical", result.Metadata.CanonicalResolvedModel)
	w.Header().Set("X-Runtime-Cache", strconv.FormatBool(result.Metadata.CacheHit))
	w.Header().Set("X-Runtime-Cache-Type", result.Metadata.CacheType)
	w.Header().Set("X-Trace-Id", result.Metadata.TraceID)
	w.Header().Set("X-Span-Id", result.Metadata.SpanID)
	if result.Metadata.SemanticStrategy != "" {
		w.Header().Set("X-Runtime-Semantic-Strategy", result.Metadata.SemanticStrategy)
	}
	if result.Metadata.SemanticIndexType != "" {
		w.Header().Set("X-Runtime-Semantic-Index", result.Metadata.SemanticIndexType)
	}
	if result.Metadata.SemanticSimilarity > 0 {
		w.Header().Set("X-Runtime-Semantic-Similarity", fmt.Sprintf("%.6f", result.Metadata.SemanticSimilarity))
	}
	w.Header().Set("X-Runtime-Attempts", strconv.Itoa(result.Metadata.AttemptCount))
	w.Header().Set("X-Runtime-Retries", strconv.Itoa(result.Metadata.RetryCount))
	if result.Metadata.FallbackFromProvider != "" {
		w.Header().Set("X-Runtime-Fallback-From", result.Metadata.FallbackFromProvider)
	}
	w.Header().Set("X-Runtime-Cost-USD", formatUSD(result.Metadata.CostMicrosUSD))
	WriteJSON(w, http.StatusOK, wireResp)
}

func normalizeChatRequest(req OpenAIChatCompletionRequest, requestID string, principal auth.Principal) (types.ChatRequest, error) {
	messages := make([]types.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, types.Message{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		})
	}

	tenant := req.User
	if principal.Tenant != "" {
		if req.User != "" && req.User != principal.Tenant {
			return types.ChatRequest{}, fmt.Errorf("api key is bound to tenant %q and cannot act as %q", principal.Tenant, req.User)
		}
		tenant = principal.Tenant
	}

	scope := requestscope.Build(principal, tenant, req.Provider)

	return types.ChatRequest{
		RequestID:   requestID,
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Scope:       scope,
	}, nil
}

func renderChatCompletionResponse(resp *types.ChatResponse) OpenAIChatCompletionResponse {
	choices := make([]OpenAIChatCompletionChoice, 0, len(resp.Choices))
	for _, choice := range resp.Choices {
		choices = append(choices, OpenAIChatCompletionChoice{
			Index: choice.Index,
			Message: OpenAIChatMessage{
				Role:    choice.Message.Role,
				Content: choice.Message.Content,
				Name:    choice.Message.Name,
			},
			FinishReason: choice.FinishReason,
		})
	}

	return OpenAIChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: resp.CreatedAt.Unix(),
		Model:   resp.Model,
		Choices: choices,
		Usage: OpenAIUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}

func modelAllowedForPrincipal(principal auth.Principal, provider, model string) bool {
	if principal.IsAdmin() {
		return true
	}
	if len(principal.AllowedProviders) > 0 {
		if !contains(principal.AllowedProviders, provider) {
			return false
		}
	}
	if len(principal.AllowedModels) > 0 {
		if !contains(principal.AllowedModels, model) {
			return false
		}
	}
	return true
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func mapUpstreamStatus(statusCode int) int {
	switch statusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		return statusCode
	default:
		return http.StatusBadGateway
	}
}
