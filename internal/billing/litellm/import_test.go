package litellm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
)

func TestParseFiltersAndConvertsSampleFixture(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "sample.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	entries, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Expected to keep 5 of the 11 fixture entries:
	// - openai/gpt-4o-mini (chat, all three prices)
	// - groq/llama-3.1-8b-instant (chat, no cached)
	// - together_ai/meta-llama/... (chat, real together_ai model)
	// - fictional-cloud/super-llm (chat, provider passes through verbatim)
	// - deepseek-chat (deduped: keeps the first one we hit; the
	//   `deepseek/deepseek-chat` duplicate is dropped)
	// Filtered out:
	// - sample_spec (sentinel key)
	// - openai/text-embedding-3-small (mode=embedding)
	// - ollama/llama3 (input cost 0)
	// - openai/gpt-3.5-turbo-free (input cost 0)
	// - together-ai-21.1b-41b (synthetic together_ai pricing-tier bucket,
	//   not a real model — bare key with `together-ai-` prefix)
	// - deepseek/deepseek-chat OR deepseek-chat (whichever loses dedup)
	if got, want := len(entries), 5; got != want {
		t.Fatalf("entries count = %d, want %d (got %+v)", got, want, entries)
	}

	byKey := make(map[string]config.ModelPriceConfig, len(entries))
	for _, e := range entries {
		byKey[e.Provider+"/"+e.Model] = e
	}

	openaiMini, ok := byKey["openai/gpt-4o-mini"]
	if !ok {
		t.Fatalf("missing openai/gpt-4o-mini in entries: %+v", entries)
	}
	// 0.00000015 USD/token * 1e12 = 150_000 micros/Mtok.
	if openaiMini.InputMicrosUSDPerMillionTokens != 150_000 {
		t.Errorf("openai/gpt-4o-mini input = %d, want 150000", openaiMini.InputMicrosUSDPerMillionTokens)
	}
	if openaiMini.OutputMicrosUSDPerMillionTokens != 600_000 {
		t.Errorf("openai/gpt-4o-mini output = %d, want 600000", openaiMini.OutputMicrosUSDPerMillionTokens)
	}
	if openaiMini.CachedInputMicrosUSDPerMillionTokens != 75_000 {
		t.Errorf("openai/gpt-4o-mini cached = %d, want 75000", openaiMini.CachedInputMicrosUSDPerMillionTokens)
	}
	if openaiMini.Source != config.PricebookSourceImported {
		t.Errorf("openai/gpt-4o-mini source = %q, want %q", openaiMini.Source, config.PricebookSourceImported)
	}

	groq, ok := byKey["groq/llama-3.1-8b-instant"]
	if !ok {
		t.Fatalf("missing groq/llama-3.1-8b-instant in entries: %+v", entries)
	}
	if groq.CachedInputMicrosUSDPerMillionTokens != 0 {
		t.Errorf("groq cached = %d, want 0 (not provided in fixture)", groq.CachedInputMicrosUSDPerMillionTokens)
	}

	if _, ok := byKey["together_ai/meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo"]; !ok {
		t.Errorf("missing together_ai/meta-llama/... — provider name passthrough failed (entries=%+v)", entries)
	}

	// Unmapped LiteLLM providers should pass through verbatim. This lets
	// operators with custom providers (PROVIDER_<NAME>_*) get prices for
	// the providers Hecate doesn't have built-ins for (cohere, replicate,
	// fireworks_ai, etc.) without us having to maintain an exhaustive map.
	fictional, ok := byKey["fictional_provider_does_not_exist/super-llm"]
	if !ok {
		t.Fatalf("missing fictional_provider_does_not_exist/super-llm — unmapped provider was dropped instead of passed through (entries=%+v)", entries)
	}
	if fictional.InputMicrosUSDPerMillionTokens != 1_000_000 {
		t.Errorf("fictional input = %d, want 1000000 (0.000001 USD/token × 1e12)", fictional.InputMicrosUSDPerMillionTokens)
	}

	// Deduplication: deepseek ships both `deepseek-chat` (bare) and
	// `deepseek/deepseek-chat` (prefixed) in the upstream JSON. After
	// our prefix-strip both resolve to the same (deepseek, deepseek-
	// chat) tuple. Without the dedupe guard, Go's non-deterministic
	// map iteration would emit two rows with potentially different
	// prices and let the second-write win on apply.
	deepseekCount := 0
	for _, e := range entries {
		if e.Provider == "deepseek" && e.Model == "deepseek-chat" {
			deepseekCount++
		}
	}
	if deepseekCount != 1 {
		t.Errorf("deepseek/deepseek-chat appeared %d times, want 1 (dedupe should collapse the bare/prefixed pair)", deepseekCount)
	}

	// together_ai pricing-tier buckets should be dropped — they're not
	// real model IDs you can route to. The fixture has
	// `together-ai-21.1b-41b` (bare key, together_ai provider, chat
	// mode). It must NOT show up in the parsed output.
	for _, e := range entries {
		if e.Provider == "together_ai" && strings.HasPrefix(e.Model, "together-ai-") {
			t.Errorf("together_ai bucket entry %q leaked through — should have been filtered", e.Model)
		}
	}
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte("not json")); err == nil {
		t.Fatal("Parse() with bad json: expected error, got nil")
	}
}

func TestParseSkipsEntriesWithBlankProvider(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"orphan/model": {"mode": "chat", "input_cost_per_token": 0.001, "output_cost_per_token": 0.002}
	}`)
	entries, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none (no litellm_provider)", entries)
	}
}
