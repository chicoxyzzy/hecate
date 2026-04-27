package litellm

import (
	"os"
	"path/filepath"
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

	// Expected to keep 3 of the 8 fixture entries:
	// - openai/gpt-4o-mini (chat, all three prices)
	// - groq/llama-3.1-8b-instant (chat, no cached)
	// - together_ai/... (chat, mapped to "together")
	// Filtered out:
	// - sample_spec (sentinel key)
	// - openai/text-embedding-3-small (mode=embedding)
	// - ollama/llama3 (input cost 0)
	// - openai/gpt-3.5-turbo-free (input cost 0)
	// - fictional-cloud/super-llm (provider not in map)
	if got, want := len(entries), 3; got != want {
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

	if _, ok := byKey["together/meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo"]; !ok {
		t.Errorf("missing together/meta-llama/... — provider name remap from together_ai → together failed (entries=%+v)", entries)
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
