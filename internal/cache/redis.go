package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/pkg/types"
)

type RedisStore struct {
	client     *storage.RedisClient
	prefix     string
	defaultTTL time.Duration
}

type redisCacheEnvelope struct {
	Response  *types.ChatResponse `json:"response"`
	WrittenAt time.Time           `json:"written_at"`
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

	var envelope redisCacheEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Response == nil {
		return nil, false
	}
	return envelope.Response, true
}

func (s *RedisStore) Set(ctx context.Context, key string, response *types.ChatResponse) error {
	payload, err := json.Marshal(redisCacheEnvelope{
		Response:  response,
		WrittenAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	return s.client.SetEX(ctx, s.cacheKey(key), s.defaultTTL, payload)
}

func (s *RedisStore) Prune(ctx context.Context, maxAge time.Duration, maxCount int) (int, error) {
	keys, err := s.client.Keys(ctx, s.cacheKey("*"))
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}

	type candidate struct {
		key       string
		writtenAt time.Time
	}

	now := time.Now()
	deleted := 0
	candidates := make([]candidate, 0, len(keys))
	for _, key := range keys {
		payload, err := s.client.Get(ctx, key)
		if err != nil {
			continue
		}
		var envelope redisCacheEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			continue
		}
		if maxAge > 0 && !envelope.WrittenAt.IsZero() && envelope.WrittenAt.Before(now.Add(-maxAge)) {
			n, err := s.client.Del(ctx, key)
			if err != nil {
				return deleted, err
			}
			if (strconv.IntSize == 32 && (n > math.MaxInt32 || n < math.MinInt32)) || (strconv.IntSize == 64 && (n > math.MaxInt64 || n < math.MinInt64)) {
				return deleted, fmt.Errorf("DEL result out of int range: %d", n)
			}
			deleted += int(n)
			continue
		}
		candidates = append(candidates, candidate{key: key, writtenAt: envelope.WrittenAt})
	}

	if maxCount > 0 && len(candidates) > maxCount {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].writtenAt.After(candidates[j].writtenAt)
		})
		for _, item := range candidates[maxCount:] {
			n, err := s.client.Del(ctx, item.key)
			if err != nil {
				return deleted, err
			}
			if (strconv.IntSize == 32 && (n > math.MaxInt32 || n < math.MinInt32)) || (strconv.IntSize == 64 && (n > math.MaxInt64 || n < math.MinInt64)) {
				return deleted, fmt.Errorf("DEL result out of int range: %d", n)
			}
			deleted += int(n)
		}
	}
	return deleted, nil
}

func (s *RedisStore) cacheKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + ":cache:" + key
}
