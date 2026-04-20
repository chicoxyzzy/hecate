package billing

import (
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestStaticPricebookEstimate(t *testing.T) {
	t.Parallel()

	pricebook := NewStaticPricebook(config.ProvidersConfig{
		OpenAICompatible: []config.OpenAICompatibleProviderConfig{
			{Name: "openai", Kind: "cloud"},
		},
	})

	tests := []struct {
		name       string
		provider   string
		model      string
		usage      types.Usage
		wantMicros int64
	}{
		{
			name:     "gpt4o mini prompt and completion",
			provider: "openai",
			model:    "gpt-4o-mini",
			usage: types.Usage{
				PromptTokens:     2000,
				CompletionTokens: 500,
			},
			wantMicros: 600,
		},
		{
			name:     "cached prompt contributes separately",
			provider: "openai",
			model:    "gpt-4.1-mini",
			usage: types.Usage{
				PromptTokens:       1000,
				CompletionTokens:   1000,
				CachedPromptTokens: 1000,
			},
			wantMicros: 2100,
		},
		{
			name:     "dated upstream model falls back to canonical price",
			provider: "openai",
			model:    "gpt-4o-mini-2024-07-18",
			usage: types.Usage{
				PromptTokens:     2000,
				CompletionTokens: 500,
			},
			wantMicros: 600,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := pricebook.Estimate(tt.provider, tt.model, tt.usage)
			if err != nil {
				t.Fatalf("Estimate() error = %v", err)
			}
			if got.TotalMicrosUSD != tt.wantMicros {
				t.Fatalf("Estimate() total = %d, want %d", got.TotalMicrosUSD, tt.wantMicros)
			}
		})
	}
}

func TestStaticPricebookEstimateLocalProviderDefaultsToZero(t *testing.T) {
	t.Parallel()

	pricebook := NewStaticPricebook(config.ProvidersConfig{
		OpenAICompatible: []config.OpenAICompatibleProviderConfig{
			{Name: "local", Kind: "local", DefaultModel: "llama3.1:8b"},
		},
	})

	got, err := pricebook.Estimate("local", "llama3.1:8b", types.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
	})
	if err != nil {
		t.Fatalf("Estimate() error = %v", err)
	}
	if got.TotalMicrosUSD != 0 {
		t.Fatalf("Estimate() total = %d, want 0", got.TotalMicrosUSD)
	}
}
