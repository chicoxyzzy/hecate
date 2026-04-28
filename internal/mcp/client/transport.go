// Package client implements the MCP-client side of the Model Context
// Protocol — the half of MCP that consumes external servers (rather
// than the existing parent-package server.go, which exposes Hecate
// itself as an MCP server).
//
// An agent_loop run with `mcp_servers` configured spawns one client
// per configured server, performs the initialize handshake, lists
// the server's tools, registers them alongside the built-in tools
// for the LLM to pick from, and dispatches tool calls back through
// the right client.
//
// Wire format and types are reused from the parent package
// (Request, Response, RPCError, InitializeParams, Tool, etc.) so
// this package stays focused on the consumer-side state machine.
//
// Transport is pluggable so the same Client works over stdio
// today and Streamable HTTP later — the protocol logic doesn't
// care about framing.
package client

import (
	"context"
	"errors"
)

// Transport is the wire-framing seam. Each Send / Recv carries
// exactly one JSON-RPC 2.0 envelope (a request, response, or
// notification). The transport owns framing — stdio is line-
// delimited JSON, HTTP/SSE will be event-delimited — but does NOT
// know about JSON-RPC semantics (request/response correlation,
// notification handling). That lives in Client.
//
// Recv blocks until a full message arrives or ctx is cancelled.
// Send writes synchronously; concurrent Send calls must be serialized
// by the caller (Client does this via its writeMu).
//
// Close shuts the transport down. After Close, Recv must return
// either io.EOF or ErrTransportClosed; any in-flight Send returns
// the same. Implementations should be safe to Close concurrently
// with a Recv that's already blocked.
type Transport interface {
	Send(ctx context.Context, frame []byte) error
	Recv(ctx context.Context) ([]byte, error)
	Close() error
}

// ErrTransportClosed is returned by Recv / Send after Close has run.
// Distinguished from io.EOF so callers can tell "peer hung up
// gracefully" (EOF) from "we shut the connection ourselves"
// (ErrTransportClosed). The Client tolerates both during shutdown.
var ErrTransportClosed = errors.New("mcp client: transport closed")
