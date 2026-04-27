package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/providers"
)

// ─── Tenant isolation defenses ─────────────────────────────────────────────
//
// Cross-tenant access is the security risk that's hardest to spot in code
// review — a single missing principal check on a handler silently exposes
// one customer's data to another. These tests construct two tenants and
// drive every mutation/read endpoint with the wrong tenant's bearer to
// pin the 403 response on each.
//
// The handler is configured with control-plane-backed API keys: every
// request must present a valid bearer.

// twoTenantSetup provisions two tenants (team-a, team-b) with their own
// API keys plus an admin token, and returns clients pre-bound to each.
type twoTenantSetup struct {
	handler http.Handler
	teamA   apiTestClient
	teamB   apiTestClient
	admin   apiTestClient
}

func newTwoTenantSetup(t *testing.T) twoTenantSetup {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cpStore := controlplane.NewMemoryStore()
	ctx := context.Background()
	for _, id := range []string{"team-a", "team-b"} {
		if _, err := cpStore.UpsertTenant(ctx, controlplane.Tenant{ID: id, Name: id, Enabled: true}); err != nil {
			t.Fatalf("UpsertTenant(%s): %v", id, err)
		}
		if _, err := cpStore.UpsertAPIKey(ctx, controlplane.APIKey{
			ID:      id,
			Name:    id,
			Key:     id + "-secret",
			Tenant:  id,
			Role:    "tenant",
			Enabled: true,
		}); err != nil {
			t.Fatalf("UpsertAPIKey(%s): %v", id, err)
		}
	}

	handler := newTestHTTPHandlerWithControlPlane(logger,
		[]providers.Provider{&fakeProvider{name: "openai"}},
		config.Config{Server: config.ServerConfig{AuthToken: "admin-secret"}},
		cpStore)

	return twoTenantSetup{
		handler: handler,
		teamA:   newAPITestClient(t, handler).withBearerToken("team-a-secret"),
		teamB:   newAPITestClient(t, handler).withBearerToken("team-b-secret"),
		admin:   newAPITestClient(t, handler).withBearerToken("admin-secret"),
	}
}

func TestChatSession_TenantBKeyCannotReadTenantASession(t *testing.T) {
	t.Parallel()
	s := newTwoTenantSetup(t)

	created := mustRequestJSON[ChatSessionResponse](s.teamA, http.MethodPost, "/v1/chat/sessions", `{"title":"a-only"}`)
	id := created.Data.ID

	// team-b's bearer must not reach the session — 403, not 200, not 404.
	// 404 would leak existence; 200 would leak content. 403 is the contract.
	rec := s.teamB.mustRequestStatus(http.StatusForbidden, http.MethodGet, "/v1/chat/sessions/"+id, "")
	if msg := decodeErrorMessage(t, rec.Body.Bytes()); !strings.Contains(msg, "outside the active tenant scope") {
		t.Errorf("error.message = %q, want tenant-scope error", msg)
	}

	// team-a still sees it.
	s.teamA.mustRequest(http.MethodGet, "/v1/chat/sessions/"+id, "")
}

func TestChatSession_TenantBKeyCannotDeleteTenantASession(t *testing.T) {
	t.Parallel()
	s := newTwoTenantSetup(t)

	created := mustRequestJSON[ChatSessionResponse](s.teamA, http.MethodPost, "/v1/chat/sessions", `{"title":"keep"}`)
	id := created.Data.ID

	s.teamB.mustRequestStatus(http.StatusForbidden, http.MethodDelete, "/v1/chat/sessions/"+id, "")
	// Side-effect check: the session must still exist after the rejected
	// delete attempt. A regression that 403-then-deletes would silently
	// nuke another tenant's data.
	s.teamA.mustRequest(http.MethodGet, "/v1/chat/sessions/"+id, "")
}

func TestChatSession_TenantBKeyCannotUpdateTenantASession(t *testing.T) {
	t.Parallel()
	s := newTwoTenantSetup(t)

	created := mustRequestJSON[ChatSessionResponse](s.teamA, http.MethodPost, "/v1/chat/sessions", `{"title":"original"}`)
	id := created.Data.ID

	s.teamB.mustRequestStatus(http.StatusForbidden, http.MethodPatch, "/v1/chat/sessions/"+id, `{"title":"hijacked"}`)
	// Side-effect check: the title is still the original.
	got := mustRequestJSON[ChatSessionResponse](s.teamA, http.MethodGet, "/v1/chat/sessions/"+id, "")
	if got.Data.Title != "original" {
		t.Errorf("title = %q, want original (rejected PATCH must not have written)", got.Data.Title)
	}
}

func TestChatSession_TenantBListExcludesTenantASessions(t *testing.T) {
	t.Parallel()
	s := newTwoTenantSetup(t)

	a1 := mustRequestJSON[ChatSessionResponse](s.teamA, http.MethodPost, "/v1/chat/sessions", `{"title":"a-1"}`)
	a2 := mustRequestJSON[ChatSessionResponse](s.teamA, http.MethodPost, "/v1/chat/sessions", `{"title":"a-2"}`)
	b1 := mustRequestJSON[ChatSessionResponse](s.teamB, http.MethodPost, "/v1/chat/sessions", `{"title":"b-1"}`)

	// team-b's list returns only b-1, never a-1 / a-2.
	listed := mustRequestJSON[ChatSessionsResponse](s.teamB, http.MethodGet, "/v1/chat/sessions", "")
	ids := make(map[string]bool)
	for _, item := range listed.Data {
		ids[item.ID] = true
	}
	if !ids[b1.Data.ID] {
		t.Errorf("team-b list missing own session %q", b1.Data.ID)
	}
	if ids[a1.Data.ID] || ids[a2.Data.ID] {
		t.Errorf("team-b list leaked team-a sessions: ids=%+v", ids)
	}
	if len(listed.Data) != 1 {
		t.Errorf("team-b list size = %d, want 1 (only own sessions)", len(listed.Data))
	}

	// team-a's list correspondingly returns only a-1 / a-2.
	listedA := mustRequestJSON[ChatSessionsResponse](s.teamA, http.MethodGet, "/v1/chat/sessions", "")
	idsA := make(map[string]bool)
	for _, item := range listedA.Data {
		idsA[item.ID] = true
	}
	if idsA[b1.Data.ID] {
		t.Errorf("team-a list leaked team-b session: ids=%+v", idsA)
	}
}

func TestChatSession_AdminCanReadAcrossTenants(t *testing.T) {
	t.Parallel()
	// Admin role sees every tenant — needed for support / audit. Pin
	// this so a regression that over-tightens the principal check
	// doesn't lock admins out of inspecting customer sessions during
	// an incident.
	s := newTwoTenantSetup(t)

	a := mustRequestJSON[ChatSessionResponse](s.teamA, http.MethodPost, "/v1/chat/sessions", `{"title":"a"}`)
	b := mustRequestJSON[ChatSessionResponse](s.teamB, http.MethodPost, "/v1/chat/sessions", `{"title":"b"}`)

	s.admin.mustRequest(http.MethodGet, "/v1/chat/sessions/"+a.Data.ID, "")
	s.admin.mustRequest(http.MethodGet, "/v1/chat/sessions/"+b.Data.ID, "")

	listed := mustRequestJSON[ChatSessionsResponse](s.admin, http.MethodGet, "/v1/chat/sessions", "")
	ids := make(map[string]bool)
	for _, item := range listed.Data {
		ids[item.ID] = true
	}
	if !ids[a.Data.ID] || !ids[b.Data.ID] {
		t.Errorf("admin list missing sessions: a=%v b=%v", ids[a.Data.ID], ids[b.Data.ID])
	}
}

// TestAdminEndpoints_TenantBearerReturnsUnauthorized pins the contract
// that admin-only endpoints reject tenant bearers — preventing a tenant
// key from reading global budget, request ledger, or admin retention
// runs even though those endpoints don't carry tenant data per-row.
func TestAdminEndpoints_TenantBearerReturnsUnauthorized(t *testing.T) {
	t.Parallel()
	s := newTwoTenantSetup(t)

	adminOnly := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/admin/budget", ""},
		{http.MethodGet, "/admin/requests", ""},
		{http.MethodGet, "/admin/retention/runs", ""},
		{http.MethodGet, "/admin/control-plane", ""},
		{http.MethodGet, "/admin/traces", ""},
	}
	for _, ep := range adminOnly {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			rec := s.teamA.mustRequestStatus(http.StatusUnauthorized, ep.method, ep.path, ep.body)
			var payload struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if payload.Error.Type != "unauthorized" {
				t.Errorf("error.type = %q, want unauthorized", payload.Error.Type)
			}
		})
	}
}
