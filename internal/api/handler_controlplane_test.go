package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/hecate/agent-runtime/internal/cache"
	"github.com/hecate/agent-runtime/internal/catalog"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/router"
	"github.com/hecate/agent-runtime/internal/telemetry"
)

// fakeProviderRuntime implements api.ProviderRuntime in memory so the
// HandleControlPlaneSetProviderEnabled / SetProviderAPIKey handlers can
// be exercised without the real loader plumbing (which pulls in a
// secret store, a registry rebuild, etc.). The fake records the calls
// so tests can assert on the payload that reaches it.
type fakeProviderRuntime struct {
	mu              sync.Mutex
	setEnabledCalls []struct {
		ID      string
		Enabled bool
	}
	rotateCalls []struct {
		ID  string
		Key string
	}
	deleteCredCalls []string
	provider        controlplane.Provider
	setEnabledErr   error
	rotateErr       error
	deleteErr       error
}

func (f *fakeProviderRuntime) Reload(_ context.Context) error { return nil }
func (f *fakeProviderRuntime) SecretStorageEnabled() bool     { return true }
func (f *fakeProviderRuntime) Upsert(_ context.Context, p controlplane.Provider, _ string) (controlplane.Provider, error) {
	return p, nil
}
func (f *fakeProviderRuntime) Delete(_ context.Context, _ string) error { return nil }

func (f *fakeProviderRuntime) SetEnabled(_ context.Context, id string, enabled bool) (controlplane.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setEnabledCalls = append(f.setEnabledCalls, struct {
		ID      string
		Enabled bool
	}{id, enabled})
	if f.setEnabledErr != nil {
		return controlplane.Provider{}, f.setEnabledErr
	}
	out := f.provider
	out.ID = id
	out.Enabled = enabled
	return out, nil
}

func (f *fakeProviderRuntime) RotateSecret(_ context.Context, id, key string) (controlplane.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotateCalls = append(f.rotateCalls, struct {
		ID  string
		Key string
	}{id, key})
	if f.rotateErr != nil {
		return controlplane.Provider{}, f.rotateErr
	}
	out := f.provider
	out.ID = id
	return out, nil
}

func (f *fakeProviderRuntime) DeleteCredential(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCredCalls = append(f.deleteCredCalls, id)
	return f.deleteErr
}

// Compile-time assertion: the fake satisfies the ProviderRuntime interface.
var _ ProviderRuntime = (*fakeProviderRuntime)(nil)

// newProviderRuntimeTestHandler wires a Handler with a real control-plane
// store + the fake provider runtime, then returns an admin-authenticated
// client and the fake so tests can assert on what the handler dispatched.
func newProviderRuntimeTestHandler(t *testing.T, runtime ProviderRuntime) (apiTestClient, controlplane.Store) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	prov := &fakeProvider{name: "openai"}
	registry := providers.NewRegistry(prov)
	providerCatalog := catalog.NewRegistryCatalog(registry, nil)
	store := controlplane.NewMemoryStore()
	cfg := config.Config{Server: config.ServerConfig{AuthToken: "admin-secret"}}
	service := gateway.NewService(gateway.Dependencies{
		Logger:    logger,
		Cache:     cache.NewMemoryStore(0),
		Router:    router.NewRuleRouter("gpt-4o-mini", providerCatalog),
		Catalog:   providerCatalog,
		Governor:  governor.NewStaticGovernor(mergeGovernorDefaults(cfg.Governor), governor.NewMemoryBudgetStore(), governor.NewMemoryBudgetStore()),
		Providers: registry,
		Tracer:    profiler.NewInMemoryTracer(nil),
		Metrics:   telemetry.NewMetrics(),
	})
	handler := NewHandler(cfg, logger, service, store, nil, nil, runtime)
	server := NewServer(logger, handler)
	return newAPITestClient(t, server).withBearerToken("admin-secret"), store
}

func TestControlPlaneSetProviderEnabledForwardsToRuntime(t *testing.T) {
	t.Parallel()
	rt := &fakeProviderRuntime{provider: controlplane.Provider{ID: "anthropic", Name: "Anthropic"}}
	admin, _ := newProviderRuntimeTestHandler(t, rt)

	rec := admin.mustRequest(http.MethodPatch, "/admin/control-plane/providers/anthropic", `{"enabled":false}`)
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var resp struct {
		Object string                     `json:"object"`
		Data   ControlPlaneProviderRecord `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Object != "control_plane_provider" {
		t.Errorf("object = %q, want control_plane_provider", resp.Object)
	}
	if resp.Data.ID != "anthropic" || resp.Data.Enabled {
		t.Errorf("data = %+v, want id=anthropic enabled=false", resp.Data)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.setEnabledCalls) != 1 {
		t.Fatalf("SetEnabled calls = %d, want 1", len(rt.setEnabledCalls))
	}
	call := rt.setEnabledCalls[0]
	if call.ID != "anthropic" || call.Enabled {
		t.Errorf("call = %+v, want anthropic/false", call)
	}
}

func TestControlPlaneSetProviderEnabledRequires400WhenRuntimeNotConfigured(t *testing.T) {
	t.Parallel()
	// Pass nil runtime so the handler falls into the
	// `dynamic provider runtime is not configured` branch — this is
	// the in-memory / file-config deployment path the UI must handle
	// gracefully rather than 500-ing.
	admin, _ := newProviderRuntimeTestHandler(t, nil)

	rec := admin.mustRequestStatus(http.StatusBadRequest, http.MethodPatch, "/admin/control-plane/providers/anthropic", `{"enabled":false}`)
	if !contains([]string{"invalid_request"}, decodeErrorType(t, rec.Body.Bytes())) {
		t.Errorf("error type = %q, want invalid_request", decodeErrorType(t, rec.Body.Bytes()))
	}
}

func TestControlPlaneSetProviderEnabledSurfacesRuntimeError(t *testing.T) {
	t.Parallel()
	rt := &fakeProviderRuntime{setEnabledErr: errors.New("provider id is unknown")}
	admin, _ := newProviderRuntimeTestHandler(t, rt)

	rec := admin.mustRequestStatus(http.StatusBadRequest, http.MethodPatch, "/admin/control-plane/providers/bogus", `{"enabled":true}`)
	// Errors from the runtime become 400 with their message verbatim —
	// useful for the operator's UI banner; otherwise they'd see a
	// generic "internal error".
	if msg := decodeErrorMessage(t, rec.Body.Bytes()); msg != "provider id is unknown" {
		t.Errorf("error.message = %q, want runtime error verbatim", msg)
	}
}

func TestControlPlaneSetProviderAPIKeyRotatesWhenKeyPresent(t *testing.T) {
	t.Parallel()
	rt := &fakeProviderRuntime{provider: controlplane.Provider{ID: "anthropic", Name: "Anthropic"}}
	admin, _ := newProviderRuntimeTestHandler(t, rt)

	rec := admin.mustRequest(http.MethodPut, "/admin/control-plane/providers/anthropic/api-key", `{"key":"sk-ant-new"}`)
	var resp struct {
		Object string                     `json:"object"`
		Data   ControlPlaneProviderRecord `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Object != "control_plane_provider_api_key" {
		t.Errorf("object = %q, want control_plane_provider_api_key", resp.Object)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.rotateCalls) != 1 {
		t.Fatalf("RotateSecret calls = %d, want 1", len(rt.rotateCalls))
	}
	if rt.rotateCalls[0].ID != "anthropic" || rt.rotateCalls[0].Key != "sk-ant-new" {
		t.Errorf("rotate call = %+v, want anthropic/sk-ant-new", rt.rotateCalls[0])
	}
	if len(rt.deleteCredCalls) != 0 {
		t.Errorf("DeleteCredential called %d times when key was non-empty; want 0", len(rt.deleteCredCalls))
	}
}

func TestControlPlaneSetProviderAPIKeyClearsWhenKeyEmpty(t *testing.T) {
	t.Parallel()
	// Empty key → DeleteCredential branch. The response contains a
	// {"id": ..., "status": "cleared"} stub rather than a full
	// provider record — the contract the UI relies on for the
	// "API key removed" toast.
	rt := &fakeProviderRuntime{}
	admin, _ := newProviderRuntimeTestHandler(t, rt)

	rec := admin.mustRequest(http.MethodPut, "/admin/control-plane/providers/anthropic/api-key", `{"key":""}`)
	var resp struct {
		Object string            `json:"object"`
		Data   map[string]string `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Data["id"] != "anthropic" || resp.Data["status"] != "cleared" {
		t.Errorf("data = %+v, want {id: anthropic, status: cleared}", resp.Data)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.deleteCredCalls) != 1 {
		t.Fatalf("DeleteCredential calls = %d, want 1", len(rt.deleteCredCalls))
	}
	if rt.deleteCredCalls[0] != "anthropic" {
		t.Errorf("delete call id = %q, want anthropic", rt.deleteCredCalls[0])
	}
	if len(rt.rotateCalls) != 0 {
		t.Errorf("RotateSecret called %d times when key was empty; want 0", len(rt.rotateCalls))
	}
}

func TestControlPlaneSetProviderAPIKeySurfacesRuntimeError(t *testing.T) {
	t.Parallel()
	rt := &fakeProviderRuntime{rotateErr: errors.New("secret store is read-only")}
	admin, _ := newProviderRuntimeTestHandler(t, rt)

	rec := admin.mustRequestStatus(http.StatusBadRequest, http.MethodPut, "/admin/control-plane/providers/anthropic/api-key", `{"key":"sk-ant"}`)
	if msg := decodeErrorMessage(t, rec.Body.Bytes()); msg != "secret store is read-only" {
		t.Errorf("error.message = %q, want runtime error verbatim", msg)
	}
}

func TestControlPlaneSetProviderAPIKeyRequires400WhenRuntimeNotConfigured(t *testing.T) {
	t.Parallel()
	admin, _ := newProviderRuntimeTestHandler(t, nil)

	admin.mustRequestStatus(http.StatusBadRequest, http.MethodPut, "/admin/control-plane/providers/anthropic/api-key", `{"key":"sk-ant"}`)
}

// TestControlPlaneSetProviderEnabledRejectsAnonymous and the API-key
// counterpart prove the auth gate fires before any handler-specific
// logic: a request with no bearer must 401, never invoke the runtime.
// Without these, a regression that drops `requireControlPlane` would
// open the dynamic-runtime endpoints to anyone.
func TestControlPlaneSetProviderEnabledRejectsAnonymous(t *testing.T) {
	t.Parallel()
	rt := &fakeProviderRuntime{}
	admin, _ := newProviderRuntimeTestHandler(t, rt)
	anon := apiTestClient{t: admin.t, handler: admin.handler} // strip the bearer

	anon.mustRequestStatus(http.StatusUnauthorized, http.MethodPatch, "/admin/control-plane/providers/anthropic", `{"enabled":false}`)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.setEnabledCalls) != 0 {
		t.Errorf("SetEnabled called %d times before auth; want 0", len(rt.setEnabledCalls))
	}
}

func TestControlPlaneSetProviderAPIKeyRejectsAnonymous(t *testing.T) {
	t.Parallel()
	rt := &fakeProviderRuntime{}
	admin, _ := newProviderRuntimeTestHandler(t, rt)
	anon := apiTestClient{t: admin.t, handler: admin.handler}

	anon.mustRequestStatus(http.StatusUnauthorized, http.MethodPut, "/admin/control-plane/providers/anthropic/api-key", `{"key":"sk-ant"}`)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.rotateCalls) != 0 || len(rt.deleteCredCalls) != 0 {
		t.Errorf("runtime mutated before auth: rotate=%d delete=%d", len(rt.rotateCalls), len(rt.deleteCredCalls))
	}
}

// decodeErrorType / decodeErrorMessage extract fields from the standard
// {"error":{"type":..., "message":...}} envelope. Inline since each
// test uses them once or twice.
func decodeErrorType(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return payload.Error.Type
}

func decodeErrorMessage(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return payload.Error.Message
}
