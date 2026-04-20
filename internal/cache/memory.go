package cache

import (
	"context"
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
	}
	return nil
}
