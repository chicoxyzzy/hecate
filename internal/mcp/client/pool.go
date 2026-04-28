package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/hecate/agent-runtime/internal/mcp"
)

// ServerConfig is one external MCP server the pool should bring up.
// Exactly one of Command (stdio) or URL (HTTP) must be set.
//
// Stdio: the pool spawns Command (with Args and merged Env) as a
// subprocess and communicates via stdin/stdout.
//
// HTTP: the pool connects to URL using the Streamable HTTP transport.
// Headers are sent on every request (e.g. {"Authorization": "Bearer
// <token>"}). Env is ignored for HTTP servers.
//
// Duplicated from pkg/types.MCPServerConfig so the client package
// stays free of the orchestrator's type tree. The orchestrator
// converts on the way in.
type ServerConfig struct {
	Name string
	// Stdio transport — mutually exclusive with URL.
	Command string
	Args    []string
	Env     map[string]string
	// HTTP transport — mutually exclusive with Command.
	URL     string
	Headers map[string]string
}

// PoolToolName is the namespace prefix every pool-vended tool name
// carries. Built into a separator constant so dispatch can split
// `mcp__filesystem__read_file` back into ("filesystem", "read_file")
// without ambiguity.
const (
	PoolToolNamespacePrefix = "mcp"
	PoolToolNamespaceSep    = "__"
)

// NamespacedTool is one tool surfaced by the pool. Name is already
// `mcp__<server>__<tool>`; Description and Schema come straight from
// the upstream server (no rewriting).
type NamespacedTool struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Pool runs N MCP-client connections, one per ServerConfig, and
// multiplexes tool dispatch by name. Lifecycle:
//
//   - NewPool spawns every server, runs initialize, lists tools.
//     Partial failure aborts: any clients already up are closed
//     before the error returns. The caller never sees a half-built
//     pool.
//   - Tools() returns the merged catalog (deterministic order: sorted
//     by namespaced name).
//   - Call routes by namespaced name to the right client; tool-level
//     errors come back as (text, isError=true, nil), protocol errors
//     as a non-nil error.
//   - Close shuts every client down. Idempotent.
type Pool struct {
	mu      sync.Mutex
	clients map[string]*Client               // server name → client
	tools   []NamespacedTool                 // sorted, stable
	bind    map[string]namespacedToolBinding // namespaced name → routing info
}

type namespacedToolBinding struct {
	serverName string
	toolName   string
}

// NewPool spawns one stdio client per config, initializes the
// handshake, and lists tools. Returns a fully ready pool or an error
// (with all in-progress clients already torn down).
//
// info propagates to every spawned Client as their MCP ClientInfo —
// servers log who connected, so a sensible "hecate-agent-loop /
// <version>" identity helps operators when they read upstream logs.
func NewPool(ctx context.Context, info mcp.ClientInfo, configs []ServerConfig) (*Pool, error) {
	p := &Pool{
		clients: make(map[string]*Client, len(configs)),
		bind:    make(map[string]namespacedToolBinding),
	}
	// Cleanup helper: on any failure, tear down whatever's already
	// up. Avoids leaking subprocess handles on a bad config.
	cleanup := func() {
		_ = p.Close()
	}

	for _, cfg := range configs {
		name := strings.TrimSpace(cfg.Name)
		if name == "" {
			cleanup()
			return nil, errors.New("mcp pool: server name is required")
		}
		if _, dup := p.clients[name]; dup {
			cleanup()
			return nil, fmt.Errorf("mcp pool: duplicate server name %q", name)
		}
		command := strings.TrimSpace(cfg.Command)
		rawURL := strings.TrimSpace(cfg.URL)
		if command != "" && rawURL != "" {
			cleanup()
			return nil, fmt.Errorf("mcp pool: server %q: command and url are mutually exclusive", name)
		}
		if command == "" && rawURL == "" {
			cleanup()
			return nil, fmt.Errorf("mcp pool: server %q: either command or url is required", name)
		}

		var transport Transport
		var transportErr error
		if rawURL != "" {
			transport, transportErr = NewHTTPTransport(rawURL, cfg.Headers, nil)
			if transportErr != nil {
				cleanup()
				return nil, fmt.Errorf("mcp pool: server %q: http: %w", name, transportErr)
			}
		} else {
			cmd := exec.CommandContext(ctx, command, cfg.Args...)
			cmd.Env = mergeEnv(os.Environ(), cfg.Env)
			transport, transportErr = NewStdioTransport(cmd)
			if transportErr != nil {
				cleanup()
				return nil, fmt.Errorf("mcp pool: server %q: spawn: %w", name, transportErr)
			}
		}
		client := New(transport, info)
		if _, err := client.Initialize(ctx); err != nil {
			// Surface stderr from stdio servers — the JSON-RPC error
			// alone (often "EOF") rarely names the root cause (missing
			// deps, bad arg, auth failure). HTTP transports have no
			// stderr; the HTTP status error already carries the detail.
			var diag string
			if st, ok := transport.(*StdioTransport); ok {
				diag = st.Stderr()
			}
			_ = client.Close()
			cleanup()
			if strings.TrimSpace(diag) != "" {
				return nil, fmt.Errorf("mcp pool: server %q: initialize: %w; stderr: %s", name, err, strings.TrimSpace(diag))
			}
			return nil, fmt.Errorf("mcp pool: server %q: initialize: %w", name, err)
		}
		serverTools, err := client.ListTools(ctx)
		if err != nil {
			_ = client.Close()
			cleanup()
			return nil, fmt.Errorf("mcp pool: server %q: list tools: %w", name, err)
		}
		p.clients[name] = client
		for _, t := range serverTools {
			ns := NamespacedToolName(name, t.Name)
			if _, dup := p.bind[ns]; dup {
				// Same upstream server vending the same tool name twice
				// is the only realistic way to hit this; treat as a
				// server bug and abort rather than silently shadow.
				_ = client.Close()
				cleanup()
				return nil, fmt.Errorf("mcp pool: server %q vended duplicate tool %q", name, t.Name)
			}
			p.bind[ns] = namespacedToolBinding{serverName: name, toolName: t.Name}
			p.tools = append(p.tools, NamespacedTool{
				Name:        ns,
				Description: t.Description,
				Schema:      t.InputSchema,
			})
		}
	}
	sort.Slice(p.tools, func(i, j int) bool { return p.tools[i].Name < p.tools[j].Name })
	return p, nil
}

// Tools returns the merged tool catalog. Stable order across calls
// (sorted by namespaced name) so the LLM sees a deterministic list.
// The slice is a copy — callers may not mutate it.
func (p *Pool) Tools() []NamespacedTool {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]NamespacedTool, len(p.tools))
	copy(out, p.tools)
	return out
}

// Call dispatches a namespaced tool call. Returns:
//   - text: the concatenated text content from the upstream
//     CallToolResult (one block per line). MCP allows non-text
//     content blocks; this layer flattens them to text since
//     agent_loop's tool-result message is text-only.
//   - isError: true when the upstream marked CallToolResult.IsError.
//   - err: non-nil for protocol-level failures (unknown tool, RPC
//     error, transport closed). Tool-level failures come back via
//     isError=true with err=nil.
func (p *Pool) Call(ctx context.Context, name string, args json.RawMessage) (text string, isError bool, err error) {
	p.mu.Lock()
	bind, ok := p.bind[name]
	client := p.clients[bind.serverName]
	p.mu.Unlock()
	if !ok {
		return "", false, fmt.Errorf("mcp pool: unknown tool %q", name)
	}
	if client == nil {
		// Defensive — shouldn't happen since bind and clients are
		// populated together. Surface as an error rather than
		// panicking.
		return "", false, fmt.Errorf("mcp pool: server for tool %q is not connected", name)
	}
	res, err := client.CallTool(ctx, bind.toolName, args)
	if err != nil {
		return "", false, err
	}
	return flattenContent(res.Content), res.IsError, nil
}

// Close tears every client down. Errors from individual clients are
// joined into a single error so the operator sees them all without
// losing the first failure to log truncation.
func (p *Pool) Close() error {
	p.mu.Lock()
	clients := p.clients
	p.clients = make(map[string]*Client)
	p.bind = make(map[string]namespacedToolBinding)
	p.tools = nil
	p.mu.Unlock()

	var errs []error
	for name, c := range clients {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close %q: %w", name, err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// NamespacedToolName builds the wire name for a server's tool. Public
// so callers can predict the name without consulting the pool — agent
// loop uses this when emitting telemetry that references a tool by
// its un-namespaced upstream name.
func NamespacedToolName(serverName, toolName string) string {
	return PoolToolNamespacePrefix + PoolToolNamespaceSep + serverName + PoolToolNamespaceSep + toolName
}

// SplitNamespacedToolName is the inverse: splits `mcp__<server>__<tool>`
// back into (server, tool, true). Returns ("", "", false) on anything
// that doesn't match the prefix or has too few segments. The tool name
// itself may contain double-underscores (some upstream servers use
// them); we honor the FIRST split after the server segment, treating
// the rest as the tool name.
func SplitNamespacedToolName(ns string) (serverName, toolName string, ok bool) {
	prefix := PoolToolNamespacePrefix + PoolToolNamespaceSep
	if !strings.HasPrefix(ns, prefix) {
		return "", "", false
	}
	rest := ns[len(prefix):]
	idx := strings.Index(rest, PoolToolNamespaceSep)
	if idx < 0 {
		return "", "", false
	}
	server := rest[:idx]
	tool := rest[idx+len(PoolToolNamespaceSep):]
	if server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

// flattenContent collapses a CallToolResult.Content slice into a
// single text string. We join blocks with newlines; non-text blocks
// (image, resource) are rendered as a placeholder so the LLM at
// least sees that something was returned. agent_loop will surface
// images / resources directly once we ship multi-modal tool results.
func flattenContent(blocks []mcp.ContentBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	var b strings.Builder
	for i, blk := range blocks {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch blk.Type {
		case "text", "":
			b.WriteString(blk.Text)
		default:
			fmt.Fprintf(&b, "[%s content omitted]", blk.Type)
		}
	}
	return b.String()
}

// mergeEnv layers the per-server Env map onto the parent process's
// environment. We inherit the parent env (so PATH, HOME, etc. work
// without per-task config) and then apply the overrides last — explicit
// wins. Returns a new slice; doesn't mutate the input.
func mergeEnv(parent []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return parent
	}
	// Index parent by key for O(1) override.
	idx := make(map[string]int, len(parent))
	out := make([]string, len(parent), len(parent)+len(overrides))
	copy(out, parent)
	for i, kv := range out {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		idx[kv[:eq]] = i
	}
	for k, v := range overrides {
		entry := k + "=" + v
		if i, ok := idx[k]; ok {
			out[i] = entry
		} else {
			out = append(out, entry)
		}
	}
	return out
}
