package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// rwPipe wires an io.Pipe with a closer hook so we can simulate stdin
// closing (which is how Serve unwinds in production when the parent
// process exits).
type rwPipe struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func newRWPipe() *rwPipe {
	r, w := io.Pipe()
	return &rwPipe{r: r, w: w}
}

func (p *rwPipe) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *rwPipe) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *rwPipe) Close() error                { return p.r.Close() }

// runServer starts a server in the background, feeds it the supplied
// JSON-RPC lines, returns the response lines (one per scan).
func runServer(t *testing.T, lines []string, register func(*Server)) []string {
	t.Helper()

	in := newRWPipe()
	var outBuf safeBuffer
	srv := NewServer("hecate-test", "0.0.0")
	if register != nil {
		register(srv)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, in, &outBuf)
	}()

	for _, line := range lines {
		_, _ = in.Write([]byte(line + "\n"))
	}
	// Close the writer end so the scanner sees EOF and Serve returns.
	_ = in.w.Close()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Serve did not return within 2s")
	}

	out := strings.TrimRight(outBuf.String(), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// safeBuffer is bytes.Buffer with a mutex — Server.writeResponse may
// write concurrently from goroutines spawned per-message.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestServer_Initialize(t *testing.T) {
	resp := runServer(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test","version":"1"}}}`,
	}, nil)
	if len(resp) != 1 {
		t.Fatalf("got %d responses, want 1", len(resp))
	}
	var r Response
	if err := json.Unmarshal([]byte(resp[0]), &r); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if r.Error != nil {
		t.Fatalf("initialize errored: %+v", r.Error)
	}
	var result InitializeResult
	if err := json.Unmarshal(r.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.ServerInfo.Name != "hecate-test" {
		t.Errorf("ServerInfo.Name = %q, want hecate-test", result.ServerInfo.Name)
	}
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("ProtocolVersion = %q, want 2024-11-05", result.ProtocolVersion)
	}
	if result.Capabilities.Tools == nil {
		t.Errorf("Capabilities.Tools must be non-nil to advertise tool support")
	}
}

func TestServer_NotificationGetsNoResponse(t *testing.T) {
	// notifications/initialized is a notification (no id) — server
	// must process it but stay silent on the wire.
	resp := runServer(t, []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}, nil)
	if len(resp) != 0 {
		t.Fatalf("notification got responses: %v", resp)
	}
}

func TestServer_UnknownMethodReturnsMethodNotFound(t *testing.T) {
	resp := runServer(t, []string{
		`{"jsonrpc":"2.0","id":7,"method":"does/not/exist"}`,
	}, nil)
	if len(resp) != 1 {
		t.Fatalf("got %d responses, want 1", len(resp))
	}
	var r Response
	_ = json.Unmarshal([]byte(resp[0]), &r)
	if r.Error == nil || r.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("got error %+v, want code %d", r.Error, ErrCodeMethodNotFound)
	}
}

func TestServer_ParseErrorRecovers(t *testing.T) {
	// A junk line must produce a parse-error response and the server
	// stays alive for the next message.
	resp := runServer(t, []string{
		`not json`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	}, nil)
	if len(resp) != 2 {
		t.Fatalf("got %d responses, want 2 (parse error + ping)", len(resp))
	}
	var first, second Response
	_ = json.Unmarshal([]byte(resp[0]), &first)
	_ = json.Unmarshal([]byte(resp[1]), &second)
	// Order isn't guaranteed (each line dispatched in its own
	// goroutine) — just look for one parse error and one ping result.
	hasParseError := (first.Error != nil && first.Error.Code == ErrCodeParseError) ||
		(second.Error != nil && second.Error.Code == ErrCodeParseError)
	hasPingResult := first.Error == nil || second.Error == nil
	if !hasParseError || !hasPingResult {
		t.Fatalf("expected one parse error + one ping success, got %s and %s", resp[0], resp[1])
	}
}

func TestServer_ListTools(t *testing.T) {
	register := func(s *Server) {
		s.RegisterTool(Tool{
			Name:        "echo",
			Description: "echo back input",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		}, func(ctx context.Context, args json.RawMessage) (CallToolResult, error) {
			return CallToolResult{Content: TextContent(string(args))}, nil
		})
	}
	resp := runServer(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	}, register)
	if len(resp) != 1 {
		t.Fatalf("got %d responses, want 1", len(resp))
	}
	var r Response
	_ = json.Unmarshal([]byte(resp[0]), &r)
	var result ListToolsResult
	_ = json.Unmarshal(r.Result, &result)
	if len(result.Tools) != 1 || result.Tools[0].Name != "echo" {
		t.Fatalf("tools = %+v, want one echo", result.Tools)
	}
}

func TestServer_CallTool_Success(t *testing.T) {
	register := func(s *Server) {
		s.RegisterTool(Tool{Name: "ping-tool", InputSchema: json.RawMessage(`{}`)},
			func(ctx context.Context, args json.RawMessage) (CallToolResult, error) {
				return CallToolResult{Content: TextContent("pong")}, nil
			})
	}
	resp := runServer(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping-tool","arguments":{}}}`,
	}, register)
	if len(resp) != 1 {
		t.Fatalf("got %d responses, want 1", len(resp))
	}
	var r Response
	_ = json.Unmarshal([]byte(resp[0]), &r)
	var result CallToolResult
	_ = json.Unmarshal(r.Result, &result)
	if len(result.Content) != 1 || result.Content[0].Text != "pong" {
		t.Fatalf("Content = %+v, want one text block 'pong'", result.Content)
	}
	if result.IsError {
		t.Fatalf("IsError = true on success path")
	}
}

func TestServer_CallTool_HandlerErrorIsToolLevel(t *testing.T) {
	// Handler errors must NOT bubble up as JSON-RPC errors — they're
	// returned as CallToolResult with isError=true. This is the MCP
	// contract: protocol errors and tool errors are different things.
	register := func(s *Server) {
		s.RegisterTool(Tool{Name: "boom", InputSchema: json.RawMessage(`{}`)},
			func(ctx context.Context, args json.RawMessage) (CallToolResult, error) {
				return CallToolResult{}, errors.New("upstream is down")
			})
	}
	resp := runServer(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom","arguments":{}}}`,
	}, register)
	var r Response
	_ = json.Unmarshal([]byte(resp[0]), &r)
	if r.Error != nil {
		t.Fatalf("handler error became JSON-RPC error: %+v", r.Error)
	}
	var result CallToolResult
	_ = json.Unmarshal(r.Result, &result)
	if !result.IsError {
		t.Fatalf("IsError should be true; got %+v", result)
	}
	if result.Content[0].Text != "upstream is down" {
		t.Fatalf("Content = %+v, want error message", result.Content)
	}
}

func TestServer_CallTool_UnknownToolIsInvalidParams(t *testing.T) {
	resp := runServer(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"missing","arguments":{}}}`,
	}, nil)
	var r Response
	_ = json.Unmarshal([]byte(resp[0]), &r)
	if r.Error == nil || r.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("got %+v, want code %d", r.Error, ErrCodeInvalidParams)
	}
}
