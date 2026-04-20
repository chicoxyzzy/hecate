package telemetry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Metrics struct {
	mu                   sync.Mutex
	chatRequestsTotal    int64
	cacheHitsTotal       int64
	cacheMissesTotal     int64
	costMicrosTotal      int64
	providerRequests     map[string]int64
	providerKindRequests map[string]int64
	cacheTypeRequests    map[string]int64
	semanticStrategyHits map[string]int64
	semanticIndexHits    map[string]int64
}

func NewMetrics() *Metrics {
	return &Metrics{
		providerRequests:     make(map[string]int64),
		providerKindRequests: make(map[string]int64),
		cacheTypeRequests:    make(map[string]int64),
		semanticStrategyHits: make(map[string]int64),
		semanticIndexHits:    make(map[string]int64),
	}
}

func (m *Metrics) RecordChat(provider, providerKind string, cacheHit bool, cacheType string, semanticStrategy string, semanticIndex string, costMicros int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.chatRequestsTotal++
	if cacheHit {
		m.cacheHitsTotal++
	} else {
		m.cacheMissesTotal++
	}
	m.costMicrosTotal += costMicros
	if provider != "" {
		m.providerRequests[provider]++
	}
	if providerKind != "" {
		m.providerKindRequests[providerKind]++
	}
	if cacheType != "" {
		m.cacheTypeRequests[cacheType]++
	}
	if semanticStrategy != "" {
		m.semanticStrategyHits[semanticStrategy]++
	}
	if semanticIndex != "" {
		m.semanticIndexHits[semanticIndex]++
	}
}

type Snapshot struct {
	ChatRequestsTotal    int64
	CacheHitsTotal       int64
	CacheMissesTotal     int64
	CostMicrosTotal      int64
	ProviderRequests     map[string]int64
	ProviderKindRequests map[string]int64
	CacheTypeRequests    map[string]int64
	SemanticStrategyHits map[string]int64
	SemanticIndexHits    map[string]int64
}

func (m *Metrics) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	providerRequests := make(map[string]int64, len(m.providerRequests))
	for k, v := range m.providerRequests {
		providerRequests[k] = v
	}
	providerKindRequests := make(map[string]int64, len(m.providerKindRequests))
	for k, v := range m.providerKindRequests {
		providerKindRequests[k] = v
	}
	cacheTypeRequests := make(map[string]int64, len(m.cacheTypeRequests))
	for k, v := range m.cacheTypeRequests {
		cacheTypeRequests[k] = v
	}
	semanticStrategyHits := make(map[string]int64, len(m.semanticStrategyHits))
	for k, v := range m.semanticStrategyHits {
		semanticStrategyHits[k] = v
	}
	semanticIndexHits := make(map[string]int64, len(m.semanticIndexHits))
	for k, v := range m.semanticIndexHits {
		semanticIndexHits[k] = v
	}

	return Snapshot{
		ChatRequestsTotal:    m.chatRequestsTotal,
		CacheHitsTotal:       m.cacheHitsTotal,
		CacheMissesTotal:     m.cacheMissesTotal,
		CostMicrosTotal:      m.costMicrosTotal,
		ProviderRequests:     providerRequests,
		ProviderKindRequests: providerKindRequests,
		CacheTypeRequests:    cacheTypeRequests,
		SemanticStrategyHits: semanticStrategyHits,
		SemanticIndexHits:    semanticIndexHits,
	}
}

type ProviderHealthSnapshot struct {
	HealthyCount  int
	DegradedCount int
}

func RenderPrometheus(snapshot Snapshot, health ProviderHealthSnapshot) string {
	var b strings.Builder

	writeHelpType(&b, "gateway_chat_requests_total", "Total chat completion requests handled by the gateway.", "counter")
	fmt.Fprintf(&b, "gateway_chat_requests_total %d\n", snapshot.ChatRequestsTotal)

	writeHelpType(&b, "gateway_cache_hits_total", "Total exact cache hits.", "counter")
	fmt.Fprintf(&b, "gateway_cache_hits_total %d\n", snapshot.CacheHitsTotal)

	writeHelpType(&b, "gateway_cache_misses_total", "Total exact cache misses.", "counter")
	fmt.Fprintf(&b, "gateway_cache_misses_total %d\n", snapshot.CacheMissesTotal)

	writeHelpType(&b, "gateway_cost_micros_usd_total", "Accumulated estimated cost in micros of USD.", "counter")
	fmt.Fprintf(&b, "gateway_cost_micros_usd_total %d\n", snapshot.CostMicrosTotal)

	writeHelpType(&b, "gateway_provider_requests_total", "Requests routed to each provider.", "counter")
	for _, key := range sortedKeys(snapshot.ProviderRequests) {
		fmt.Fprintf(&b, "gateway_provider_requests_total{provider=%q} %d\n", key, snapshot.ProviderRequests[key])
	}

	writeHelpType(&b, "gateway_provider_kind_requests_total", "Requests handled by provider kind.", "counter")
	for _, key := range sortedKeys(snapshot.ProviderKindRequests) {
		fmt.Fprintf(&b, "gateway_provider_kind_requests_total{provider_kind=%q} %d\n", key, snapshot.ProviderKindRequests[key])
	}

	writeHelpType(&b, "gateway_cache_type_requests_total", "Requests grouped by cache result type.", "counter")
	for _, key := range sortedKeys(snapshot.CacheTypeRequests) {
		fmt.Fprintf(&b, "gateway_cache_type_requests_total{cache_type=%q} %d\n", key, snapshot.CacheTypeRequests[key])
	}

	writeHelpType(&b, "gateway_semantic_strategy_hits_total", "Semantic cache hits grouped by retrieval strategy.", "counter")
	for _, key := range sortedKeys(snapshot.SemanticStrategyHits) {
		fmt.Fprintf(&b, "gateway_semantic_strategy_hits_total{strategy=%q} %d\n", key, snapshot.SemanticStrategyHits[key])
	}

	writeHelpType(&b, "gateway_semantic_index_hits_total", "Semantic cache hits grouped by index type.", "counter")
	for _, key := range sortedKeys(snapshot.SemanticIndexHits) {
		fmt.Fprintf(&b, "gateway_semantic_index_hits_total{index_type=%q} %d\n", key, snapshot.SemanticIndexHits[key])
	}

	writeHelpType(&b, "gateway_provider_health", "Current provider health counts.", "gauge")
	fmt.Fprintf(&b, "gateway_provider_health{status=%q} %d\n", "healthy", health.HealthyCount)
	fmt.Fprintf(&b, "gateway_provider_health{status=%q} %d\n", "degraded", health.DegradedCount)

	return b.String()
}

func writeHelpType(b *strings.Builder, name, help, metricType string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s %s\n", name, metricType)
}

func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
