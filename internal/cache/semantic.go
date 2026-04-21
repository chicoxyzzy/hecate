package cache

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/internal/models"
	"github.com/hecate/agent-runtime/internal/requestscope"
	"github.com/hecate/agent-runtime/pkg/types"
)

type SemanticStore interface {
	Search(ctx context.Context, query SemanticQuery) (*SemanticMatch, bool)
	Set(ctx context.Context, entry SemanticEntry) error
}

type SemanticQuery struct {
	Namespace     string
	Text          string
	MinSimilarity float64
	MaxTextChars  int
}

type SemanticMatch struct {
	Response   *types.ChatResponse
	Similarity float64
	Strategy   string
	IndexType  string
}

type SemanticEntry struct {
	Namespace string
	Text      string
	Response  *types.ChatResponse
	ExpiresAt time.Time
}

type NoopSemanticStore struct{}

func (NoopSemanticStore) Search(context.Context, SemanticQuery) (*SemanticMatch, bool) {
	return nil, false
}

func (NoopSemanticStore) Set(context.Context, SemanticEntry) error {
	return nil
}

type MemorySemanticStore struct {
	mu         sync.RWMutex
	entries    []semanticRecord
	defaultTTL time.Duration
	maxEntries int
	embedder   Embedder
}

type semanticRecord struct {
	entry  SemanticEntry
	vector []float64
}

func NewMemorySemanticStore(defaultTTL time.Duration, maxEntries int, embedder Embedder) *MemorySemanticStore {
	if maxEntries <= 0 {
		maxEntries = 10_000
	}
	if embedder == nil {
		embedder = LocalSimpleEmbedder{}
	}
	return &MemorySemanticStore{
		entries:    make([]semanticRecord, 0, maxEntries),
		defaultTTL: defaultTTL,
		maxEntries: maxEntries,
		embedder:   embedder,
	}
}

func (s *MemorySemanticStore) Search(ctx context.Context, query SemanticQuery) (*SemanticMatch, bool) {
	text := query.Text
	if query.MaxTextChars > 0 && len(text) > query.MaxTextChars {
		text = text[:query.MaxTextChars]
	}
	queryVector, err := s.embedder.Embed(ctx, text)
	if err != nil {
		return nil, false
	}
	if len(queryVector) == 0 {
		return nil, false
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := s.entries[:0]
	var best *SemanticMatch
	for _, record := range s.entries {
		if !record.entry.ExpiresAt.IsZero() && now.After(record.entry.ExpiresAt) {
			continue
		}
		filtered = append(filtered, record)
		if record.entry.Namespace != query.Namespace {
			continue
		}
		score := cosineSimilarity(queryVector, record.vector)
		if score < query.MinSimilarity {
			continue
		}
		if best == nil || score > best.Similarity {
			cloned := cloneChatResponse(record.entry.Response)
			best = &SemanticMatch{
				Response:   cloned,
				Similarity: score,
				Strategy:   "memory_scan",
			}
		}
	}
	s.entries = filtered
	return best, best != nil
}

func (s *MemorySemanticStore) Set(ctx context.Context, entry SemanticEntry) error {
	if entry.Response == nil || strings.TrimSpace(entry.Namespace) == "" || strings.TrimSpace(entry.Text) == "" {
		return nil
	}
	vector, err := s.embedder.Embed(ctx, entry.Text)
	if err != nil || len(vector) == 0 {
		return err
	}

	if entry.ExpiresAt.IsZero() && s.defaultTTL > 0 {
		entry.ExpiresAt = time.Now().Add(s.defaultTTL)
	}
	entry.Response = cloneChatResponse(entry.Response)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, semanticRecord{
		entry:  entry,
		vector: append([]float64(nil), vector...),
	})
	if len(s.entries) > s.maxEntries {
		s.entries = append([]semanticRecord(nil), s.entries[len(s.entries)-s.maxEntries:]...)
	}
	return nil
}

func BuildSemanticNamespace(req types.ChatRequest, decision types.RouteDecision) string {
	tenant := requestscope.EffectiveTenant(requestscope.FromChatRequest(req), "anonymous")
	parts := []string{
		"tenant:" + tenant,
		"provider:" + decision.Provider,
		"model:" + models.Canonicalize(decision.Model),
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func BuildSemanticText(req types.ChatRequest, maxChars int) string {
	var lines []string
	for _, msg := range req.Messages {
		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role == "" {
			role = "message"
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		lines = append(lines, role+": "+content)
	}
	text := strings.Join(lines, "\n")
	if maxChars > 0 && len(text) > maxChars {
		text = text[:maxChars]
	}
	return text
}

var semanticUnsafePattern = regexp.MustCompile(`\b(today|latest|current|recent|news|price|stock|score|weather)\b`)

func EligibleForSemanticCache(req types.ChatRequest, maxChars int) bool {
	text := strings.TrimSpace(BuildSemanticText(req, maxChars))
	if text == "" {
		return false
	}
	if semanticUnsafePattern.MatchString(strings.ToLower(text)) {
		return false
	}
	return true
}

func cloneChatResponse(resp *types.ChatResponse) *types.ChatResponse {
	if resp == nil {
		return nil
	}
	cloned := *resp
	cloned.Choices = append([]types.ChatChoice(nil), resp.Choices...)
	return &cloned
}
