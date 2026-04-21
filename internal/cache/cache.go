package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
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
		Scope       requestScope    `json:"scope,omitempty"`
	}{
		Model:       models.Canonicalize(req.Model),
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Scope:       buildRequestScope(req.Metadata),
	}

	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type requestScope struct {
	ProviderHint     string   `json:"provider_hint,omitempty"`
	Tenant           string   `json:"tenant,omitempty"`
	User             string   `json:"user,omitempty"`
	AllowedProviders []string `json:"allowed_providers,omitempty"`
	AllowedModels    []string `json:"allowed_models,omitempty"`
}

func buildRequestScope(metadata map[string]string) requestScope {
	return requestScope{
		ProviderHint:     metadata["provider"],
		Tenant:           metadata["tenant"],
		User:             metadata["user"],
		AllowedProviders: normalizedCSV(metadata["auth_allowed_providers"]),
		AllowedModels:    normalizedCSV(metadata["auth_allowed_models"]),
	}
}

func normalizedCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil
	}

	slices.Sort(out)
	return out
}

type entry struct {
	response  *types.ChatResponse
	expiresAt time.Time
}
