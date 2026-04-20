package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/hecate/agent-runtime/internal/models"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Store interface {
	Get(ctx context.Context, key string) (*types.ChatResponse, bool)
	Set(ctx context.Context, key string, response *types.ChatResponse) error
}

type KeyBuilder interface {
	Key(req types.ChatRequest) (string, error)
}

type StableKeyBuilder struct{}

func (StableKeyBuilder) Key(req types.ChatRequest) (string, error) {
	normalized := struct {
		Model       string          `json:"model"`
		Messages    []types.Message `json:"messages"`
		MaxTokens   int             `json:"max_tokens,omitempty"`
		Temperature float64         `json:"temperature,omitempty"`
	}{
		Model:       models.Canonicalize(req.Model),
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}

	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type entry struct {
	response  *types.ChatResponse
	expiresAt time.Time
}
