package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Server is the MCP server core. Wire it with RegisterTool, then call
// Serve to drive a stdio (or any io.Reader/io.Writer) loop. Concurrent
// requests share the dispatcher; each request handler runs in its own
// goroutine so a slow tool doesn't head-of-line block the next one.
//
// The server is designed to live as long as the stdio pair — there's
// no graceful-restart story. When stdin closes, Serve returns; when
// the parent process kills us, we exit. That matches how Claude
// Desktop / Cursor / Zed manage MCP subprocesses today.
type Server struct {
	info  ServerInfo
	tools toolRegistry

	// Mutex guards writes to the output stream — multiple goroutines
	// produce responses concurrently and JSON-RPC framing requires an
	// uninterrupted message per write.
	writeMu sync.Mutex
}

// NewServer constructs an MCP server with the given identity. The name
// is what shows up in MCP client UIs (Claude Desktop's connector list,
// Cursor's @-mention, etc.); pick something operators recognize.
func NewServer(name, version string) *Server {
	return &Server{
		info: ServerInfo{Name: name, Version: version},
		tools: toolRegistry{
			byName: make(map[string]registeredTool),
		},
	}
}

// RegisterTool wires a tool into the server. Must be called before
// Serve; the registry is not safe for concurrent mutation while a
// dispatcher is active.
//
// schema must be a JSON Schema document (json.RawMessage) describing
// the tool's `arguments` shape — clients use it for autocomplete /
// validation. Pass json.RawMessage("{}") for "any object".
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.tools.byName[tool.Name] = registeredTool{
		descriptor: tool,
		handler:    handler,
	}
}

// Serve drives the JSON-RPC loop. Reads newline-delimited messages
// from in, writes responses to out. Returns when in is closed (EOF) or
// when ctx is cancelled — and only AFTER all in-flight handlers have
// produced their responses (we wait on a WaitGroup so a fast-closing
// stdin doesn't drop the last response).
//
// Reader buffer is bumped because tool arguments can carry sizable
// JSON. 1 MiB is enough headroom for any practical use case while
// still bounding memory if a client misbehaves.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	// Outer-context-cancellation wins over scanner.Scan blocking on
	// stdin: when ctx fires we ask the OS to close stdin, which makes
	// Scan return false and the loop unwinds.
	go func() {
		<-ctx.Done()
		if closer, ok := in.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	var wg sync.WaitGroup
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Copy because scanner.Bytes is reused on the next call.
		msg := make([]byte, len(line))
		copy(msg, line)

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handleMessage(ctx, msg, out)
		}()
	}
	// Wait for in-flight handlers before returning. Without this,
	// closing stdin races with the last response write — the parent
	// process can lose the final reply.
	wg.Wait()

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("mcp: scanner: %w", err)
	}
	return nil
}

// handleMessage parses one JSON-RPC envelope and dispatches it. Errors
// at the parse layer become JSON-RPC error responses (or are silently
// dropped for notifications, per spec).
func (s *Server) handleMessage(ctx context.Context, raw []byte, out io.Writer) {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		// Parse error — we don't know the ID so send a best-effort
		// error response with a null ID, per JSON-RPC §5.1.
		s.writeResponse(out, errorResponse(nil, NewError(ErrCodeParseError, "parse error: "+err.Error())))
		return
	}
	if req.JSONRPC != "2.0" {
		s.writeResponse(out, errorResponse(req.ID, NewError(ErrCodeInvalidRequest, "jsonrpc must be \"2.0\"")))
		return
	}

	result, rpcErr := s.dispatch(ctx, &req)

	// Notifications must not get a response, even on error.
	if req.IsNotification() {
		return
	}

	if rpcErr != nil {
		s.writeResponse(out, errorResponse(req.ID, rpcErr))
		return
	}
	s.writeResponse(out, successResponse(req.ID, result))
}

// dispatch routes the request to the right method handler.
//
// Methods we implement:
//   - initialize             → handshake; returns server capabilities
//   - notifications/initialized → ack from client; we don't need to
//     react but spec requires we accept it
//   - tools/list             → enumerate registered tools
//   - tools/call             → invoke a tool by name
//   - ping                   → liveness check; returns empty result
//
// Unknown methods get a -32601 (method not found) response so MCP
// clients that probe optional capabilities don't see hard failures.
func (s *Server) dispatch(ctx context.Context, req *Request) (any, *RPCError) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		// No-op ack.
		return nil, nil
	case "tools/list":
		return s.handleListTools()
	case "tools/call":
		return s.handleCallTool(ctx, req)
	case "ping":
		return struct{}{}, nil
	default:
		return nil, NewError(ErrCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s *Server) handleInitialize(req *Request) (any, *RPCError) {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, NewError(ErrCodeInvalidParams, "invalid initialize params: "+err.Error())
		}
	}
	// We accept whatever protocol version the client requested but
	// reply with our own — clients are expected to negotiate down if
	// they support multiple versions.
	return InitializeResult{
		ProtocolVersion: declaredProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{},
		},
		ServerInfo: s.info,
	}, nil
}

func (s *Server) handleListTools() (any, *RPCError) {
	return ListToolsResult{Tools: s.tools.list()}, nil
}

func (s *Server) handleCallTool(ctx context.Context, req *Request) (any, *RPCError) {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, NewError(ErrCodeInvalidParams, "invalid tools/call params: "+err.Error())
	}
	tool, ok := s.tools.byName[params.Name]
	if !ok {
		return nil, NewError(ErrCodeInvalidParams, fmt.Sprintf("unknown tool: %s", params.Name))
	}
	result, err := tool.handler(ctx, params.Arguments)
	if err != nil {
		// Tool-level error → return CallToolResult with isError=true,
		// not a JSON-RPC error. Per MCP spec, tool failures aren't
		// protocol failures; the client renders them as content.
		return CallToolResult{
			Content: TextContent(err.Error()),
			IsError: true,
		}, nil
	}
	return result, nil
}

// ─── Output ──────────────────────────────────────────────────────────

func (s *Server) writeResponse(out io.Writer, resp Response) {
	body, err := json.Marshal(resp)
	if err != nil {
		// Should never happen — every field is JSON-marshalable by
		// construction. Log to stderr (the dispatcher's logger isn't
		// available here) and drop.
		fmt.Fprintf(out, `{"jsonrpc":"2.0","id":null,"error":{"code":%d,"message":%q}}`+"\n",
			ErrCodeInternalError, "internal: marshal response: "+err.Error())
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = out.Write(body)
	_, _ = out.Write([]byte("\n"))
}

func successResponse(id *json.RawMessage, result any) Response {
	raw, err := json.Marshal(result)
	if err != nil {
		return errorResponse(id, NewError(ErrCodeInternalError, "marshal result: "+err.Error()))
	}
	return Response{JSONRPC: "2.0", ID: id, Result: raw}
}

func errorResponse(id *json.RawMessage, e *RPCError) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: e}
}

// ─── Tool registry ───────────────────────────────────────────────────

// ToolHandler is the function signature for a tool implementation.
// Args is the raw JSON-encoded arguments object — handlers unmarshal
// into their own typed shape. Returning a non-nil error becomes a
// tool-level failure (CallToolResult with isError=true); returning a
// CallToolResult lets the handler set the content blocks directly.
type ToolHandler func(ctx context.Context, args json.RawMessage) (CallToolResult, error)

type registeredTool struct {
	descriptor Tool
	handler    ToolHandler
}

type toolRegistry struct {
	byName map[string]registeredTool
}

// list returns descriptors in registration order. We use a separate
// `order` slice rather than relying on map iteration so the wire
// output is stable across runs — clients cache lists and a churning
// order would invalidate caches needlessly.
func (r toolRegistry) list() []Tool {
	out := make([]Tool, 0, len(r.byName))
	for _, t := range r.byName {
		out = append(out, t.descriptor)
	}
	// Sort by name for deterministic ordering. Map iteration is random
	// in Go; without a sort the same client would see a different list
	// order on each connect, which makes change-detection lossy.
	sortToolsByName(out)
	return out
}

func sortToolsByName(tools []Tool) {
	for i := 1; i < len(tools); i++ {
		for j := i; j > 0 && tools[j-1].Name > tools[j].Name; j-- {
			tools[j-1], tools[j] = tools[j], tools[j-1]
		}
	}
}
