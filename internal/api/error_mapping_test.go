package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestClassifyGatewayError(t *testing.T) {
	priceErr := missingPriceError(t)

	tests := []struct {
		name          string
		err           error
		status        int
		openAIType    string
		anthropicType string
	}{
		{
			name:          "price missing",
			err:           fmt.Errorf("estimate preflight cost: %w", priceErr),
			status:        http.StatusFailedDependency,
			openAIType:    errCodePriceMissing,
			anthropicType: "api_error",
		},
		{
			name: "provider auth failure",
			err: &providers.UpstreamError{
				StatusCode: http.StatusUnauthorized,
				Message:    "Incorrect API key provided",
				Type:       "invalid_request_error",
			},
			status:        http.StatusBadGateway,
			openAIType:    errCodeProviderAuthFailed,
			anthropicType: "authentication_error",
		},
		{
			name: "provider rate limited",
			err: &providers.UpstreamError{
				StatusCode: http.StatusTooManyRequests,
				Message:    "rate limit exceeded",
				Type:       "rate_limit_error",
			},
			status:        http.StatusTooManyRequests,
			openAIType:    errCodeProviderRateLimited,
			anthropicType: "rate_limit_error",
		},
		{
			name: "provider unavailable",
			err: &providers.UpstreamError{
				StatusCode: http.StatusServiceUnavailable,
				Message:    "upstream unavailable",
				Type:       "server_error",
			},
			status:        http.StatusBadGateway,
			openAIType:    errCodeProviderUnavailable,
			anthropicType: "api_error",
		},
		{
			name: "unsupported model from upstream",
			err: &providers.UpstreamError{
				StatusCode: http.StatusNotFound,
				Message:    "The model `llama3.1:8b` does not exist or you do not have access to it.",
				Type:       "invalid_request_error",
			},
			status:        http.StatusBadRequest,
			openAIType:    errCodeUnsupportedModel,
			anthropicType: "invalid_request_error",
		},
		{
			name:          "unsupported model from router",
			err:           errors.New(`route request: provider "ollama" does not support explicit model "qwen2.5:7b"`),
			status:        http.StatusBadRequest,
			openAIType:    errCodeUnsupportedModel,
			anthropicType: "invalid_request_error",
		},
		{
			name:          "route impossible",
			err:           errors.New("route request: no provider available for default routing"),
			status:        http.StatusServiceUnavailable,
			openAIType:    errCodeRouteImpossible,
			anthropicType: "api_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyGatewayError(tt.err)
			if got.Status != tt.status {
				t.Fatalf("status = %d, want %d", got.Status, tt.status)
			}
			if got.OpenAIType != tt.openAIType {
				t.Fatalf("OpenAIType = %q, want %q", got.OpenAIType, tt.openAIType)
			}
			if got.AnthropicType != tt.anthropicType {
				t.Fatalf("AnthropicType = %q, want %q", got.AnthropicType, tt.anthropicType)
			}
			if got.Message == "" {
				t.Fatal("Message is empty")
			}
		})
	}
}

func missingPriceError(t *testing.T) error {
	t.Helper()

	pricebook := billing.NewStaticPricebook(config.ProvidersConfig{
		OpenAICompatible: []config.OpenAICompatibleProviderConfig{{
			Name: "openai",
			Kind: string(providers.KindCloud),
		}},
	}, config.PricebookConfig{})
	_, err := pricebook.Estimate("openai", "unpriced-model", types.Usage{PromptTokens: 1})
	if err == nil {
		t.Fatal("Estimate() error = nil, want missing price")
	}
	if !billing.IsPriceNotFound(err) {
		t.Fatalf("Estimate() error = %v, want price-not-found", err)
	}
	return err
}
