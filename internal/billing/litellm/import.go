// Package litellm imports cloud-LLM token pricing from BerriAI's
// model_prices_and_context_window.json upstream into Hecate's pricebook
// schema. The fetch is intentionally synchronous and stateless: callers
// (the admin import preview/apply handlers) drive the lifecycle.
//
// Attribution: the pricing data fetched by this package is maintained
// by the LiteLLM project (https://github.com/BerriAI/litellm) and
// distributed under the MIT License. We do not vendor a copy of the
// file in the repository or in the published binaries — it's fetched
// at runtime when an operator triggers an import. The MIT copyright
// notice is reproduced in NOTICE.md at the repository root in
// accordance with the license terms.
package litellm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
)

// SourceURL is the canonical LiteLLM pricing data feed. Pinned to `main`
// rather than a tag because LiteLLM doesn't version this file; the JSON
// schema has been stable for years.
const SourceURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// fetchTimeout caps the HTTP fetch so a hung GitHub mirror can't pin an
// admin handler indefinitely.
const fetchTimeout = 30 * time.Second

// providerNameMap rewrites LiteLLM's provider names to the IDs Hecate uses
// in `internal/config/builtin_providers.go`. Anything not in this map is
// dropped silently — Hecate only prices models for providers it knows
// how to route to.
//
// The right-hand side must match a built-in provider ID exactly; the
// `_, ok := config.BuiltInProviderByID(...)` lookup at apply time is the
// final guardrail.
var providerNameMap = map[string]string{
	// One-to-one passthroughs.
	"openai":    "openai",
	"anthropic": "anthropic",
	"groq":      "groq",
	"deepseek":  "deepseek",
	"mistral":   "mistral",
	"xai":       "xai",
	// LiteLLM uses underscored / dashed names for some providers.
	"together_ai": "together",
	// Google's chat/generation models live under several LiteLLM keys.
	// Map the common ones to Hecate's "google" provider preset.
	"gemini":                    "google",
	"google":                    "google",
	"vertex_ai":                 "google",
	"vertex_ai-language-models": "google",
	"vertex_ai-chat-models":     "google",
}

// litellmEntry mirrors the subset of fields we read out of LiteLLM's JSON.
// We deliberately decode into a map first so that unknown / variable-shaped
// fields (e.g. `supports_vision`, `output_cost_per_image`) don't trip the
// strict JSON decoder.
type litellmEntry struct {
	provider           string
	mode               string
	inputCostPerToken  float64
	outputCostPerToken float64
	cachedInputCost    float64
	hasCachedInputCost bool
}

// Fetch retrieves the upstream LiteLLM pricing JSON and converts it to
// Hecate pricebook entries. If httpClient is nil, http.DefaultClient is
// used. A 30 s timeout is enforced via the request context.
func Fetch(ctx context.Context, httpClient *http.Client) ([]config.ModelPriceConfig, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, SourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build litellm fetch request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch litellm pricing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch litellm pricing: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read litellm pricing body: %w", err)
	}
	return Parse(body)
}

// Parse decodes a LiteLLM pricing-data JSON payload into Hecate pricebook
// entries. Entries that aren't chat/completion models, that have no input
// cost, or whose provider doesn't map to a Hecate built-in are dropped.
func Parse(raw []byte) ([]config.ModelPriceConfig, error) {
	var rawEntries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawEntries); err != nil {
		return nil, fmt.Errorf("decode litellm pricing: %w", err)
	}

	out := make([]config.ModelPriceConfig, 0, len(rawEntries))
	for key, valueRaw := range rawEntries {
		// LiteLLM ships a "sample_spec" sentinel in the same map. Skip it.
		if key == "sample_spec" {
			continue
		}
		entry, ok := decodeEntry(valueRaw)
		if !ok {
			continue
		}
		// Filter on mode: Hecate's pricebook is for LLM token usage only.
		// Embeddings, image generation, audio transcription, etc. all have
		// different cost models that don't fit InputMicros/OutputMicros.
		switch entry.mode {
		case "chat", "completion":
			// ok
		default:
			continue
		}
		// Map LiteLLM provider name to Hecate's built-in ID.
		providerID, ok := providerNameMap[entry.provider]
		if !ok {
			continue
		}
		// Belt-and-suspenders: confirm the mapped ID is a real built-in
		// provider so a stale entry in providerNameMap can't smuggle a
		// junk row through.
		if _, ok := config.BuiltInProviderByID(providerID); !ok {
			continue
		}
		// "Free" upstream prices are ambiguous — could mean "we don't
		// know" or "the model is genuinely free." Skip rather than
		// record a bogus zero, since 0 is meaningful in Hecate's schema
		// (used for local providers).
		if entry.inputCostPerToken <= 0 {
			continue
		}
		// Strip the leading provider prefix (e.g. "openai/gpt-4o" → "gpt-4o").
		// Some entries are bare model IDs ("gpt-4o") with no "/" — keep those as-is.
		modelName := key
		if i := strings.Index(key, "/"); i >= 0 {
			modelName = key[i+1:]
		}
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}

		row := config.ModelPriceConfig{
			Provider:                        providerID,
			Model:                           modelName,
			InputMicrosUSDPerMillionTokens:  usdPerTokenToMicrosPerMillion(entry.inputCostPerToken),
			OutputMicrosUSDPerMillionTokens: usdPerTokenToMicrosPerMillion(entry.outputCostPerToken),
			Source:                          config.PricebookSourceImported,
		}
		if entry.hasCachedInputCost {
			row.CachedInputMicrosUSDPerMillionTokens = usdPerTokenToMicrosPerMillion(entry.cachedInputCost)
		}
		out = append(out, row)
	}
	return out, nil
}

// usdPerTokenToMicrosPerMillion converts LiteLLM's USD-per-token figures to
// Hecate's micros-USD-per-million-tokens int64. micros = usd * 1e6, scaled
// per million tokens, so the combined factor is 1e12.
func usdPerTokenToMicrosPerMillion(usdPerToken float64) int64 {
	if usdPerToken <= 0 {
		return 0
	}
	return int64(math.Round(usdPerToken * 1e12))
}

// decodeEntry pulls just the fields we care about out of a LiteLLM record.
// Returns ok=false if the JSON isn't an object or the required scalar
// fields are missing/wrong-typed — both are signals that this row isn't
// a usable model pricing entry.
func decodeEntry(raw json.RawMessage) (litellmEntry, bool) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return litellmEntry{}, false
	}
	provider, _ := obj["litellm_provider"].(string)
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return litellmEntry{}, false
	}
	mode, _ := obj["mode"].(string)
	entry := litellmEntry{
		provider: provider,
		mode:     strings.TrimSpace(mode),
	}
	if v, ok := obj["input_cost_per_token"].(float64); ok {
		entry.inputCostPerToken = v
	}
	if v, ok := obj["output_cost_per_token"].(float64); ok {
		entry.outputCostPerToken = v
	}
	if v, ok := obj["cache_read_input_token_cost"].(float64); ok {
		entry.cachedInputCost = v
		entry.hasCachedInputCost = true
	}
	return entry, true
}
