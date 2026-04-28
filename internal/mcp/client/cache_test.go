package client

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/mcp"
)

// makeCacheTestServer wires an httptest-backed MCP server that
// records every initialize handshake. Tests use the count to detect
// whether an Acquire hit the cache (no new spawn = count unchanged) or
// missed (count increments).
//
// Returns the URL plus a closure that returns the current spawn count.
// We reuse the JSON-RPC plumbing from pool_http_test (newTestMCPHTTPServer
// + registerStandardHandlers) so the fixture stays a single source of
// truth across the cache and pool integration tests.
func makeCacheTestServer(t *testing.T, name string, tools []mcp.Tool) (url string, initCount func() int32) {
	t.Helper()
	hs, srv := newTestMCPHTTPServer(t)
	var inits atomic.Int32
	// Wrap initialize so we can count it. The standard registrar sets
	// handlers AFTER we capture the URL, so we replace just the
	// initialize handler with a counting variant.
	registerStandardHandlers(srv, name, tools, map[string]func(json.RawMessage) mcp.CallToolResult{})
	srv.handle("initialize", func(req mcp.Request) (any, *mcp.RPCError) {
		inits.Add(1)
		return mcp.InitializeResult{
			ProtocolVersion: declaredClientProtocolVersion,
			Capabilities:    mcp.ServerCapabilities{Tools: &mcp.ToolsCapability{}},
			ServerInfo:      mcp.ServerInfo{Name: name, Version: "0.0.0"},
		}, nil
	})
	return hs.URL, inits.Load
}

// newCacheTestCache builds a SharedClientCache with a tight TTL and
// reaper interval suitable for tests. The reaper interval is passed
// through the unexported constructor so it's set BEFORE the reaper
// goroutine starts — mutating c.reaper after construction would
// race with the goroutine's read of it in reaperLoop. (An earlier
// version of this helper did exactly that and tripped go test
// -race; the unexported newSharedClientCacheWithReaper is the
// race-free seam tests should use.)
//
// reaper == 0 falls back to the cache's internal default
// (defaultReaperInterval = 30s) — fine for tests that don't care
// about reaper timing.
func newCacheTestCache(t *testing.T, ttl, reaper time.Duration) *SharedClientCache {
	t.Helper()
	c := newSharedClientCacheWithReaper(ttl, reaper, mcp.ClientInfo{Name: "hecate-cache-test", Version: "0.0.0"})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestCache_Acquire_HitsAndMisses pins the core caching invariant: a
// second Acquire with the same config returns the same Client without
// re-spawning, while a different config triggers a fresh spawn.
func TestCache_Acquire_HitsAndMisses(t *testing.T) {
	t.Parallel()
	urlA, initsA := makeCacheTestServer(t, "a", []mcp.Tool{
		{Name: "t1", InputSchema: json.RawMessage(`{}`)},
	})
	urlB, initsB := makeCacheTestServer(t, "b", []mcp.Tool{
		{Name: "t1", InputSchema: json.RawMessage(`{}`)},
	})

	cache := newCacheTestCache(t, time.Minute, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfgA := ServerConfig{Name: "alias-a", URL: urlA}
	cfgB := ServerConfig{Name: "alias-b", URL: urlB}

	// First Acquire of A → spawn.
	clientA1, _, releaseA1, err := cache.Acquire(ctx, cfgA)
	if err != nil {
		t.Fatalf("Acquire A 1: %v", err)
	}
	if got := initsA(); got != 1 {
		t.Errorf("server A initialize count = %d, want 1 after first Acquire", got)
	}

	// Second Acquire of A with the SAME upstream URL → must hit cache,
	// no new initialize.
	clientA2, _, releaseA2, err := cache.Acquire(ctx, cfgA)
	if err != nil {
		t.Fatalf("Acquire A 2: %v", err)
	}
	if got := initsA(); got != 1 {
		t.Errorf("server A initialize count = %d, want 1 after second Acquire (cache hit expected)", got)
	}
	if clientA1 != clientA2 {
		t.Error("expected the same *Client pointer on cache hit")
	}

	// Acquire B → different config, must spawn a fresh client.
	_, _, releaseB, err := cache.Acquire(ctx, cfgB)
	if err != nil {
		t.Fatalf("Acquire B: %v", err)
	}
	if got := initsB(); got != 1 {
		t.Errorf("server B initialize count = %d, want 1", got)
	}
	if got := initsA(); got != 1 {
		t.Errorf("server A initialize count drifted to %d after acquiring B", got)
	}

	stats := cache.Stats()
	if stats.Entries != 2 {
		t.Errorf("Stats.Entries = %d, want 2", stats.Entries)
	}
	// Stats.InUse sums refcounts: A acquired twice (refcount=2) plus
	// B acquired once (refcount=1) = 3 live references in flight.
	if stats.InUse != 3 {
		t.Errorf("Stats.InUse = %d, want 3 (A held twice + B held once)", stats.InUse)
	}

	releaseA1()
	releaseA2()
	releaseB()
}

// TestCache_NameNotInKey verifies that two configs differing only in
// Name (the operator's per-task alias) share a cached Client. This is
// the "two tasks aliasing the same upstream as 'fs' and 'filesystem'
// share one subprocess" case from the cache's design comment.
func TestCache_NameNotInKey(t *testing.T) {
	t.Parallel()
	url, inits := makeCacheTestServer(t, "fs", []mcp.Tool{
		{Name: "read", InputSchema: json.RawMessage(`{}`)},
	})

	cache := newCacheTestCache(t, time.Minute, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c1, _, r1, err := cache.Acquire(ctx, ServerConfig{Name: "fs", URL: url})
	if err != nil {
		t.Fatalf("Acquire fs: %v", err)
	}
	defer r1()
	c2, _, r2, err := cache.Acquire(ctx, ServerConfig{Name: "filesystem", URL: url})
	if err != nil {
		t.Fatalf("Acquire filesystem: %v", err)
	}
	defer r2()
	if c1 != c2 {
		t.Error("two configs differing only in Name should share a cached Client")
	}
	if got := inits(); got != 1 {
		t.Errorf("initialize count = %d, want 1 (Name should not affect cache key)", got)
	}
}

// TestCache_TTLEvictsIdleEntries: an entry whose refcount drops to
// zero and stays idle longer than the configured TTL is evicted by
// the reaper, freeing the underlying Client. We use a tiny TTL +
// reaper interval to exercise this in a few hundred ms.
func TestCache_TTLEvictsIdleEntries(t *testing.T) {
	t.Parallel()
	url, _ := makeCacheTestServer(t, "x", []mcp.Tool{
		{Name: "t", InputSchema: json.RawMessage(`{}`)},
	})

	cache := newCacheTestCache(t, 80*time.Millisecond, 30*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := ServerConfig{Name: "x", URL: url}

	_, _, release, err := cache.Acquire(ctx, cfg)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got := cache.Stats().Entries; got != 1 {
		t.Fatalf("Stats.Entries = %d, want 1", got)
	}
	release()

	// Wait long enough for TTL + reaper interval to fire. We poll
	// Stats() rather than sleeping a fixed duration so the test stays
	// fast on a warm machine and forgiving on a cold one.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cache.Stats().Entries == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("entry was not evicted after TTL elapsed; Stats() = %+v", cache.Stats())
}

// TestCache_TTLDoesNotEvictInUseEntries: even after the TTL elapses,
// an entry with refcount > 0 must NOT be evicted — that would yank
// the Client out from under an in-flight run. The test holds the
// release func across the TTL window and verifies the entry survives.
func TestCache_TTLDoesNotEvictInUseEntries(t *testing.T) {
	t.Parallel()
	url, _ := makeCacheTestServer(t, "x", []mcp.Tool{
		{Name: "t", InputSchema: json.RawMessage(`{}`)},
	})

	cache := newCacheTestCache(t, 50*time.Millisecond, 20*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := ServerConfig{Name: "x", URL: url}
	_, _, release, err := cache.Acquire(ctx, cfg)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	// Wait several TTL windows. The entry is in-use, so it must survive.
	time.Sleep(200 * time.Millisecond)

	stats := cache.Stats()
	if stats.Entries != 1 {
		t.Errorf("Stats.Entries = %d, want 1 (in-use entry must not evict)", stats.Entries)
	}
	if stats.InUse != 1 {
		t.Errorf("Stats.InUse = %d, want 1", stats.InUse)
	}
}

// TestCache_Evict_RemovesEntry pins the manual-eviction surface that
// Pool.Call uses on transport errors. After Evict, Stats reflects the
// removal and the next Acquire spawns fresh.
func TestCache_Evict_RemovesEntry(t *testing.T) {
	t.Parallel()
	url, inits := makeCacheTestServer(t, "x", []mcp.Tool{
		{Name: "t", InputSchema: json.RawMessage(`{}`)},
	})

	cache := newCacheTestCache(t, time.Minute, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := ServerConfig{Name: "x", URL: url}
	_, _, release, err := cache.Acquire(ctx, cfg)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	release()

	if got := inits(); got != 1 {
		t.Errorf("initialize count = %d, want 1 before Evict", got)
	}

	cache.Evict(cfg)
	if got := cache.Stats().Entries; got != 0 {
		t.Errorf("Stats.Entries = %d, want 0 after Evict", got)
	}

	// Next Acquire must respawn — incrementing the initialize counter.
	_, _, r2, err := cache.Acquire(ctx, cfg)
	if err != nil {
		t.Fatalf("Acquire after Evict: %v", err)
	}
	defer r2()
	if got := inits(); got != 2 {
		t.Errorf("initialize count = %d, want 2 (Evict should force respawn on next Acquire)", got)
	}
}

// TestCache_Close_TearsDownAllEntries: Close removes every entry and
// closes its Client, even ones that are still in-use. Idempotent on
// the second call.
func TestCache_Close_TearsDownAllEntries(t *testing.T) {
	t.Parallel()
	url1, _ := makeCacheTestServer(t, "a", []mcp.Tool{{Name: "t", InputSchema: json.RawMessage(`{}`)}})
	url2, _ := makeCacheTestServer(t, "b", []mcp.Tool{{Name: "t", InputSchema: json.RawMessage(`{}`)}})

	cache := NewSharedClientCache(time.Minute, mcp.ClientInfo{Name: "hecate-cache-close-test", Version: "0"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, _, err := cache.Acquire(ctx, ServerConfig{Name: "a", URL: url1})
	if err != nil {
		t.Fatalf("Acquire a: %v", err)
	}
	_, _, _, err = cache.Acquire(ctx, ServerConfig{Name: "b", URL: url2})
	if err != nil {
		t.Fatalf("Acquire b: %v", err)
	}

	if got := cache.Stats().Entries; got != 2 {
		t.Fatalf("Stats.Entries = %d, want 2 before Close", got)
	}

	if err := cache.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := cache.Stats().Entries; got != 0 {
		t.Errorf("Stats.Entries = %d, want 0 after Close", got)
	}
	// Idempotent: second Close is a no-op (reaperWg already drained).
	if err := cache.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestCache_ConcurrentAcquireSameKey: many goroutines racing to
// Acquire the same key must see exactly one underlying Client. The
// race-recheck in Acquire (insert under lock; if another goroutine
// won, close ours and use theirs) is what we're pinning.
func TestCache_ConcurrentAcquireSameKey(t *testing.T) {
	t.Parallel()
	url, inits := makeCacheTestServer(t, "x", []mcp.Tool{
		{Name: "t", InputSchema: json.RawMessage(`{}`)},
	})

	cache := newCacheTestCache(t, time.Minute, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := ServerConfig{Name: "x", URL: url}

	const goroutines = 10
	results := make(chan *Client, goroutines)
	releases := make(chan func(), goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			c, _, release, err := cache.Acquire(ctx, cfg)
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			results <- c
			releases <- release
		}()
	}
	wg.Wait()
	close(results)
	close(releases)

	// All goroutines must have received the SAME *Client.
	var first *Client
	for c := range results {
		if first == nil {
			first = c
			continue
		}
		if c != first {
			t.Error("concurrent Acquires for same key returned different *Clients")
			break
		}
	}

	// Race may produce extra spawns; the cache discards losers, so
	// initialize count is bounded by goroutines but on a sane impl
	// is small. We assert it's not zero (we did spawn) and not
	// pathologically large (every goroutine respawned).
	got := inits()
	if got < 1 {
		t.Errorf("initialize count = %d, want >= 1", got)
	}
	if got > goroutines {
		t.Errorf("initialize count = %d, want <= %d", got, goroutines)
	}

	for r := range releases {
		r()
	}
}

// TestCache_DoubleReleaseIsIdempotent: a release func called twice
// must not double-decrement the refcount, otherwise a third Acquire
// could see the entry as evictable and lose it under us. We Acquire
// twice (refcount=2), call the same release twice (must take it to 1,
// not 0), then verify the entry is still in-use.
func TestCache_DoubleReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	url, _ := makeCacheTestServer(t, "x", []mcp.Tool{
		{Name: "t", InputSchema: json.RawMessage(`{}`)},
	})

	cache := newCacheTestCache(t, time.Minute, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := ServerConfig{Name: "x", URL: url}
	_, _, release1, err := cache.Acquire(ctx, cfg)
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	_, _, release2, err := cache.Acquire(ctx, cfg)
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	defer release2()

	if got := cache.Stats().InUse; got != 2 {
		t.Fatalf("Stats.InUse = %d, want 2 after two Acquires", got)
	}

	// First release: 2 → 1.
	release1()
	if got := cache.Stats().InUse; got != 1 {
		t.Errorf("Stats.InUse = %d, want 1 after one release", got)
	}
	// Second release of the SAME func: must be a no-op.
	release1()
	if got := cache.Stats().InUse; got != 1 {
		t.Errorf("Stats.InUse = %d, want 1 (double-release should be idempotent)", got)
	}
}
