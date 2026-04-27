package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/auth"
	"github.com/hecate/agent-runtime/internal/taskstate"
)

// HandleEvents serves GET /v1/events — a paginated cross-run feed of
// task events. Useful for external dashboards (Grafana, Slack
// notifiers, audit log shippers) that want a single subscription
// rather than per-run polling.
//
// Query parameters:
//   - event_type: comma-separated allowlist (e.g. "agent.turn.completed,run.finished")
//   - task_id:    optional single task scope
//   - after_sequence: cursor; only events with sequence > this are returned
//   - limit:      max items, default 200, capped at 500
//
// Auth: any authenticated principal. Non-admin principals are
// auto-scoped to their tenant — we look up their tenant tasks and
// constrain the underlying store query to those task IDs. Admins see
// all events across all tenants.
func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	filter, errMsg := buildEventFilterFromRequest(r)
	if errMsg != "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, errMsg)
		return
	}
	proceed, scopedToNothing := applyTenantScopeForEvents(ctx, h, principal, &filter, w)
	if !proceed {
		return
	}
	if scopedToNothing {
		WriteJSON(w, http.StatusOK, EventsResponse{Object: "events", Data: []TaskRunEventItem{}})
		return
	}

	events, err := h.taskStore.ListEvents(ctx, filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	items := make([]TaskRunEventItem, 0, len(events))
	var nextSeq int64
	for _, event := range events {
		items = append(items, renderTaskRunEvent(event))
		if event.Sequence > nextSeq {
			nextSeq = event.Sequence
		}
	}
	WriteJSON(w, http.StatusOK, EventsResponse{
		Object:            "events",
		Data:              items,
		NextAfterSequence: nextSeq,
	})
}

// HandleEventsStream serves GET /v1/events/stream — a long-lived SSE
// connection that flushes new events as they're appended. Each
// message is one event; the SSE `id` field is the event sequence so
// reconnects via `Last-Event-ID` are seamless.
//
// Same auth + scope rules as HandleEvents. Non-admin tenant
// constraints are re-resolved every poll iteration to pick up newly
// created tasks during the stream's lifetime.
func (h *Handler) HandleEventsStream(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, "streaming not supported by server")
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	baseFilter, errMsg := buildEventFilterFromRequest(r)
	if errMsg != "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, errMsg)
		return
	}
	// Resume cursor: prefer Last-Event-ID over after_sequence so
	// browser EventSource reconnects pick up automatically.
	if v := strings.TrimSpace(r.Header.Get("Last-Event-ID")); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > baseFilter.AfterSequence {
			baseFilter.AfterSequence = parsed
		}
	}

	writeSSEHeaders(w)
	// Send a comment line to flush headers immediately. Without this,
	// some proxies hold the connection until the first event, which
	// looks like a hang on the client side.
	fmt.Fprintln(w, ": ok")
	flusher.Flush()

	pollInterval := 250 * time.Millisecond
	heartbeatInterval := 15 * time.Second
	lastHeartbeat := time.Now()
	cursor := baseFilter.AfterSequence

	for {
		if err := ctx.Err(); err != nil {
			return
		}

		// Re-scope per iteration so newly created tenant tasks are
		// picked up mid-stream. For admins this is a no-op. For
		// tenants we use the same empty-vs-nil-safe intersection
		// logic as the polling endpoint — passing an empty TaskIDs
		// to the store would match everything in some implementations
		// but nothing in others, so we short-circuit explicitly.
		filter := baseFilter
		filter.AfterSequence = cursor
		if !principal.IsAdmin() && strings.TrimSpace(principal.Tenant) != "" {
			ids, err := loadTenantTaskIDs(ctx, h, principal.Tenant)
			if err != nil {
				fmt.Fprintf(w, "event: error\ndata: {\"error\":{\"message\":%q}}\n\n", err.Error())
				flusher.Flush()
				return
			}
			if len(ids) == 0 {
				// Tenant has no tasks yet — nothing to stream this
				// iteration; sleep with cancel awareness and retry.
				if !sleepWithContext(ctx, pollInterval) {
					return
				}
				continue
			}
			if len(baseFilter.TaskIDs) > 0 {
				// Intersect with the tenant's set; if the caller
				// asked exclusively for foreign tasks, idle out.
				allowed := make(map[string]struct{}, len(ids))
				for _, id := range ids {
					allowed[id] = struct{}{}
				}
				intersected := make([]string, 0, len(baseFilter.TaskIDs))
				for _, id := range baseFilter.TaskIDs {
					if _, ok := allowed[id]; ok {
						intersected = append(intersected, id)
					}
				}
				if len(intersected) == 0 {
					if !sleepWithContext(ctx, pollInterval) {
						return
					}
					continue
				}
				filter.TaskIDs = intersected
			} else {
				filter.TaskIDs = ids
			}
		}

		events, err := h.taskStore.ListEvents(ctx, filter)
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: {\"error\":{\"message\":%q}}\n\n", err.Error())
			flusher.Flush()
			return
		}
		for _, event := range events {
			payload, marshalErr := json.Marshal(map[string]any{
				"object": "event",
				"data":   renderTaskRunEvent(event),
			})
			if marshalErr != nil {
				continue
			}
			fmt.Fprintf(w, "id: %d\nevent: event\ndata: %s\n\n", event.Sequence, payload)
			cursor = event.Sequence
			lastHeartbeat = time.Now()
		}
		flusher.Flush()

		// Keep idle connections warm so proxies / load balancers
		// don't time them out. Only emit when nothing else has
		// flushed in the heartbeat window.
		if time.Since(lastHeartbeat) >= heartbeatInterval {
			fmt.Fprintln(w, ": heartbeat")
			flusher.Flush()
			lastHeartbeat = time.Now()
		}

		if !sleepWithContext(ctx, pollInterval) {
			return
		}
	}
}

// buildEventFilterFromRequest parses the public-events query string.
// Returns a filter with default Limit when none is supplied; on
// validation failure returns an empty filter and a human-readable
// error message for a 400 response.
func buildEventFilterFromRequest(r *http.Request) (taskstate.EventFilter, string) {
	q := r.URL.Query()
	filter := taskstate.EventFilter{}

	if raw := strings.TrimSpace(q.Get("event_type")); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				filter.EventTypes = append(filter.EventTypes, t)
			}
		}
	}
	if raw := strings.TrimSpace(q.Get("task_id")); raw != "" {
		filter.TaskIDs = []string{raw}
	}
	if raw := strings.TrimSpace(q.Get("after_sequence")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			return taskstate.EventFilter{}, "after_sequence must be a non-negative integer"
		}
		filter.AfterSequence = parsed
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return taskstate.EventFilter{}, "limit must be a positive integer"
		}
		// Cap at 500 to keep responses bounded; rate-limited polling
		// + the cursor-based design still lets clients drain history.
		if parsed > 500 {
			parsed = 500
		}
		filter.Limit = parsed
	}
	if filter.Limit == 0 {
		filter.Limit = 200
	}
	return filter, ""
}

// applyTenantScopeForEvents constrains a non-admin principal's filter
// to their tenant's task IDs. Returns (proceed, scopedToNothing):
//   - proceed=false means an error response was already written.
//   - scopedToNothing=true means the principal's effective scope is
//     empty (no tenant tasks, or asked for a foreign task_id). The
//     caller should write a successful empty response WITHOUT calling
//     the store — passing an empty TaskIDs slice to ListEvents would
//     match nothing in some store implementations but everything in
//     others (the empty-vs-nil ambiguity is a footgun), so we
//     short-circuit here instead.
func applyTenantScopeForEvents(ctx context.Context, h *Handler, principal auth.Principal, filter *taskstate.EventFilter, w http.ResponseWriter) (proceed bool, scopedToNothing bool) {
	if principal.IsAdmin() {
		return true, false
	}
	tenant := strings.TrimSpace(principal.Tenant)
	if tenant == "" {
		// Anonymous (no auth configured) — let it through untouched
		// so dev setups can subscribe without a tenant claim.
		return true, false
	}
	ids, err := loadTenantTaskIDs(ctx, h, tenant)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return false, false
	}
	if len(ids) == 0 {
		// Tenant has no tasks at all — empty result is the answer.
		return true, true
	}
	if len(filter.TaskIDs) > 0 {
		// Caller asked for specific tasks; only allow those that
		// belong to their tenant. Intersect with the tenant's set.
		allowed := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			allowed[id] = struct{}{}
		}
		intersected := make([]string, 0, len(filter.TaskIDs))
		for _, id := range filter.TaskIDs {
			if _, ok := allowed[id]; ok {
				intersected = append(intersected, id)
			}
		}
		if len(intersected) == 0 {
			// Caller asked only for foreign tasks; deny via empty
			// response (rather than 403, which would leak existence).
			return true, true
		}
		filter.TaskIDs = intersected
	} else {
		filter.TaskIDs = ids
	}
	return true, false
}

func loadTenantTaskIDs(ctx context.Context, h *Handler, tenant string) ([]string, error) {
	tasks, err := h.taskStore.ListTasks(ctx, taskstate.TaskFilter{Tenant: tenant, Limit: 5000})
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.ID)
	}
	return ids, nil
}

// sleepWithContext sleeps for d or returns false if ctx is cancelled
// first. Lets the SSE poll loop exit promptly when the client
// disconnects rather than always waiting the full poll interval.
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
