// Package litellm imports cloud-LLM token pricing from BerriAI's
// model_prices_and_context_window.json upstream into Hecate's pricebook
// schema. The fetch is intentionally synchronous and stateless: callers
// (the admin import preview/apply handlers) drive the lifecycle.
//
// Provider names from LiteLLM's `litellm_provider` field are passed
// through verbatim. Hecate's built-in provider IDs are aligned with
// LiteLLM's canonical names (e.g. `gemini`, `together_ai`) so no
// translation is needed. Names that don't match a Hecate built-in still
// pass through — operators may have a custom-configured provider with
// that exact name (PROVIDER_<NAME>_*), or be planning to add one later.
// The import-preview UI flags such rows as `not configured` so the
// operator stays in control of what actually gets persisted.
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
// entries. Entries that aren't chat/completion models or that have no
// input cost are dropped. The `litellm_provider` value is used as the
// Hecate provider ID verbatim — Hecate's built-in IDs match LiteLLM's
// canonical names. Entries whose provider doesn't match a Hecate
// built-in still pass through with the LiteLLM name unchanged: the
// operator may have a custom-configured provider with that name, or
// may add one later. The import-preview UI flags such rows as
// `not configured` so they're never auto-checked.
func Parse(raw []byte) ([]config.ModelPriceConfig, error) {
	var rawEntries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawEntries); err != nil {
		return nil, fmt.Errorf("decode litellm pricing: %w", err)
	}

	out := make([]config.ModelPriceConfig, 0, len(rawEntries))
	// seen tracks (provider, model) tuples we've already emitted so a
	// duplicate LiteLLM key (deepseek ships both `deepseek-chat` and
	// `deepseek/deepseek-chat`, for example) doesn't produce two rows
	// with the same identity but potentially different prices. Go's map
	// iteration is non-deterministic, so without this guard the
	// last-write-wins outcome flickered between runs.
	seen := make(map[string]struct{}, len(rawEntries))
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
		// Use the LiteLLM provider name verbatim. Hecate's built-in IDs
		// are aligned with LiteLLM's canonical names, and unmatched names
		// pass through so operators with custom-configured providers
		// (PROVIDER_<NAME>_*) still get pricing for them.
		providerID := entry.provider
		// "Free" upstream prices are ambiguous — could mean "we don't
		// know" or "the model is genuinely free." Skip rather than
		// record a bogus zero, since 0 is meaningful in Hecate's schema
		// (used for local providers).
		if entry.inputCostPerToken <= 0 {
			continue
		}
		// LiteLLM ships pseudo-entries for `together_ai` that aren't
		// real model IDs but pricing-tier brackets — keys like
		// `together-ai-21.1b-41b`, `together-ai-41.1b-80b`. They have
		// `litellm_provider: "together_ai"` and a chat mode but the
		// "model" can't be sent to Together's API. Skip them so they
		// don't pollute the pricebook with non-routable rows. Heuristic:
		// the bare key starts with `together-ai-` (the bucket pattern)
		// and has no `/` separator, since real Together AI models always
		// use the `together_ai/<vendor>/<model>` form.
		if providerID == "together_ai" && !strings.Contains(key, "/") && strings.HasPrefix(key, "together-ai-") {
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
		dedupKey := providerID + "\x00" + modelName
		if _, dup := seen[dedupKey]; dup {
			continue
		}
		seen[dedupKey] = struct{}{}

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
