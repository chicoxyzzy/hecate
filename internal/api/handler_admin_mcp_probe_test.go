package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/mcp"
)

// startInlineMCPHTTPProbeFixture spins up an httptest.Server that
// answers initialize + tools/list with a controllable tool catalog.
// Mirrors startInlineMCPHTTPFixture in handler_admin_mcp_cache_test.go
// but lets the caller declare the tool list so probe tests can
// assert specific names / schemas come back.
func startInlineMCPHTTPProbeFixture(t *testing.T, name string, tools []mcp.Tool) string {
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
			result = mcp.ListToolsResult{Tools: tools}
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

// TestMCPProbeEndpoint_ReturnsToolCatalog: a clean probe against an
// httptest fixture returns the upstream's tools/list verbatim, with
// tool names un-namespaced (the dry-run surface should reflect what
// the server itself calls them, not the gateway's runtime alias).
func TestMCPProbeEndpoint_ReturnsToolCatalog(t *testing.T) {
	t.Parallel()

	url := startInlineMCPHTTPProbeFixture(t, "fs", []mcp.Tool{
		{Name: "read_file", Description: "Read a file under /workspace", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
		{Name: "list_directory", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	client := newAPITestClient(t, handler)

	body := map[string]any{"name": "fs", "url": url}
	raw, _ := json.Marshal(body)
	response := mustRequestJSON[MCPProbeResponse](client, http.MethodPost, "/v1/mcp/probe", string(raw))

	if response.Object != "mcp_probe" {
		t.Fatalf("object = %q, want mcp_probe", response.Object)
	}
	if got := len(response.Data.Tools); got != 2 {
		t.Fatalf("tool count = %d, want 2", got)
	}
	// Names should be un-namespaced — the upstream said "read_file",
	// not "mcp__fs__read_file".
	wantNames := map[string]bool{"read_file": false, "list_directory": false}
	for _, tool := range response.Data.Tools {
		if _, ok := wantNames[tool.Name]; !ok {
			t.Errorf("unexpected tool name %q", tool.Name)
			continue
		}
		wantNames[tool.Name] = true
	}
	for name, seen := range wantNames {
		if !seen {
			t.Errorf("missing tool %q", name)
		}
	}
	// Description and schema forwarded verbatim.
	for _, tool := range response.Data.Tools {
		if tool.Name == "read_file" && tool.Description == "" {
			t.Errorf("read_file description dropped")
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q schema is empty, want forwarded", tool.Name)
		}
	}
}

// TestMCPProbeEndpoint_DefaultsNameWhenMissing: operators dry-running
// a config typically don't care about the alias — the probe is for
// learning what tools come up. We default the name to "probe" rather
// than rejecting; the request still has to satisfy the
// command-XOR-url invariant.
func TestMCPProbeEndpoint_DefaultsNameWhenMissing(t *testing.T) {
	t.Parallel()

	url := startInlineMCPHTTPProbeFixture(t, "fs", []mcp.Tool{
		{Name: "ping", InputSchema: json.RawMessage(`{}`)},
	})

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	client := newAPITestClient(t, handler)

	// No "name" field — should default to "probe" and still succeed.
	body := map[string]any{"url": url}
	raw, _ := json.Marshal(body)
	response := mustRequestJSON[MCPProbeResponse](client, http.MethodPost, "/v1/mcp/probe", string(raw))
	if len(response.Data.Tools) != 1 {
		t.Errorf("tool count = %d, want 1", len(response.Data.Tools))
	}
}

// TestMCPProbeEndpoint_ValidationErrors covers the 400 paths: missing
// transport, both transports, malformed JSON. Each should surface a
// concrete diagnostic so the operator can correct without spelunking
// through gateway logs.
func TestMCPProbeEndpoint_ValidationErrors(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	client := newAPITestClient(t, handler)

	cases := []struct {
		desc    string
		body    string
		wantMsg string
	}{
		{
			desc:    "neither command nor url",
			body:    `{"name":"x"}`,
			wantMsg: "either command or url is required",
		},
		{
			desc:    "both command and url",
			body:    `{"name":"x","command":"npx","url":"https://example.com/mcp"}`,
			wantMsg: "mutually exclusive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			rec := client.mustRequestStatus(http.StatusBadRequest, http.MethodPost, "/v1/mcp/probe", tc.body)
			if !strings.Contains(rec.Body.String(), tc.wantMsg) {
				t.Errorf("body = %s, want substring %q", rec.Body.String(), tc.wantMsg)
			}
		})
	}
}

// TestMCPProbeEndpoint_UpstreamFailureSurfacesAs400: a probe against
// an unreachable URL should surface the transport error verbatim
// rather than swallowing it as a 500. Operators who typo a URL or
// forget to start their MCP server need to see the actual problem.
func TestMCPProbeEndpoint_UpstreamFailureSurfacesAs400(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})
	client := newAPITestClient(t, handler)

	// Port 1 is reserved/unused; connection refused.
	body := `{"name":"unreachable","url":"http://127.0.0.1:1/mcp"}`
	rec := client.mustRequestStatus(http.StatusBadRequest, http.MethodPost, "/v1/mcp/probe", body)
	if !strings.Contains(rec.Body.String(), "127.0.0.1:1") && !strings.Contains(rec.Body.String(), "connection refused") {
		t.Errorf("body = %s, want it to mention the unreachable target", rec.Body.String())
	}
}

// TestMCPProbeEndpoint_RejectsAnonymous: probe matches POST /v1/tasks
// auth (requireAny). With auth turned on (non-empty AuthToken) and no
// bearer / api-key header, the probe is rejected before any spawn
// attempt — protecting the gateway from anonymous "what tools does
// this command vend" reconnaissance.
func TestMCPProbeEndpoint_RejectsAnonymous(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	cfg.Server.AuthToken = "test-admin-token"

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, cfg)
	client := newAPITestClient(t, handler)

	body := `{"name":"x","command":"echo"}`
	rec := client.mustRequestStatus(http.StatusUnauthorized, http.MethodPost, "/v1/mcp/probe", body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestMCPProbeEndpoint_TimeoutDoesNotWedge: a server that accepts the
// connection but never answers tools/list must surface as an error
// within ~10s, not hang the request. We construct a server that
// stalls on tools/list and assert the request completes inside a
// safety bound that's longer than the handler's 10s deadline.
func TestMCPProbeEndpoint_TimeoutDoesNotWedge(t *testing.T) {
	t.Parallel()

	stall := make(chan struct{})
	t.Cleanup(func() { close(stall) })
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mcp.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			result := mcp.InitializeResult{
				ProtocolVersion: mcp.DeclaredProtocolVersion,
				Capabilities:    mcp.ServerCapabilities{Tools: &mcp.ToolsCapability{}},
				ServerInfo:      mcp.ServerInfo{Name: "stuck", Version: "0"},
			}
			raw, _ := json.Marshal(result)
			_ = json.NewEncoder(w).Encode(mcp.Response{JSONRPC: "2.0", ID: req.ID, Result: raw})
		case "tools/list":
			// Stall until the test ends or the request's context
			// times out — whichever fires first.
			select {
			case <-stall:
			case <-r.Context().Done():
			}
		case "notifications/initialized":
			return
		}
	}))
	t.Cleanup(hs.Close)

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := newTestHTTPHandlerForProviders(logger, nil, config.Config{})

	body := `{"name":"stuck","url":"` + hs.URL + `"}`

	// The handler caps at 10s. Use a 15s safety bound so the test
	// would catch a runaway / non-bounded probe — but the response
	// should come back inside the handler's own deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/probe", bytes.NewReader([]byte(body))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	// 400 (transport-level error from a deadline-exceeded probe).
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	// Must complete inside the handler's 10s deadline plus modest
	// slop (Go scheduling, test machine warm-up). 13s gives a
	// failing test before the test's own 15s outer bound trips.
	if elapsed > 13*time.Second {
		t.Errorf("probe took %v, want < 13s (handler bounds at 10s)", elapsed)
	}
}
