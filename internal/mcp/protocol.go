// Package mcp implements an MCP (Model Context Protocol) server that
// exposes the Hecate gateway's task, session, and observability surfaces
// to MCP clients (Claude Desktop, Cursor, Zed, ...).
//
// We hand-roll the protocol — there's no battle-tested Go SDK we trust
// yet, the wire format is small enough to keep readable, and we want
// the freedom to track spec revisions on our own cadence.
//
// Transport: stdio with newline-delimited JSON messages. Each line is a
// complete JSON-RPC 2.0 envelope. Frames are NOT length-prefixed (LSP
// uses Content-Length headers; MCP-stdio doesn't). HTTP/SSE transport
// is planned for v0.2 and will share the dispatcher in server.go but
// have its own framing.
//
// Spec target: protocol version "2024-11-05" — the first stable MCP
// release. Newer revisions are additive on the wire; we'll bump
// declaredProtocolVersion when we adopt a feature from a newer rev.
package mcp

import "encoding/json"

// declaredProtocolVersion is what the server reports during the
// initialize handshake. Clients negotiate down to a version they speak;
// if a client sends a different version, we still accept it and reply
// with our supported version.
const declaredProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 wire types.
//
// We define our own rather than pulling in net/rpc/jsonrpc because the
// stdlib variant is JSON-RPC 1.0 and doesn't speak the 2.0 envelope
// (no `jsonrpc: "2.0"` field, different error shape). A 100-line
// hand-roll is simpler than wrapping the stdlib.

// Request is a JSON-RPC 2.0 request OR notification. The distinction:
// requests carry an `id` and require a response; notifications omit
// `id` and the server stays silent. We use *RawMessage for ID so the
// caller's choice of string vs number ID round-trips byte-for-byte.
type Request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// IsNotification reports whether this Request is a notification (no ID,
// no response expected). Per JSON-RPC 2.0 §4.1.
func (r *Request) IsNotification() bool { return r.ID == nil }

// Response is a JSON-RPC 2.0 response. Either Result OR Error is set,
// never both. We marshal Result via RawMessage so handler code can
// build any shape and we don't double-encode.
type Response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

// RPCError is the error envelope. Code values follow JSON-RPC 2.0 plus
// MCP-specific extensions in the application range (-32000 and below).
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

// JSON-RPC 2.0 standard error codes.
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// MCP-specific error codes live in the JSON-RPC application range.
// We pick a small set rather than mirroring every HTTP status from the
// upstream — clients don't care about the gateway's internal code.
const (
	// ErrCodeUpstreamError covers any failure the MCP server hits
	// while talking to the Hecate HTTP API (network, 5xx, timeouts).
	// The Data payload carries the raw upstream error string for
	// debugging.
	ErrCodeUpstreamError = -32001
)

// NewError constructs an RPCError. Keeps call sites compact.
func NewError(code int, msg string) *RPCError {
	return &RPCError{Code: code, Message: msg}
}

// NewErrorWithData attaches an arbitrary data payload (anything that
// json.Marshal handles).
func NewErrorWithData(code int, msg string, data any) *RPCError {
	raw, err := json.Marshal(data)
	if err != nil {
		// Fall back to a code-only error if the payload can't marshal —
		// surfacing the original error is more useful than panicking.
		return &RPCError{Code: code, Message: msg}
	}
	return &RPCError{Code: code, Message: msg, Data: raw}
}

// ─── MCP-specific payload types ──────────────────────────────────────

// InitializeParams is the initialize request body. We accept arbitrary
// client capabilities — no need to validate them; an unknown capability
// just goes unused.
type InitializeParams struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities,omitempty"`
	ClientInfo      ClientInfo      `json:"clientInfo,omitempty"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the initialize response. We declare only the
// `tools` capability for v0.1 — resources, prompts, sampling come in
// later releases. The `logging` capability is not declared (we don't
// emit MCP-formatted log notifications yet); host stderr from the
// subprocess instead.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	// Empty object signals "this server supports tools/list and
	// tools/call but doesn't broadcast list-changed notifications".
	// MCP wire format treats `{}` and `null` differently here.
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	// ListChanged advertises that we'll emit `notifications/tools/list_changed`
	// when the tool set mutates. We don't (the set is fixed at startup),
	// so we leave this false.
	ListChanged bool `json:"listChanged,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool is the MCP tool descriptor returned by tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// CallToolParams is the body of a tools/call request.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// CallToolResult is the body of the tools/call response. MCP allows
// rich content blocks (text, image, resource); we emit text-only for
// v0.1 because every tool we ship returns string output.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	// IsError surfaces tool-level failures (the call dispatched but the
	// tool itself errored). Distinct from JSON-RPC errors which are
	// reserved for protocol failures.
	IsError bool `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// TextContent is the conventional shape of a tools/call result.
func TextContent(text string) []ContentBlock {
	return []ContentBlock{{Type: "text", Text: text}}
}

// ListToolsResult is the body of tools/list.
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}
