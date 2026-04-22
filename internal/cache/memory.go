package cache

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

type MemoryStore struct {
	mu         sync.RWMutex
	entries    map[string]entry
	defaultTTL time.Duration
}

func NewMemoryStore(defaultTTL time.Duration) *MemoryStore {
	return &MemoryStore{
		entries:    make(map[string]entry),
		defaultTTL: defaultTTL,
	}
}

func (s *MemoryStore) Get(_ context.Context, key string) (*types.ChatResponse, bool) {
	s.mu.RLock()
	found, ok := s.entries[key]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}

	if !found.expiresAt.IsZero() && time.Now().After(found.expiresAt) {
		s.mu.Lock()
		delete(s.entries, key)
		s.mu.Unlock()
		return nil, false
	}

	cloned := *found.response
	cloned.Choices = append([]types.ChatChoice(nil), found.response.Choices...)
	return &cloned, true
}

func (s *MemoryStore) Set(_ context.Context, key string, response *types.ChatResponse) error {
	cloned := *response
	cloned.Choices = append([]types.ChatChoice(nil), response.Choices...)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[key] = entry{
		response:  &cloned,
		expiresAt: time.Now().Add(s.defaultTTL),
		writtenAt: time.Now().UTC(),
	}
	return nil
}

func (s *MemoryStore) Prune(_ context.Context, maxAge time.Duration, maxCount int) (int, error) {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	type record struct {
		key   string
		entry entry
	}
	records := make([]record, 0, len(s.entries))
	for key, item := range s.entries {
		if (!item.expiresAt.IsZero() && now.After(item.expiresAt)) || (maxAge > 0 && !item.writtenAt.IsZero() && item.writtenAt.Before(now.Add(-maxAge))) {
			delete(s.entries, key)
			deleted++
			continue
		}
		records = append(records, record{key: key, entry: item})
	}

	if maxCount > 0 && len(records) > maxCount {
		sort.Slice(records, func(i, j int) bool {
			return records[i].entry.writtenAt.After(records[j].entry.writtenAt)
		})
		for _, item := range records[maxCount:] {
			delete(s.entries, item.key)
			deleted++
		}
	}

	return deleted, nil
}
