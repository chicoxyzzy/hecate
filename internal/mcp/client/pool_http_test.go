package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/mcp"
)

// mcpHTTPServer is a minimal in-process MCP server that responds to
// JSON-RPC requests over HTTP (application/json responses, not SSE).
// It routes by method name; unknown methods return -32601.
type mcpHTTPServer struct {
	handlers map[string]func(req mcp.Request) (any, *mcp.RPCError)
}

func newMCPHTTPServer() *mcpHTTPServer {
	return &mcpHTTPServer{handlers: make(map[string]func(mcp.Request) (any, *mcp.RPCError))}
}

func (s *mcpHTTPServer) handle(method string, fn func(mcp.Request) (any, *mcp.RPCError)) {
	s.handlers[method] = fn
}

func (s *mcpHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req mcp.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	fn, ok := s.handlers[req.Method]
	if !ok {
		resp := mcp.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   mcp.NewError(mcp.ErrCodeMethodNotFound, "method not found: "+req.Method),
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	result, rpcErr := fn(req)
	var resp mcp.Response
	resp.JSONRPC = "2.0"
	resp.ID = req.ID
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		raw, _ := json.Marshal(result)
		resp.Result = raw
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func newTestMCPHTTPServer(t *testing.T) (*httptest.Server, *mcpHTTPServer) {
	t.Helper()
	srv := newMCPHTTPServer()
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	return hs, srv
}

// registerStandardHandlers sets up initialize + tools/list + tools/call
// on srv using the provided tool list and call handler table.
func registerStandardHandlers(
	srv *mcpHTTPServer,
	serverName string,
	tools []mcp.Tool,
	callHandlers map[string]func(json.RawMessage) mcp.CallToolResult,
) {
	srv.handle("initialize", func(req mcp.Request) (any, *mcp.RPCError) {
		return mcp.InitializeResult{
			ProtocolVersion: declaredClientProtocolVersion,
			Capabilities:    mcp.ServerCapabilities{Tools: &mcp.ToolsCapability{}},
			ServerInfo:      mcp.ServerInfo{Name: serverName, Version: "0.0.0"},
		}, nil
	})
	srv.handle("notifications/initialized", func(req mcp.Request) (any, *mcp.RPCError) {
		return nil, nil
	})
	srv.handle("tools/list", func(req mcp.Request) (any, *mcp.RPCError) {
		return mcp.ListToolsResult{Tools: tools}, nil
	})
	srv.handle("tools/call", func(req mcp.Request) (any, *mcp.RPCError) {
		var params mcp.CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, mcp.NewError(mcp.ErrCodeInvalidParams, err.Error())
		}
		fn, ok := callHandlers[params.Name]
		if !ok {
			return nil, mcp.NewError(mcp.ErrCodeInvalidParams, "unknown tool: "+params.Name)
		}
		return fn(params.Arguments), nil
	})
}

// TestPool_HTTPTransportRoundTrip exercises the full NewPool path with an
// HTTP transport: initialize handshake, tool listing, and a tool call all
// happen over a real httptest.Server rather than in-process stdio pipes.
func TestPool_HTTPTransportRoundTrip(t *testing.T) {
	t.Parallel()

	hs, srv := newTestMCPHTTPServer(t)
	registerStandardHandlers(srv, "remote", []mcp.Tool{
		{Name: "echo", Description: "echo the input", InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`)},
	}, map[string]func(json.RawMessage) mcp.CallToolResult{
		"echo": func(args json.RawMessage) mcp.CallToolResult {
			return mcp.CallToolResult{Content: mcp.TextContent("echo: " + string(args))}
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, mcp.ClientInfo{Name: "test", Version: "0"}, []ServerConfig{
		{Name: "remote", URL: hs.URL},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	// Tool catalog should contain exactly the one namespaced tool.
	tools := pool.Tools()
	if len(tools) != 1 {
		t.Fatalf("Tools() len = %d, want 1", len(tools))
	}
	if tools[0].Name != "mcp__remote__echo" {
		t.Errorf("tool name = %q, want mcp__remote__echo", tools[0].Name)
	}

	// Call the tool and verify the round-trip.
	text, isErr, err := pool.Call(ctx, "mcp__remote__echo", json.RawMessage(`{"msg":"hello"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if isErr {
		t.Errorf("Call returned isError=true")
	}
	if !strings.Contains(text, "echo:") {
		t.Errorf("text = %q, want contains 'echo:'", text)
	}
}

// TestPool_HTTPTransportAuthHeaderForwarded verifies that the Headers
// field on ServerConfig is forwarded on every MCP request — critical for
// bearer-token auth against cloud MCP servers.
func TestPool_HTTPTransportAuthHeaderForwarded(t *testing.T) {
	t.Parallel()

	const wantToken = "Bearer pool-secret-token"
	gotTokens := make(chan string, 8) // buffer so the handler never blocks

	hs, srv := newTestMCPHTTPServer(t)

	// Intercept every request to capture the Authorization header.
	origServeHTTP := srv.ServeHTTP
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTokens <- r.Header.Get("Authorization")
		origServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	registerStandardHandlers(srv, "secure", []mcp.Tool{
		{Name: "ping", InputSchema: json.RawMessage(`{}`)},
	}, map[string]func(json.RawMessage) mcp.CallToolResult{
		"ping": func(json.RawMessage) mcp.CallToolResult {
			return mcp.CallToolResult{Content: mcp.TextContent("pong")}
		},
	})
	_ = hs // shut up the "unused" linter — we registered handlers on srv

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, mcp.ClientInfo{Name: "test", Version: "0"}, []ServerConfig{
		{Name: "secure", URL: ts.URL, Headers: map[string]string{"Authorization": wantToken}},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })

	// Drain the tokens collected during initialize + tools/list.
	// We don't care about those — only assert the tool call carries the header.
	// Drain up to 4 requests (initialize, notifications/initialized, tools/list, etc.)
	for i := 0; i < 3; i++ {
		select {
		case tok := <-gotTokens:
			if tok != wantToken {
				t.Errorf("request %d Authorization = %q, want %q", i+1, tok, wantToken)
			}
		default:
		}
	}

	_, _, err = pool.Call(ctx, "mcp__secure__ping", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	select {
	case tok := <-gotTokens:
		if tok != wantToken {
			t.Errorf("tool call Authorization = %q, want %q", tok, wantToken)
		}
	case <-time.After(time.Second):
		t.Error("no Authorization header captured for tool call")
	}
}

// TestPool_HTTPTransportMutualExclusivity verifies that NewPool rejects
// a config that sets both Command and URL.
func TestPool_HTTPTransportMutualExclusivity(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := NewPool(ctx, mcp.ClientInfo{}, []ServerConfig{
		{Name: "bad", Command: "npx", URL: "https://example.com/mcp"},
	})
	if err == nil {
		t.Fatal("expected error for command+url, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v, want 'mutually exclusive'", err)
	}
}

// TestPool_HTTPTransportNeitherCommandNorURL verifies that NewPool
// rejects a config where neither Command nor URL is set.
func TestPool_HTTPTransportNeitherCommandNorURL(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := NewPool(ctx, mcp.ClientInfo{}, []ServerConfig{
		{Name: "empty"},
	})
	if err == nil {
		t.Fatal("expected error for no command and no url, got nil")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("err = %v, want 'required'", err)
	}
}
