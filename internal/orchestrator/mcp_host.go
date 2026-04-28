package orchestrator

import (
	"context"
	"encoding/json"

	"github.com/hecate/agent-runtime/internal/mcp"
	mcpclient "github.com/hecate/agent-runtime/internal/mcp/client"
	"github.com/hecate/agent-runtime/internal/version"
	"github.com/hecate/agent-runtime/pkg/types"
)

// AgentMCPHost is the seam the agent loop uses to talk to a bundle of
// external MCP servers. The production implementation is a
// mcpclient.Pool wrapper; tests substitute an in-memory fake.
//
// Lifetime is one-per-run: built before the loop's first turn, closed
// before Execute returns. Long-lived per-task pooling is a follow-up
// — for now we eat the spawn cost on each run because runs are short
// and the simplicity of "subprocess dies with the run" is worth more
// than the few-hundred-ms savings.
type AgentMCPHost interface {
	// Tools returns the merged tool catalog (already in the LLM's
	// expected shape, names already namespaced). Stable order.
	Tools() []types.Tool
	// Call dispatches a tool by its namespaced name. Returns:
	//   - text:    the upstream content, flattened to a single string
	//   - isError: upstream signaled CallToolResult.IsError
	//   - err:     protocol-level failure (transport, RPC error)
	Call(ctx context.Context, name string, args json.RawMessage) (text string, isError bool, err error)
	// Close shuts every underlying client down. Idempotent.
	Close() error
}

// AgentMCPHostFactory builds a host from a slice of per-task server
// configs. Returns nil when configs is empty (the agent loop skips
// MCP plumbing entirely in that case). On error the caller treats
// the run as failed — there's no partial-host fallback.
type AgentMCPHostFactory func(ctx context.Context, configs []types.MCPServerConfig) (AgentMCPHost, error)

// DefaultMCPHostFactory is the production factory: spawns one stdio
// subprocess per config via mcpclient.NewPool. Wire this into
// AgentLoopExecutor at runner construction time.
func DefaultMCPHostFactory(ctx context.Context, configs []types.MCPServerConfig) (AgentMCPHost, error) {
	if len(configs) == 0 {
		return nil, nil
	}
	pool, err := mcpclient.NewPool(ctx, agentClientInfo(), toClientServerConfigs(configs))
	if err != nil {
		return nil, err
	}
	return &poolMCPHost{pool: pool}, nil
}

// agentClientInfo is what every spawned MCP server sees as the
// connecting client identity. Stable name so operators reading
// upstream server logs can correlate.
func agentClientInfo() mcp.ClientInfo {
	return mcp.ClientInfo{Name: "hecate-agent-loop", Version: version.Version}
}

// toClientServerConfigs converts the orchestrator-side config slice
// into the client package's shape. Duplicated representation by
// design: the client package owns its own types so it stays free of
// the orchestrator's tree.
func toClientServerConfigs(configs []types.MCPServerConfig) []mcpclient.ServerConfig {
	out := make([]mcpclient.ServerConfig, 0, len(configs))
	for _, c := range configs {
		out = append(out, mcpclient.ServerConfig{
			Name:    c.Name,
			Command: c.Command,
			Args:    c.Args,
			Env:     c.Env,
		})
	}
	return out
}

// poolMCPHost adapts mcpclient.Pool to AgentMCPHost. The conversion
// from NamespacedTool to types.Tool happens here so the agent_loop
// gets a uniform tool catalog (built-ins + MCP) it can hand the LLM
// without any further marshaling.
type poolMCPHost struct {
	pool *mcpclient.Pool
}

func (h *poolMCPHost) Tools() []types.Tool {
	src := h.pool.Tools()
	out := make([]types.Tool, 0, len(src))
	for _, t := range src {
		// Schemas come straight from upstream MCP servers as JSON
		// Schema documents — the LLM's tool-call format expects the
		// same shape, so we forward verbatim. An empty schema becomes
		// a permissive `{"type":"object"}` so the LLM still sees a
		// well-formed tool descriptor.
		schema := t.Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		out = append(out, types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schema,
			},
		})
	}
	return out
}

func (h *poolMCPHost) Call(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	return h.pool.Call(ctx, name, args)
}

func (h *poolMCPHost) Close() error {
	return h.pool.Close()
}
