package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/mcp"
	mcpclient "github.com/hecate/agent-runtime/internal/mcp/client"
	"github.com/hecate/agent-runtime/internal/secrets"
	"github.com/hecate/agent-runtime/internal/telemetry"
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

// DefaultMCPHostFactory is the no-cipher / no-cache default. Use
// NewDefaultMCPHostFactory(cipher, cache) via Runner.SetMCPHostFactory
// when the control-plane cipher is available (so env values stored as
// "enc:<base64>" are decrypted at spawn time) and/or when a shared
// client cache is wired (so subprocesses are reused across runs).
var DefaultMCPHostFactory AgentMCPHostFactory = NewDefaultMCPHostFactory(nil, nil)

// NewDefaultMCPHostFactory returns a production factory that resolves
// secret env values and produces a Pool per run. cipher may be nil —
// enc:-prefixed values that arrive without a cipher return a clear
// error at spawn time so the operator knows the key is missing, rather
// than forwarding ciphertext to the subprocess.
//
// cache may also be nil. When non-nil, every per-server client is
// acquired from the cache and released on Pool.Close, so subsequent
// runs that configure the same upstream skip the spawn cost. When nil,
// the factory falls back to the existing per-run lifetime — every run
// spawns and closes its own subprocesses.
func NewDefaultMCPHostFactory(cipher secrets.Cipher, cache *mcpclient.SharedClientCache) AgentMCPHostFactory {
	return func(ctx context.Context, configs []types.MCPServerConfig) (AgentMCPHost, error) {
		if len(configs) == 0 {
			return nil, nil
		}
		resolved, err := resolveEnvConfigs(configs, cipher)
		if err != nil {
			return nil, err
		}
		clientCfgs := toClientServerConfigs(resolved)
		var pool *mcpclient.Pool
		if cache != nil {
			pool, err = mcpclient.NewPoolWithCache(ctx, clientCfgs, cache)
		} else {
			pool, err = mcpclient.NewPool(ctx, agentClientInfo(), clientCfgs)
		}
		if err != nil {
			return nil, err
		}
		return &poolMCPHost{pool: pool}, nil
	}
}

// isEnvRef reports whether v is a $VAR_NAME reference. Accepted syntax
// is a dollar sign followed by a POSIX env-var name: the first
// character must be [A-Za-z_] and subsequent characters [A-Za-z0-9_].
// A bare "$", "$123", or "$foo-bar" are not valid references.
func isEnvRef(v string) bool {
	if len(v) < 2 || v[0] != '$' {
		return false
	}
	for i, c := range v[1:] {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
			// valid in any position
		case c >= '0' && c <= '9':
			if i == 0 {
				return false // can't start with a digit
			}
		default:
			return false
		}
	}
	return true
}

// resolveEnvValue resolves a single env value at subprocess spawn time:
//
//   - "$VAR_NAME" — looked up via os.LookupEnv; errors if unset or empty
//     (an empty token is almost always a misconfiguration).
//   - "enc:<base64>" — decrypted with cipher; errors if cipher is nil
//     (key not configured) or decryption fails.
//   - starts with "$" but is not a valid name → error (malformed reference).
//   - anything else → returned as a literal, unchanged.
func resolveEnvValue(serverName, key, value string, cipher secrets.Cipher) (string, error) {
	switch {
	case strings.HasPrefix(value, types.MCPEnvEncPrefix):
		if cipher == nil {
			return "", fmt.Errorf("mcp server %q: env %q: value is encrypted (enc:) but no control-plane secret key is configured", serverName, key)
		}
		plaintext, err := cipher.Decrypt(value[len(types.MCPEnvEncPrefix):])
		if err != nil {
			return "", fmt.Errorf("mcp server %q: env %q: decrypt: %w", serverName, key, err)
		}
		return plaintext, nil

	case len(value) > 0 && value[0] == '$':
		if !isEnvRef(value) {
			return "", fmt.Errorf("mcp server %q: env %q: %q looks like a variable reference but is not a valid env-var name (expected $NAME)", serverName, key, value)
		}
		varName := value[1:]
		resolved, exists := os.LookupEnv(varName)
		if !exists {
			return "", fmt.Errorf("mcp server %q: env %q: $%s is not set in the runtime environment", serverName, key, varName)
		}
		if resolved == "" {
			return "", fmt.Errorf("mcp server %q: env %q: $%s is set but empty", serverName, key, varName)
		}
		return resolved, nil

	default:
		return value, nil
	}
}

// resolveEnvConfigs resolves every env value in each config. Returns a
// new slice without mutating the originals. The first resolution error
// aborts the whole set — a partial-resolution pool would spawn servers
// with wrong or missing credentials.
func resolveEnvConfigs(configs []types.MCPServerConfig, cipher secrets.Cipher) ([]types.MCPServerConfig, error) {
	if len(configs) == 0 {
		return configs, nil
	}
	out := make([]types.MCPServerConfig, len(configs))
	for i, cfg := range configs {
		resolved := cfg
		if len(cfg.Env) > 0 {
			env := make(map[string]string, len(cfg.Env))
			for k, v := range cfg.Env {
				rv, err := resolveEnvValue(cfg.Name, k, v, cipher)
				if err != nil {
					return nil, err
				}
				env[k] = rv
			}
			resolved.Env = env
		}
		if len(cfg.Headers) > 0 {
			headers := make(map[string]string, len(cfg.Headers))
			for k, v := range cfg.Headers {
				rv, err := resolveEnvValue(cfg.Name, k, v, cipher)
				if err != nil {
					return nil, err
				}
				headers[k] = rv
			}
			resolved.Headers = headers
		}
		out[i] = resolved
	}
	return out, nil
}

// agentClientInfo is what every spawned MCP server sees as the
// connecting client identity. Stable name so operators reading
// upstream server logs can correlate.
func agentClientInfo() mcp.ClientInfo {
	return mcp.ClientInfo{Name: "hecate-agent-loop", Version: version.Version}
}

// NewAgentMCPClientCache builds a SharedClientCache configured with
// the same client identity that uncached agent-loop runs use. main.go
// constructs one of these at startup, hands it to the api.Handler, and
// the handler wires it into the runner's MCP host factory. Letting
// orchestrator own the constructor keeps the agentClientInfo helper
// unexported and ensures the cache and the per-run path can never
// drift on identity strings.
//
// ttl is the idle TTL for cached entries; 0 falls back to the cache's
// internal default (5 minutes).
//
// maxEntries caps how many distinct upstream clients the cache holds
// at once. When at-or-over the cap on a fresh insert, the cache evicts
// the least-recently-used IDLE entry first; if every entry is in-use,
// the over-cap insert is allowed (TTL eviction catches up later). 0
// falls back to the cache's internal default (256). Negative disables
// the cap (unbounded growth — used by tests that don't care).
//
// metrics, when non-nil, gets wired in as a CacheObserver so cache
// hit/miss/evict events show up on the cache-events counter. nil =
// no metrics (cache still functions; callers just lose observability).
func NewAgentMCPClientCache(ttl time.Duration, maxEntries int, metrics *telemetry.OrchestratorMetrics) *mcpclient.SharedClientCache {
	var cache *mcpclient.SharedClientCache
	if maxEntries == 0 {
		// Distinguish "operator left the field zero-valued" from
		// "operator deliberately disabled the cap" by treating zero
		// as "use the cache's default" and negative as "disabled."
		// The orchestrator.Config field documentation calls this out.
		cache = mcpclient.NewSharedClientCache(ttl, agentClientInfo())
	} else {
		cache = mcpclient.NewSharedClientCacheWithLimits(ttl, maxEntries, agentClientInfo())
	}
	if metrics != nil {
		// Capture metrics in closures so the cache stays free of any
		// telemetry-package dependency. The closures themselves are
		// nil-safe (the metrics SDK no-ops on nil instruments).
		cache.SetObserver(&mcpclient.CacheObserver{
			OnHit: func(server string) {
				metrics.RecordMCPCacheEvent(context.Background(), telemetry.MCPCacheEventRecord{
					Server: server, Event: telemetry.MCPCacheEventHit,
				})
			},
			OnMiss: func(server string) {
				metrics.RecordMCPCacheEvent(context.Background(), telemetry.MCPCacheEventRecord{
					Server: server, Event: telemetry.MCPCacheEventMiss,
				})
			},
			OnEvicted: func(server string) {
				metrics.RecordMCPCacheEvent(context.Background(), telemetry.MCPCacheEventRecord{
					Server: server, Event: telemetry.MCPCacheEventEvicted,
				})
			},
		})
	}
	return cache
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
			URL:     c.URL,
			Headers: c.Headers,
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
