package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/chatstate"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

func (h *Handler) HandleCreateChatSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	var req CreateChatSessionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "New chat"
	}

	session := types.ChatSession{
		ID:        newChatSessionID(),
		Title:     title,
		Tenant:    principal.Tenant,
		User:      principal.Name,
		CreatedAt: time.Now().UTC(),
	}
	result, err := h.service.CreateChatSession(ctx, session)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.chat.sessions.create.failed",
			slog.String("event.name", "gateway.chat.sessions.create.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, ChatSessionResponse{
		Object: "chat_session",
		Data:   renderChatSession(result.Session),
	})
}

func (h *Handler) HandleChatSessions(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	limit := h.config.Chat.SessionLimit
	if limit <= 0 {
		limit = 50
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "limit query parameter must be a non-negative integer")
			return
		}
		if value > 200 {
			value = 200
		}
		limit = value
	}

	filter := chatstate.Filter{Limit: limit}
	if principal.IsAdmin() {
		filter.Tenant = strings.TrimSpace(r.URL.Query().Get("tenant"))
	} else {
		filter.Tenant = principal.Tenant
	}

	result, err := h.service.ListChatSessions(ctx, filter)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.chat.sessions.list.failed",
			slog.String("event.name", "gateway.chat.sessions.list.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	items := make([]ChatSessionSummaryItem, 0, len(result.Sessions))
	for _, session := range result.Sessions {
		if !principal.IsAdmin() && principal.Tenant != "" && session.Tenant != principal.Tenant {
			continue
		}
		items = append(items, renderChatSessionSummary(session))
	}
	WriteJSON(w, http.StatusOK, ChatSessionsResponse{
		Object: "chat_sessions",
		Data:   items,
	})
}

func (h *Handler) HandleChatSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "session id is required")
		return
	}

	result, err := h.service.GetChatSession(ctx, id)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.chat.sessions.get.failed",
			slog.String("event.name", "gateway.chat.sessions.get.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusNotFound, errCodeInvalidRequest, "chat session not found")
		return
	}
	if !principal.IsAdmin() && principal.Tenant != "" && result.Session.Tenant != principal.Tenant {
		WriteError(w, http.StatusForbidden, errCodeForbidden, "chat session is outside the active tenant scope")
		return
	}

	WriteJSON(w, http.StatusOK, ChatSessionResponse{
		Object: "chat_session",
		Data:   renderChatSession(result.Session),
	})
}

func renderChatSessionSummary(session types.ChatSession) ChatSessionSummaryItem {
	item := ChatSessionSummaryItem{
		ID:        session.ID,
		Title:     session.Title,
		Tenant:    session.Tenant,
		User:      session.User,
		TurnCount: len(session.Turns),
	}
	if !session.CreatedAt.IsZero() {
		item.CreatedAt = session.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !session.UpdatedAt.IsZero() {
		item.UpdatedAt = session.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if len(session.Turns) > 0 {
		last := session.Turns[len(session.Turns)-1]
		item.LastModel = last.Model
		item.LastProvider = last.Provider
		item.LastCostUSD = formatUSD(last.CostMicrosUSD)
		item.LastRequestID = last.RequestID
	}
	return item
}

func renderChatSession(session types.ChatSession) ChatSessionItem {
	item := ChatSessionItem{
		ID:     session.ID,
		Title:  session.Title,
		Tenant: session.Tenant,
		User:   session.User,
		Turns:  make([]ChatSessionTurnItem, 0, len(session.Turns)),
	}
	if !session.CreatedAt.IsZero() {
		item.CreatedAt = session.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !session.UpdatedAt.IsZero() {
		item.UpdatedAt = session.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	for _, turn := range session.Turns {
		record := ChatSessionTurnItem{
			ID:                turn.ID,
			RequestID:         turn.RequestID,
			UserMessage:       OpenAIChatMessage(turn.UserMessage),
			AssistantMessage:  OpenAIChatMessage(turn.AssistantMessage),
			RequestedProvider: turn.RequestedProvider,
			Provider:          turn.Provider,
			ProviderKind:      turn.ProviderKind,
			RequestedModel:    turn.RequestedModel,
			Model:             turn.Model,
			CostMicrosUSD:     turn.CostMicrosUSD,
			CostUSD:           formatUSD(turn.CostMicrosUSD),
			PromptTokens:      turn.PromptTokens,
			CompletionTokens:  turn.CompletionTokens,
			TotalTokens:       turn.TotalTokens,
		}
		if !turn.CreatedAt.IsZero() {
			record.CreatedAt = turn.CreatedAt.UTC().Format(time.RFC3339Nano)
		}
		item.Turns = append(item.Turns, record)
	}
	return item
}

func newChatSessionID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "chat-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return "chat_" + hex.EncodeToString(buf)
}
