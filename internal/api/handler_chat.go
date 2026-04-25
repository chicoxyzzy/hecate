package api

import (
	"context"
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
	if !h.checkRateLimit(w, principal.KeyID) {
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

	if internalReq.Stream {
		h.handleChatCompletionsStream(w, r, ctx, internalReq)
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
		if gateway.IsBudgetExceededError(err) {
			WriteError(w, http.StatusPaymentRequired, "budget_exceeded", err.Error())
			return
		}
		if gateway.IsRateLimitedError(err) {
			WriteError(w, http.StatusTooManyRequests, "rate_limit_exceeded", err.Error())
			return
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

	if internalReq.SessionID != "" {
		if _, err := h.service.RecordChatTurn(ctx, internalReq.SessionID, internalReq, result); err != nil {
			telemetry.Warn(h.logger, ctx, "gateway.chat.sessions.record_failed",
				slog.String("event.name", "gateway.chat.sessions.record_failed"),
				slog.String("hecate.chat.session_id", internalReq.SessionID),
				slog.Any("error", err),
			)
		}
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

func (h *Handler) handleChatCompletionsStream(w http.ResponseWriter, r *http.Request, ctx context.Context, req types.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, "streaming not supported by server")
		return
	}

	// Route first — no bytes written yet, so errors can still be JSON.
	handle, streamCtx, err := h.service.RouteForStream(ctx, req)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gen_ai.gateway.stream.route_failed",
			slog.String("event.name", "gen_ai.gateway.stream.route_failed"),
			slog.String(telemetry.AttrGenAIRequestModel, req.Model),
			slog.Any("error", err),
		)
		statusCode := http.StatusInternalServerError
		if gateway.IsClientError(err) {
			statusCode = http.StatusBadRequest
		}
		if gateway.IsBudgetExceededError(err) {
			WriteError(w, http.StatusPaymentRequired, "budget_exceeded", err.Error())
			return
		}
		if gateway.IsRateLimitedError(err) {
			WriteError(w, http.StatusTooManyRequests, "rate_limit_exceeded", err.Error())
			return
		}
		if gateway.IsDeniedError(err) {
			statusCode = http.StatusForbidden
		}
		errMsg := err.Error()
		var upstreamErr *providers.UpstreamError
		if errors.As(err, &upstreamErr) {
			statusCode = mapUpstreamStatus(upstreamErr.StatusCode)
			errMsg = upstreamErr.Message
		}
		WriteError(w, statusCode, errCodeGatewayError, errMsg)
		return
	}

	// Routing succeeded — now commit to SSE.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Runtime-Provider", handle.Metadata.Provider)
	w.Header().Set("X-Runtime-Provider-Kind", handle.Metadata.ProviderKind)
	w.Header().Set("X-Runtime-Route-Reason", handle.Metadata.RouteReason)
	w.Header().Set("X-Runtime-Requested-Model", handle.Metadata.RequestedModel)
	w.Header().Set("X-Runtime-Model", handle.Metadata.Model)
	w.Header().Set("X-Trace-Id", handle.Metadata.TraceID)
	w.Header().Set("X-Span-Id", handle.Metadata.SpanID)
	w.WriteHeader(http.StatusOK)

	captured, err := handle.ExecuteAndCapture(flushWriter{w, flusher})
	if err != nil {
		telemetry.Error(h.logger, streamCtx, "gen_ai.gateway.stream.failed",
			slog.String("event.name", "gen_ai.gateway.stream.failed"),
			slog.String(telemetry.AttrGenAIRequestModel, req.Model),
			slog.Any("error", err),
		)
		// Headers already sent; write a terminal SSE error event.
		errMsg := err.Error()
		var upstreamErr *providers.UpstreamError
		if errors.As(err, &upstreamErr) {
			errMsg = upstreamErr.Message
		}
		fmt.Fprintf(w, "data: {\"error\":{\"message\":%q}}\n\ndata: [DONE]\n\n", errMsg)
		flusher.Flush()
		return
	}

	if req.SessionID != "" && captured.Content != "" {
		resolvedModel := captured.Model
		if resolvedModel == "" {
			resolvedModel = handle.Metadata.Model
		}
		syntheticResult := &gateway.ChatResult{
			Response: &types.ChatResponse{
				ID:    handle.Metadata.RequestID,
				Model: resolvedModel,
				Choices: []types.ChatChoice{{
					Index:        0,
					Message:      types.Message{Role: "assistant", Content: captured.Content},
					FinishReason: captured.FinishReason,
				}},
			},
			Metadata: gateway.ResponseMetadata{
				RequestID:    handle.Metadata.RequestID,
				Provider:     handle.Metadata.Provider,
				ProviderKind: handle.Metadata.ProviderKind,
				RouteReason:  handle.Metadata.RouteReason,
				Model:        resolvedModel,
			},
		}
		if _, err := h.service.RecordChatTurn(streamCtx, req.SessionID, req, syntheticResult); err != nil {
			telemetry.Warn(h.logger, streamCtx, "gateway.chat.sessions.stream_record_failed",
				slog.String("event.name", "gateway.chat.sessions.stream_record_failed"),
				slog.String("hecate.chat.session_id", req.SessionID),
				slog.Any("error", err),
			)
		}
	}
}

type flushWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (fw flushWriter) Write(p []byte) (int, error) { return fw.w.Write(p) }
func (fw flushWriter) Flush()                      { fw.flusher.Flush() }

func normalizeChatRequest(req OpenAIChatCompletionRequest, requestID string, principal auth.Principal) (types.ChatRequest, error) {
	messages := make([]types.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		content := ""
		if msg.Content != nil {
			content = *msg.Content
		}
		m := types.Message{
			Role:       msg.Role,
			Content:    content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			m.ToolCalls = make([]types.ToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
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
		messages = append(messages, m)
	}

	tools := make([]types.Tool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, types.Tool{
			Type: t.Type,
			Function: types.ToolFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
				Strict:      t.Function.Strict,
			},
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
		RequestID:    requestID,
		SessionID:    req.SessionID,
		SessionTitle: req.SessionTitle,
		Model:        req.Model,
		Messages:     messages,
		Temperature:  req.Temperature,
		MaxTokens:    req.MaxTokens,
		Scope:        scope,
		Tools:        tools,
		ToolChoice:   req.ToolChoice,
		Stream:       req.Stream,
	}, nil
}

func renderChatCompletionResponse(resp *types.ChatResponse) OpenAIChatCompletionResponse {
	choices := make([]OpenAIChatCompletionChoice, 0, len(resp.Choices))
	for _, choice := range resp.Choices {
		msg := OpenAIChatMessage{
			Role: choice.Message.Role,
			Name: choice.Message.Name,
		}
		if len(choice.Message.ToolCalls) > 0 {
			// null content is correct when tool_calls are present
			msg.ToolCalls = make([]OpenAIToolCall, 0, len(choice.Message.ToolCalls))
			for _, tc := range choice.Message.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, OpenAIToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: OpenAIToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		} else {
			c := choice.Message.Content
			msg.Content = &c
		}
		choices = append(choices, OpenAIChatCompletionChoice{
			Index:        choice.Index,
			Message:      msg,
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

func messageToWire(msg types.Message) OpenAIChatMessage {
	wire := OpenAIChatMessage{
		Role:       msg.Role,
		Name:       msg.Name,
		ToolCallID: msg.ToolCallID,
	}
	if len(msg.ToolCalls) > 0 {
		wire.ToolCalls = make([]OpenAIToolCall, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			wire.ToolCalls = append(wire.ToolCalls, OpenAIToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: OpenAIToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	} else {
		c := msg.Content
		wire.Content = &c
	}
	return wire
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
