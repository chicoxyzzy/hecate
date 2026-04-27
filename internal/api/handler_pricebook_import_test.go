package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/governor"
)

// fakePricebookFetch swaps out the package-level pricebookImportFetcher
// for the duration of t. Tests pass a hand-built slice; the real
// LiteLLM HTTP path is never exercised. Restored via t.Cleanup so other
// tests in the package see the production fetcher.
func fakePricebookFetch(t *testing.T, entries []config.ModelPriceConfig, err error) {
	t.Helper()
	prev := pricebookImportFetcher
	pricebookImportFetcher = func(_ context.Context) ([]config.ModelPriceConfig, error) {
		if err != nil {
			return nil, err
		}
		out := make([]config.ModelPriceConfig, len(entries))
		copy(out, entries)
		return out, nil
	}
	t.Cleanup(func() { pricebookImportFetcher = prev })
}

func newPricebookImportTestHandler(t *testing.T, store controlplane.Store) (apiTestClient, controlplane.Store) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if store == nil {
		store = controlplane.NewMemoryStore()
	}
	handler := newBudgetTestHandlerWithConfig(logger, config.Config{
		Server: config.ServerConfig{AuthToken: "admin-secret"},
	}, governor.NewMemoryBudgetStore(), store)
	return newAPITestClient(t, handler).withBearerToken("admin-secret"), store
}

func TestPricebookImportPreviewClassifiesEntries(t *testing.T) {
	// Not parallel: pricebookImportFetcher is package-global.
	admin, store := newPricebookImportTestHandler(t, nil)

	// Seed: an "imported" row whose price will change in the fetched data,
	// an "imported" row whose price matches (should be unchanged), and a
	// "manual" row that LiteLLM also lists (should land in skipped).
	ctx := context.Background()
	if _, err := store.UpsertPricebookEntry(ctx, config.ModelPriceConfig{
		Provider: "openai", Model: "gpt-4o-mini",
		InputMicrosUSDPerMillionTokens:  100_000, // will change
		OutputMicrosUSDPerMillionTokens: 200_000,
		Source:                          config.PricebookSourceImported,
	}); err != nil {
		t.Fatalf("seed imported row: %v", err)
	}
	if _, err := store.UpsertPricebookEntry(ctx, config.ModelPriceConfig{
		Provider: "openai", Model: "gpt-4o",
		InputMicrosUSDPerMillionTokens:  2_500_000,
		OutputMicrosUSDPerMillionTokens: 10_000_000,
		Source:                          config.PricebookSourceImported,
	}); err != nil {
		t.Fatalf("seed unchanged imported row: %v", err)
	}
	if _, err := store.UpsertPricebookEntry(ctx, config.ModelPriceConfig{
		Provider: "anthropic", Model: "claude-sonnet-4-6",
		InputMicrosUSDPerMillionTokens:  3_000_000,
		OutputMicrosUSDPerMillionTokens: 15_000_000,
		Source:                          config.PricebookSourceManual,
	}); err != nil {
		t.Fatalf("seed manual row: %v", err)
	}

	// Fetcher returns four rows: one update (price change), one unchanged,
	// one skipped (matches manual), one new (added).
	fakePricebookFetch(t, []config.ModelPriceConfig{
		{Provider: "openai", Model: "gpt-4o-mini",
			InputMicrosUSDPerMillionTokens: 150_000, OutputMicrosUSDPerMillionTokens: 600_000,
			Source: config.PricebookSourceImported},
		{Provider: "openai", Model: "gpt-4o",
			InputMicrosUSDPerMillionTokens: 2_500_000, OutputMicrosUSDPerMillionTokens: 10_000_000,
			Source: config.PricebookSourceImported},
		{Provider: "anthropic", Model: "claude-sonnet-4-6",
			InputMicrosUSDPerMillionTokens: 9_999_999, OutputMicrosUSDPerMillionTokens: 9_999_999,
			Source: config.PricebookSourceImported},
		{Provider: "groq", Model: "llama-3.1-8b-instant",
			InputMicrosUSDPerMillionTokens: 50_000, OutputMicrosUSDPerMillionTokens: 80_000,
			Source: config.PricebookSourceImported},
	}, nil)

	recorder := admin.mustRequest("POST", "/admin/control-plane/pricebook/import/preview", "")
	var resp struct {
		Object string              `json:"object"`
		Data   PricebookImportDiff `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview body: %v", err)
	}

	if resp.Object != "control_plane_pricebook_import_diff" {
		t.Errorf("object = %q, want control_plane_pricebook_import_diff", resp.Object)
	}
	if got := len(resp.Data.Added); got != 1 || resp.Data.Added[0].Model != "llama-3.1-8b-instant" {
		t.Errorf("added = %+v, want one entry for llama-3.1-8b-instant", resp.Data.Added)
	}
	if got := len(resp.Data.Updated); got != 1 || resp.Data.Updated[0].Entry.Model != "gpt-4o-mini" {
		t.Errorf("updated = %+v, want one entry for gpt-4o-mini", resp.Data.Updated)
	}
	// Skipped now pairs LiteLLM's proposal (Entry) with the current
	// manual row (Previous), mirroring Updated's shape — the UI uses
	// Entry to render the inline "Import" affordance for manual rows.
	if got := len(resp.Data.Skipped); got != 1 || resp.Data.Skipped[0].Entry.Provider != "anthropic" {
		t.Errorf("skipped = %+v, want one anthropic entry", resp.Data.Skipped)
	}
	if resp.Data.Skipped[0].Previous.InputMicrosUSDPerMillionTokens != 3_000_000 {
		t.Errorf("skipped Previous = %+v, want manual row with input=3000000", resp.Data.Skipped[0].Previous)
	}
	if resp.Data.Skipped[0].Entry.InputMicrosUSDPerMillionTokens != 9_999_999 {
		t.Errorf("skipped Entry = %+v, want LiteLLM proposal with input=9999999", resp.Data.Skipped[0].Entry)
	}
	if resp.Data.Unchanged != 1 {
		t.Errorf("unchanged = %d, want 1", resp.Data.Unchanged)
	}
	if resp.Data.FetchedAt == "" {
		t.Error("fetched_at is empty; want RFC3339 timestamp")
	}

	// Preview must NOT have written anything.
	state, _ := store.Snapshot(ctx)
	for _, entry := range state.Pricebook {
		if entry.Provider == "openai" && entry.Model == "gpt-4o-mini" && entry.InputMicrosUSDPerMillionTokens != 100_000 {
			t.Fatalf("preview mutated store: gpt-4o-mini input = %d, want unchanged 100000", entry.InputMicrosUSDPerMillionTokens)
		}
	}
}

func TestPricebookImportApplyPersistsAddedAndUpdated(t *testing.T) {
	// Not parallel: pricebookImportFetcher is package-global.
	admin, store := newPricebookImportTestHandler(t, nil)

	ctx := context.Background()
	if _, err := store.UpsertPricebookEntry(ctx, config.ModelPriceConfig{
		Provider: "openai", Model: "gpt-4o-mini",
		InputMicrosUSDPerMillionTokens:  100_000,
		OutputMicrosUSDPerMillionTokens: 200_000,
		Source:                          config.PricebookSourceImported,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fakePricebookFetch(t, []config.ModelPriceConfig{
		{Provider: "openai", Model: "gpt-4o-mini",
			InputMicrosUSDPerMillionTokens: 150_000, OutputMicrosUSDPerMillionTokens: 600_000,
			Source: config.PricebookSourceImported},
		{Provider: "groq", Model: "llama-3.1-8b-instant",
			InputMicrosUSDPerMillionTokens: 50_000, OutputMicrosUSDPerMillionTokens: 80_000,
			Source: config.PricebookSourceImported},
	}, nil)

	recorder := admin.mustRequest("POST", "/admin/control-plane/pricebook/import/apply", "")
	var resp struct {
		Object string              `json:"object"`
		Data   PricebookImportDiff `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("decode apply body: %v", err)
	}
	if got := len(resp.Data.Applied); got != 2 {
		t.Fatalf("applied = %d, want 2 (one add + one update); body=%+v", got, resp.Data)
	}

	state, _ := store.Snapshot(ctx)
	var miniInput, llamaInput int64
	for _, entry := range state.Pricebook {
		switch {
		case entry.Provider == "openai" && entry.Model == "gpt-4o-mini":
			miniInput = entry.InputMicrosUSDPerMillionTokens
			if entry.Source != config.PricebookSourceImported {
				t.Errorf("gpt-4o-mini source = %q, want imported", entry.Source)
			}
		case entry.Provider == "groq" && entry.Model == "llama-3.1-8b-instant":
			llamaInput = entry.InputMicrosUSDPerMillionTokens
		}
	}
	if miniInput != 150_000 {
		t.Errorf("gpt-4o-mini input post-apply = %d, want 150000", miniInput)
	}
	if llamaInput != 50_000 {
		t.Errorf("llama input post-apply = %d, want 50000", llamaInput)
	}
}

func TestPricebookImportApplyHonorsKeysFilter(t *testing.T) {
	// Not parallel: pricebookImportFetcher is package-global.
	admin, store := newPricebookImportTestHandler(t, nil)
	ctx := context.Background()

	fakePricebookFetch(t, []config.ModelPriceConfig{
		{Provider: "openai", Model: "gpt-4o-mini",
			InputMicrosUSDPerMillionTokens: 150_000, OutputMicrosUSDPerMillionTokens: 600_000,
			Source: config.PricebookSourceImported},
		{Provider: "groq", Model: "llama-3.1-8b-instant",
			InputMicrosUSDPerMillionTokens: 50_000, OutputMicrosUSDPerMillionTokens: 80_000,
			Source: config.PricebookSourceImported},
	}, nil)

	body := `{"keys":["groq/llama-3.1-8b-instant"]}`
	admin.mustRequest("POST", "/admin/control-plane/pricebook/import/apply", body)

	state, _ := store.Snapshot(ctx)
	var hasOpenAI, hasGroq bool
	for _, entry := range state.Pricebook {
		if entry.Provider == "openai" && entry.Model == "gpt-4o-mini" {
			hasOpenAI = true
		}
		if entry.Provider == "groq" && entry.Model == "llama-3.1-8b-instant" {
			hasGroq = true
		}
	}
	if hasOpenAI {
		t.Errorf("openai/gpt-4o-mini was applied despite not being in keys filter")
	}
	if !hasGroq {
		t.Errorf("groq/llama-3.1-8b-instant should have been applied")
	}
}

// TestPricebookImportApplyPreservesManualRows is the core regression
// guard for the option-A merge strategy: when an "imported" entry from
// LiteLLM has the same (provider, model) as an existing "manual" row,
// apply MUST NOT overwrite it. This is what protects an operator's
// negotiated provider discount from being clobbered by the next import.
//
// The row should also surface in the response's `skipped` list so the
// UI can tell the operator "we left this one alone — delete it first
// if you want LiteLLM's price."
func TestPricebookImportApplyPreservesManualRows(t *testing.T) {
	// Not parallel: pricebookImportFetcher is package-global.
	admin, store := newPricebookImportTestHandler(t, nil)
	ctx := context.Background()

	const negotiated = int64(80_000) // operator's discounted rate
	if _, err := store.UpsertPricebookEntry(ctx, config.ModelPriceConfig{
		Provider: "openai", Model: "gpt-4o-mini",
		InputMicrosUSDPerMillionTokens:  negotiated,
		OutputMicrosUSDPerMillionTokens: 200_000,
		Source:                          config.PricebookSourceManual,
	}); err != nil {
		t.Fatalf("seed manual row: %v", err)
	}

	// LiteLLM offers a different (higher) input price for the same model.
	// Apply must NOT change the persisted manual row.
	fakePricebookFetch(t, []config.ModelPriceConfig{
		{Provider: "openai", Model: "gpt-4o-mini",
			InputMicrosUSDPerMillionTokens:  150_000,
			OutputMicrosUSDPerMillionTokens: 600_000,
			Source:                          config.PricebookSourceImported},
	}, nil)

	recorder := admin.mustRequest("POST", "/admin/control-plane/pricebook/import/apply", "")
	var resp struct {
		Object string              `json:"object"`
		Data   PricebookImportDiff `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("decode apply body: %v", err)
	}

	// The manual row must appear in `skipped` and not in `applied`.
	if got := len(resp.Data.Applied); got != 0 {
		t.Errorf("applied = %d, want 0 (manual row must not be touched); body=%+v", got, resp.Data)
	}
	foundSkipped := false
	for _, entry := range resp.Data.Skipped {
		if entry.Entry.Provider == "openai" && entry.Entry.Model == "gpt-4o-mini" {
			foundSkipped = true
			break
		}
	}
	if !foundSkipped {
		t.Errorf("manual openai/gpt-4o-mini missing from `skipped` list; body=%+v", resp.Data)
	}

	// Storage check: the negotiated price stays put, source is still manual.
	state, _ := store.Snapshot(ctx)
	for _, entry := range state.Pricebook {
		if entry.Provider == "openai" && entry.Model == "gpt-4o-mini" {
			if entry.InputMicrosUSDPerMillionTokens != negotiated {
				t.Errorf("manual row clobbered: input = %d, want %d (negotiated)",
					entry.InputMicrosUSDPerMillionTokens, negotiated)
			}
			if entry.Source != config.PricebookSourceManual {
				t.Errorf("manual row source flipped to %q, want manual", entry.Source)
			}
		}
	}
}

// TestPricebookImportApplyOverwritesManualWhenKeyExplicit verifies that
// the operator can opt in to replacing a manual row with LiteLLM's
// price by passing the row's key in the apply request. Blanket apply
// (covered by TestPricebookImportApplyPreservesManualRows) still leaves
// manual rows alone — this is the explicit-consent escape hatch the
// pricebook UI uses for the "Replace manual" section in the consent
// dialog.
func TestPricebookImportApplyOverwritesManualWhenKeyExplicit(t *testing.T) {
	admin, store := newPricebookImportTestHandler(t, nil)
	ctx := context.Background()

	const negotiated = int64(80_000)
	if _, err := store.UpsertPricebookEntry(ctx, config.ModelPriceConfig{
		Provider: "openai", Model: "gpt-4o-mini",
		InputMicrosUSDPerMillionTokens:  negotiated,
		OutputMicrosUSDPerMillionTokens: 200_000,
		Source:                          config.PricebookSourceManual,
	}); err != nil {
		t.Fatalf("seed manual row: %v", err)
	}

	fakePricebookFetch(t, []config.ModelPriceConfig{
		{Provider: "openai", Model: "gpt-4o-mini",
			InputMicrosUSDPerMillionTokens:  150_000,
			OutputMicrosUSDPerMillionTokens: 600_000,
			Source:                          config.PricebookSourceImported},
	}, nil)

	// Explicit key list — operator opts in to overwriting the manual row.
	body := `{"keys":["openai/gpt-4o-mini"]}`
	recorder := admin.mustRequest("POST", "/admin/control-plane/pricebook/import/apply", body)
	var resp struct {
		Object string              `json:"object"`
		Data   PricebookImportDiff `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("decode apply body: %v", err)
	}

	// Applied must contain the row; storage now reflects LiteLLM's price
	// and the source has flipped to imported.
	if len(resp.Data.Applied) != 1 {
		t.Fatalf("applied = %+v, want 1 entry", resp.Data.Applied)
	}
	if resp.Data.Applied[0].InputMicrosUSDPerMillionTokens != 150_000 {
		t.Errorf("applied input = %d, want 150000", resp.Data.Applied[0].InputMicrosUSDPerMillionTokens)
	}
	state, _ := store.Snapshot(ctx)
	for _, entry := range state.Pricebook {
		if entry.Provider == "openai" && entry.Model == "gpt-4o-mini" {
			if entry.InputMicrosUSDPerMillionTokens != 150_000 {
				t.Errorf("manual row not overwritten: input = %d, want 150000", entry.InputMicrosUSDPerMillionTokens)
			}
			if entry.Source != config.PricebookSourceImported {
				t.Errorf("source = %q, want imported (explicit override flips source)", entry.Source)
			}
		}
	}
}

// TestPricebookImportPreviewSortsAlphabetically guards the wire-level
// sort: each section (Added, Updated, Skipped) must come back ordered
// by `provider/model` so the import-preview modal renders a stable,
// scannable list. Without sort, Go's non-deterministic map iteration
// shuffles the rows on every preview call.
func TestPricebookImportPreviewSortsAlphabetically(t *testing.T) {
	// Not parallel: pricebookImportFetcher is package-global.
	admin, store := newPricebookImportTestHandler(t, nil)
	ctx := context.Background()

	// Seed an "imported" row so the corresponding fetched entry classifies
	// as Updated (not Added) and a "manual" row so its fetched twin lands
	// in Skipped.
	for _, seed := range []config.ModelPriceConfig{
		{Provider: "openai", Model: "gpt-4o-mini",
			InputMicrosUSDPerMillionTokens: 100, OutputMicrosUSDPerMillionTokens: 200,
			Source: config.PricebookSourceImported},
		{Provider: "anthropic", Model: "claude-haiku-4-5",
			InputMicrosUSDPerMillionTokens: 800, OutputMicrosUSDPerMillionTokens: 4000,
			Source: config.PricebookSourceManual},
	} {
		if _, err := store.UpsertPricebookEntry(ctx, seed); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Fetch entries deliberately ordered NON-alphabetically so the test
	// would fail without an explicit sort step in the handler.
	fakePricebookFetch(t, []config.ModelPriceConfig{
		// Added section
		{Provider: "xai", Model: "grok-3", InputMicrosUSDPerMillionTokens: 1, OutputMicrosUSDPerMillionTokens: 2,
			Source: config.PricebookSourceImported},
		{Provider: "groq", Model: "llama-3.1-8b-instant", InputMicrosUSDPerMillionTokens: 50, OutputMicrosUSDPerMillionTokens: 80,
			Source: config.PricebookSourceImported},
		{Provider: "anthropic", Model: "claude-opus-4-7", InputMicrosUSDPerMillionTokens: 200, OutputMicrosUSDPerMillionTokens: 1000,
			Source: config.PricebookSourceImported},
		// Updated section: same identity as the imported seed but
		// different prices, so it classifies as Updated.
		{Provider: "openai", Model: "gpt-4o-mini", InputMicrosUSDPerMillionTokens: 150, OutputMicrosUSDPerMillionTokens: 600,
			Source: config.PricebookSourceImported},
		// Skipped section: same identity as the manual seed.
		{Provider: "anthropic", Model: "claude-haiku-4-5", InputMicrosUSDPerMillionTokens: 1000, OutputMicrosUSDPerMillionTokens: 5000,
			Source: config.PricebookSourceImported},
	}, nil)

	recorder := admin.mustRequest("POST", "/admin/control-plane/pricebook/import/preview", "")
	var resp struct {
		Object string              `json:"object"`
		Data   PricebookImportDiff `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview body: %v", err)
	}

	addedKeys := make([]string, 0, len(resp.Data.Added))
	for _, e := range resp.Data.Added {
		addedKeys = append(addedKeys, e.Provider+"/"+e.Model)
	}
	wantAdded := []string{"anthropic/claude-opus-4-7", "groq/llama-3.1-8b-instant", "xai/grok-3"}
	for i, want := range wantAdded {
		if i >= len(addedKeys) || addedKeys[i] != want {
			t.Errorf("added[%d] = %q, want %q (full added=%v)", i, safeIndex(addedKeys, i), want, addedKeys)
		}
	}

	if len(resp.Data.Updated) != 1 || resp.Data.Updated[0].Entry.Provider != "openai" {
		t.Errorf("updated section unexpected: %+v", resp.Data.Updated)
	}
	if len(resp.Data.Skipped) != 1 || resp.Data.Skipped[0].Entry.Provider != "anthropic" {
		t.Errorf("skipped section unexpected: %+v", resp.Data.Skipped)
	}
}

// safeIndex returns "" instead of panicking when i is out of range, so
// a too-short list still produces a useful test failure message.
func safeIndex(s []string, i int) string {
	if i < 0 || i >= len(s) {
		return "<missing>"
	}
	return s[i]
}

func TestPricebookImportPreviewRejectsAnonymous(t *testing.T) {
	// Not parallel: pricebookImportFetcher is package-global.
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	store := controlplane.NewMemoryStore()
	handler := newBudgetTestHandlerWithConfig(logger, config.Config{
		Server: config.ServerConfig{AuthToken: "admin-secret"},
	}, governor.NewMemoryBudgetStore(), store)

	fakePricebookFetch(t, nil, nil)

	req := httptest.NewRequest("POST", "/admin/control-plane/pricebook/import/preview", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401 (no bearer); body=%s", rec.Code, rec.Body.String())
	}
}
