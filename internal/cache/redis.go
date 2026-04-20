package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/pkg/types"
)

type RedisStore struct {
	client     *storage.RedisClient
	prefix     string
	defaultTTL time.Duration
}

func NewRedisStore(client *storage.RedisClient, prefix string, defaultTTL time.Duration) *RedisStore {
	return &RedisStore{
		client:     client,
		prefix:     prefix,
		defaultTTL: defaultTTL,
	}
}

func (s *RedisStore) Get(ctx context.Context, key string) (*types.ChatResponse, bool) {
	payload, err := s.client.Get(ctx, s.cacheKey(key))
	if err != nil {
		if errors.Is(err, storage.ErrNil) {
			return nil, false
		}
		return nil, false
	}

	var response types.ChatResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, false
	}
	return &response, true
}

func (s *RedisStore) Set(ctx context.Context, key string, response *types.ChatResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return s.client.SetEX(ctx, s.cacheKey(key), s.defaultTTL, payload)
}

func (s *RedisStore) cacheKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + ":cache:" + key
}
