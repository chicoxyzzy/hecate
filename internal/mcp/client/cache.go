package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/internal/mcp"
)

// SharedClientCache amortizes MCP-client startup across runs. Today
// every agent-loop run spawns its MCP subprocesses fresh and tears
// them down at run end — paying ~hundreds of ms per stdio server on
// process exec + initialize handshake + tools/list. The cache holds
// one Client per upstream config and hands it back on subsequent
// runs, so an operator firing a batch of tasks against the same
// server pays the spawn cost once instead of N times.
//
// Lifecycle:
//
//   - Acquire(cfg) returns the cached Client (and its tools snapshot)
//     for cfg, spawning one on a miss. The caller gets back a release
//     func and must call it when done — the cache decrements the
//     refcount but does NOT immediately close the Client.
//   - Idle entries (refcount == 0, lastUsed older than ttl) are
//     evicted by a background reaper goroutine. This is the only
//     normal teardown path.
//   - Evict(cfg) removes a cached entry on demand. Used by callers
//     who detect a transport-level error (subprocess died) so the
//     next Acquire spawns fresh instead of returning the dead client.
//   - Close stops the reaper and tears down every cached Client.
//     Idempotent. Caller must ensure no in-flight runs are still
//     using cached clients before Close, since Close cuts those
//     connections.
//
// Concurrency: every public method is safe for concurrent use. The
// internal lock is held only for short bookkeeping; spawning happens
// outside the lock (with a race-recheck on insert) so a slow upstream
// init doesn't serialize unrelated Acquires.
//
// Cache key: a SHA-256 over the transport-identifying fields of
// ServerConfig — Command/Args/Env for stdio, URL/Headers for HTTP.
// The operator-chosen Name is intentionally excluded: it's the
// per-task alias used to namespace tools (mcp__<name>__<tool>), not
// part of upstream identity. Two tasks aliasing the same upstream as
// "fs" and "filesystem" share one subprocess.
type SharedClientCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry

	ttl    time.Duration
	info   mcp.ClientInfo
	reaper time.Duration

	// maxEntries is the soft cap on cached upstream count. Acquire's
	// miss path evicts the least-recently-used IDLE entry before
	// inserting a new one when the cache is at-or-over this size. If
	// every entry is in-use (refcount > 0) the over-cap insert is
	// allowed — rejecting an Acquire would break a legitimate run,
	// and TTL eviction will catch up once anything goes idle. 0
	// disables the cap (used by tests that don't care).
	maxEntries int

	closeCh   chan struct{}
	closeOnce sync.Once
	reaperWg  sync.WaitGroup
}

type cacheEntry struct {
	client   *Client
	tools    []mcp.Tool
	inUse    int
	lastUsed time.Time
}

const (
	defaultCacheTTL       = 5 * time.Minute
	defaultReaperInterval = 30 * time.Second
	// defaultCacheMaxEntries is the SharedClientCache's default soft
	// cap. 256 is generous for any real deployment (most operators
	// use 1-3 distinct MCP servers across all their tasks) but tight
	// enough to bound a runaway tenant or a config-permutation churn
	// from accumulating an unbounded set of cached subprocesses.
	defaultCacheMaxEntries = 256
)

// NewSharedClientCache builds a cache with the given idle TTL and
// a sensible default max-entries cap (256). Every Client the cache
// spawns reports info as its MCP ClientInfo on the initialize
// handshake, so upstream server logs identify a single stable client
// identity (e.g. "hecate-agent-loop / <version>") regardless of which
// run triggered the spawn.
//
// ttl <= 0 falls back to defaultCacheTTL (5 minutes). The reaper runs
// every 30s and evicts entries idle past ttl.
//
// For deployments that need a different cap, see
// NewSharedClientCacheWithLimits.
func NewSharedClientCache(ttl time.Duration, info mcp.ClientInfo) *SharedClientCache {
	return newSharedClientCacheFull(ttl, defaultReaperInterval, defaultCacheMaxEntries, info)
}

// NewSharedClientCacheWithLimits is the explicit-cap counterpart for
// callers that want to override the max-entries cap (e.g. a deployment
// expecting many distinct MCP servers per tenant). maxEntries <= 0
// disables the cap entirely — only TTL eviction applies.
//
// All other knobs (ttl, reaper interval) match NewSharedClientCache.
func NewSharedClientCacheWithLimits(ttl time.Duration, maxEntries int, info mcp.ClientInfo) *SharedClientCache {
	return newSharedClientCacheFull(ttl, defaultReaperInterval, maxEntries, info)
}

// newSharedClientCacheFull is the internal constructor that takes
// every knob. Tests use it to drive tight TTL / reaper / cap windows
// without racing the reaper goroutine — every field must be set
// BEFORE the goroutine starts (mutating after construction would race
// with the goroutine's own reads).
//
// Sentinel handling: ttl <= 0 → defaultCacheTTL; reaperInterval <= 0
// → defaultReaperInterval; maxEntries <= 0 → cap disabled.
func newSharedClientCacheFull(ttl, reaperInterval time.Duration, maxEntries int, info mcp.ClientInfo) *SharedClientCache {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	if reaperInterval <= 0 {
		reaperInterval = defaultReaperInterval
	}
	c := &SharedClientCache{
		entries:    make(map[string]*cacheEntry),
		ttl:        ttl,
		info:       info,
		reaper:     reaperInterval,
		maxEntries: maxEntries,
		closeCh:    make(chan struct{}),
	}
	c.reaperWg.Add(1)
	go c.reaperLoop()
	return c
}

// newSharedClientCacheWithReaper is the legacy three-arg constructor
// kept for tests written against the prior signature. Treats the cap
// as disabled (0) — explicit cap tests use newSharedClientCacheFull
// directly.
func newSharedClientCacheWithReaper(ttl, reaperInterval time.Duration, info mcp.ClientInfo) *SharedClientCache {
	return newSharedClientCacheFull(ttl, reaperInterval, 0, info)
}

// Acquire returns a Client + tools snapshot for cfg, spawning one on
// a miss. The caller MUST call the returned release func exactly once
// when finished — that decrements the refcount so the reaper can
// eventually evict the entry.
//
// Returns the same (client, tools) tuple for every Acquire of the
// same cfg until either Evict is called or Close runs. The tools
// slice is the upstream's catalog at first-init time; we don't
// re-list across runs because tools/list is rarely dynamic and
// re-running it on every Acquire would erase most of the cache's
// latency win.
//
// On error (init fails, network down) no entry is created and no
// release func is returned.
func (c *SharedClientCache) Acquire(ctx context.Context, cfg ServerConfig) (*Client, []mcp.Tool, func(), error) {
	key := configKey(cfg)

	// Fast path: cache hit.
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		e.inUse++
		e.lastUsed = time.Now()
		client, tools := e.client, e.tools
		c.mu.Unlock()
		return client, tools, c.releaseFor(key), nil
	}
	c.mu.Unlock()

	// Miss: spawn outside the lock so concurrent Acquires for OTHER
	// keys aren't blocked behind this one's process exec.
	client, tools, err := spawnClient(ctx, c.info, cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	// Race recheck: another goroutine may have spawned the same key
	// while we were spawning ours. Whoever inserts first wins; the
	// loser closes its Client and uses the winner's.
	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		c.mu.Unlock()
		_ = client.Close()
		c.mu.Lock()
		e.inUse++
		e.lastUsed = time.Now()
		client, tools = e.client, e.tools
		c.mu.Unlock()
		return client, tools, c.releaseFor(key), nil
	}
	// Cap enforcement: if we're at-or-over maxEntries before this
	// insert, try to evict the least-recently-used IDLE entry first
	// so the new insert doesn't grow the working set unbounded. If
	// every entry is in-use we allow the over-cap insert anyway —
	// blocking Acquire would break a legitimate run, and TTL eviction
	// or future releases will catch up. Eviction happens INSIDE the
	// lock so a concurrent Acquire can't race into the slot we're
	// freeing; the actual Close call goes outside the lock so a slow
	// teardown doesn't block other operations.
	var evicted *Client
	if c.maxEntries > 0 && len(c.entries) >= c.maxEntries {
		if victimKey := c.pickLRUIdleLocked(); victimKey != "" {
			evicted = c.entries[victimKey].client
			delete(c.entries, victimKey)
		}
	}
	c.entries[key] = &cacheEntry{
		client:   client,
		tools:    tools,
		inUse:    1,
		lastUsed: time.Now(),
	}
	c.mu.Unlock()
	if evicted != nil {
		_ = evicted.Close()
	}
	return client, tools, c.releaseFor(key), nil
}

// pickLRUIdleLocked returns the key of the least-recently-used entry
// with refcount == 0, or "" if no idle entry exists. Caller must hold
// c.mu. O(N) over the cache; cheap enough for a cap of a few hundred.
func (c *SharedClientCache) pickLRUIdleLocked() string {
	var (
		victimKey  string
		victimTime time.Time
	)
	for key, e := range c.entries {
		if e.inUse > 0 {
			continue
		}
		if victimKey == "" || e.lastUsed.Before(victimTime) {
			victimKey = key
			victimTime = e.lastUsed
		}
	}
	return victimKey
}

// Evict removes a cached entry on demand and tears down its Client.
// Use when a Pool.Call returns a transport-closed error, indicating
// the subprocess died — without eviction the next Acquire would
// hand back the same dead client.
//
// No-op if the cfg isn't currently cached. The Client is closed
// outside the cache lock so a slow tear-down doesn't block other
// Acquires.
func (c *SharedClientCache) Evict(cfg ServerConfig) {
	key := configKey(cfg)
	c.mu.Lock()
	e, ok := c.entries[key]
	if ok {
		delete(c.entries, key)
	}
	c.mu.Unlock()
	if ok {
		_ = e.client.Close()
	}
}

// Close stops the reaper and tears down every cached Client.
// Idempotent — a second call is a no-op. Errors from individual
// client closes are joined so the operator sees them all.
//
// Callers should ensure no in-flight runs are still holding cached
// clients before Close — typically Runner.Shutdown drains those
// first. Close does NOT wait for refcount=0; it tears entries down
// regardless, so a stragller run will see a transport error.
func (c *SharedClientCache) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeCh)
	})
	c.reaperWg.Wait()

	c.mu.Lock()
	clients := make([]*Client, 0, len(c.entries))
	for _, e := range c.entries {
		clients = append(clients, e.client)
	}
	c.entries = make(map[string]*cacheEntry)
	c.mu.Unlock()

	var errs []error
	for _, cl := range clients {
		if err := cl.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// CacheStats is a snapshot of cache occupancy. Used by tests and the
// /v1/admin/stats endpoint to confirm the cache is doing useful work.
//
//   - Entries is the number of distinct cached upstreams.
//   - InUse is the SUM of refcounts across all entries — i.e. the
//     total number of live Acquire→Release pairs in flight, NOT the
//     count of entries with at least one acquirer. (An entry held by
//     two concurrent runs contributes 2 to InUse and 0 to Idle.)
//   - Idle is the number of entries with refcount == 0; these are
//     the ones the reaper will evict once their lastUsed crosses the
//     TTL boundary.
//
// Entries == InUse-bucket-entries + Idle.
type CacheStats struct {
	Entries int
	InUse   int
	Idle    int
}

// Stats returns the current cache state. Cheap; safe to call hot.
func (c *SharedClientCache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := CacheStats{Entries: len(c.entries)}
	for _, e := range c.entries {
		s.InUse += e.inUse
		if e.inUse == 0 {
			s.Idle++
		}
	}
	return s
}

func (c *SharedClientCache) releaseFor(key string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if e, ok := c.entries[key]; ok {
				e.inUse--
				if e.inUse < 0 {
					// Defensive: a double-release would otherwise let
					// the reaper kick out an entry someone is still
					// holding. Clamp to zero rather than panic.
					e.inUse = 0
				}
				e.lastUsed = time.Now()
			}
		})
	}
}

func (c *SharedClientCache) reaperLoop() {
	defer c.reaperWg.Done()
	ticker := time.NewTicker(c.reaper)
	defer ticker.Stop()
	for {
		select {
		case <-c.closeCh:
			return
		case <-ticker.C:
			c.evictIdle()
		}
	}
}

func (c *SharedClientCache) evictIdle() {
	cutoff := time.Now().Add(-c.ttl)
	c.mu.Lock()
	var toClose []*Client
	for key, e := range c.entries {
		if e.inUse == 0 && e.lastUsed.Before(cutoff) {
			toClose = append(toClose, e.client)
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
	for _, cl := range toClose {
		_ = cl.Close()
	}
}

// configKey is the SHA-256 cache key over a ServerConfig's
// transport-identifying fields. We sort env/header maps before
// hashing so two configs that differ only in iteration order map to
// the same key.
//
// Name is intentionally NOT in the key — it's the operator-chosen
// alias used by Pool to namespace tools, not part of the upstream
// identity. Two tasks aliasing the same upstream as "fs" and
// "filesystem" share one subprocess.
func configKey(cfg ServerConfig) string {
	h := sha256.New()
	h.Write([]byte("cmd|"))
	h.Write([]byte(cfg.Command))
	for _, a := range cfg.Args {
		h.Write([]byte("\x00arg|"))
		h.Write([]byte(a))
	}
	for _, k := range sortedMapKeys(cfg.Env) {
		h.Write([]byte("\x00env|"))
		h.Write([]byte(k))
		h.Write([]byte{'='})
		h.Write([]byte(cfg.Env[k]))
	}
	h.Write([]byte("\x00url|"))
	h.Write([]byte(cfg.URL))
	for _, k := range sortedMapKeys(cfg.Headers) {
		h.Write([]byte("\x00hdr|"))
		h.Write([]byte(k))
		h.Write([]byte{'='})
		h.Write([]byte(cfg.Headers[k]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sortedMapKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// IsTransportClosedErr reports whether err is the kind of transport
// failure that warrants evicting the cached client (subprocess died,
// HTTP server hung up, stdio EOF). Pool.Call uses this to decide
// when to call Cache.Evict.
func IsTransportClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrTransportClosed)
}
