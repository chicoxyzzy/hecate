package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/mcp"
	mcpclient "github.com/hecate/agent-runtime/internal/mcp/client"
)

// startInlineMCPHTTPFixture spins up an httptest.Server that speaks
// just enough MCP for SharedClientCache.Acquire to spawn a Client
// (initialize + tools/list) — it doesn't have to support tools/call
// because the tests here exercise cache stats, not tool dispatch.
//
// Lives in this file rather than reusing the mcp/client package's
// httptest helpers because importing those would require an
// internal-test cycle (api → mcp/client → cache_test fixtures). The
// shape is identical to mcp_host_cache_test.go's fakeMCPHTTPServer
// in the orchestrator package.
func startInlineMCPHTTPFixture(t *testing.T, name string) string {
	t.Helper()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcp.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch req.Method {
		case "initialize":
			result = mcp.InitializeResult{
				ProtocolVersion: mcp.DeclaredProtocolVersion,
				Capabilities:    mcp.ServerCapabilities{Tools: &mcp.ToolsCapability{}},
				ServerInfo:      mcp.ServerInfo{Name: name, Version: "0"},
			}
		case "tools/list":
			result = mcp.ListToolsResult{Tools: []mcp.Tool{
				{Name: "ping", InputSchema: json.RawMessage(`{}`)},
			}}
		case "notifications/initialized":
			return
		default:
			http.Error(w, "unsupported", http.StatusBadRequest)
			return
		}
		raw, _ := json.Marshal(result)
		_ = json.NewEncoder(w).Encode(mcp.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  raw,
		})
	}))
	t.Cleanup(hs.Close)
	return hs.URL
}

// TestMCPCacheStatsEndpoint_ConfiguredFalseWhenNoCache: a Handler
// built without SetMCPClientCache must report Configured=false rather
// than 4xx-ing or returning misleading zeros without context. The
// existing test fixture (newTestHTTPHandlerForProviders) doesn't wire
// the cache, so this is the most-common dev path; the endpoint has
// to render cleanly so admin UIs can show a "no cache" cell instead
// of error-handling a 4xx.
func TestMCPCacheStatsEndpoint_ConfiguredFalseWhenNoCache(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	client := newAPITestClient(t, handler)

	response := mustRequestJSON[MCPCacheStatsResponse](client, http.MethodGet, "/admin/mcp/cache", "")
	if response.Object != "mcp_cache_stats" {
		t.Fatalf("object = %q, want mcp_cache_stats", response.Object)
	}
	if response.Data.CheckedAt == "" {
		t.Fatal("checked_at = empty, want timestamp")
	}
	if response.Data.Configured {
		t.Errorf("configured = true, want false (test handler doesn't wire a cache)")
	}
	// All counters must be present (not omitted) so clients don't
	// have to special-case the no-cache shape.
	if response.Data.Entries != 0 || response.Data.InUse != 0 || response.Data.Idle != 0 {
		t.Errorf("counters = {%d,%d,%d}, want all zero on no-cache", response.Data.Entries, response.Data.InUse, response.Data.Idle)
	}
}

// TestMCPCacheStatsEndpoint_ReportsLiveStats: with a real cache wired
// (and a couple of acquires), the endpoint surfaces accurate
// entries / in-use / idle counts. Pinning the wiring path that
// production uses (Handler.SetMCPClientCache → Stats() → response
// item).
//
// Builds a minimal *Handler directly rather than going through
// NewHandler so the test stays focused on the cache-stats surface
// and doesn't drag in unrelated wiring (auth, rate limiter, runner).
func TestMCPCacheStatsEndpoint_ReportsLiveStats(t *testing.T) {
	t.Parallel()

	cache := mcpclient.NewSharedClientCache(time.Minute, mcp.ClientInfo{Name: "hecate-cache-stats-test", Version: "0"})
	t.Cleanup(func() { _ = cache.Close() })

	urlA := startInlineMCPHTTPFixture(t, "a")
	urlB := startInlineMCPHTTPFixture(t, "b")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// {Entries=2, InUse=1, Idle=1}: A held, B released.
	_, _, releaseA, err := cache.Acquire(ctx, mcpclient.ServerConfig{Name: "a", URL: urlA})
	if err != nil {
		t.Fatalf("Acquire A: %v", err)
	}
	t.Cleanup(releaseA)
	_, _, releaseB, err := cache.Acquire(ctx, mcpclient.ServerConfig{Name: "b", URL: urlB})
	if err != nil {
		t.Fatalf("Acquire B: %v", err)
	}
	releaseB()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := &Handler{
		logger:         logger,
		mcpClientCache: cache,
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/mcp/cache", nil)
	h.HandleMCPCacheStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var response MCPCacheStatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !response.Data.Configured {
		t.Errorf("configured = false, want true")
	}
	if response.Data.Entries != 2 {
		t.Errorf("entries = %d, want 2", response.Data.Entries)
	}
	if response.Data.InUse != 1 {
		t.Errorf("in_use = %d, want 1", response.Data.InUse)
	}
	if response.Data.Idle != 1 {
		t.Errorf("idle = %d, want 1", response.Data.Idle)
	}
	if response.Data.CheckedAt == "" {
		t.Errorf("checked_at = empty, want timestamp")
	}
}

// TestMCPCacheStatsEndpoint_RejectsNonAdmin pins the auth gate. The
// endpoint surfaces gateway-wide cache state which is operational
// metadata, not a tenant resource — requireAdmin is the right
// check, matching the rest of /admin/*. We trip it by invoking
// against a Handler whose authenticator isn't the permissive
// no-auth one (config.Config{Server: ServerConfig{AuthToken: "x"}}
// turns auth on).
func TestMCPCacheStatsEndpoint_RejectsNonAdmin(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	cfg.Server.AuthToken = "test-admin-token"

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	// Pass a nil providers slice (not a slice containing nil) so the
	// fixture's NewRegistry(...) stays happy — newTestHTTPHandlerWithConfig
	// would wrap a nil provider in a single-element slice, which
	// crashes the registry constructor.
	handler := newTestHTTPHandlerForProviders(logger, nil, cfg)
	client := newAPITestClient(t, handler)

	// No bearer token → unauthorized.
	rec := client.mustRequestStatus(http.StatusUnauthorized, http.MethodGet, "/admin/mcp/cache", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
