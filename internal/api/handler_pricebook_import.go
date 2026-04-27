package api

import (
	"context"
	"net/http"
	"time"

	"github.com/hecate/agent-runtime/internal/billing/litellm"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
)

// pricebookImportFetcher is the seam tests use to substitute a fixture
// loader for the real LiteLLM HTTP fetch. Production code calls
// `litellm.Fetch`, which talks to GitHub. Tests reassign this var to
// return a hand-built slice without any network I/O.
var pricebookImportFetcher = func(ctx context.Context) ([]config.ModelPriceConfig, error) {
	return litellm.Fetch(ctx, http.DefaultClient)
}

// HandleControlPlanePricebookImportPreview fetches the upstream LiteLLM
// pricing data, diffs it against the current pricebook, and returns the
// proposed changes without applying anything.
//
// The diff has three buckets:
//   - Added:   imported rows that don't currently exist
//   - Updated: imported rows that would change a current "imported" row's price
//   - Skipped: current "manual" rows that LiteLLM also has — we never overwrite
//     manual edits, so we report them so the UI can explain
//
// Imported rows that exactly match the current pricebook are silently
// counted in Unchanged.
func (h *Handler) HandleControlPlanePricebookImportPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireControlPlane(w, r); !ok {
		return
	}

	diff, err := h.computePricebookImportDiff(r.Context())
	if err != nil {
		WriteError(w, http.StatusBadGateway, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_pricebook_import_diff",
		"data":   diff,
	})
}

// HandleControlPlanePricebookImportApply runs the same fetch+diff as the
// preview handler and then persists the rows it would add or update via
// `controlPlane.UpsertPricebookEntry`. The optional `keys` field in the
// request body restricts the apply to a subset (e.g. just the rows the
// operator checked in the modal). Empty/missing keys means "apply
// everything".
func (h *Handler) HandleControlPlanePricebookImportApply(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req PricebookImportApplyRequest
	// Apply is allowed with an empty body — that's "apply everything". So we
	// only fail on a non-empty body that isn't valid JSON.
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}

	diff, err := h.computePricebookImportDiff(r.Context())
	if err != nil {
		WriteError(w, http.StatusBadGateway, errCodeGatewayError, err.Error())
		return
	}

	keyFilter := pricebookKeyFilter(req.Keys)
	ctx := controlplane.WithActor(r.Context(), controlPlaneActor(principal, r))

	applied := make([]ControlPlanePricebookRecord, 0, len(diff.Added)+len(diff.Updated))
	for _, entry := range diff.Added {
		if !keyFilter.allows(entry.Provider, entry.Model) {
			continue
		}
		saved, upsertErr := h.controlPlane.UpsertPricebookEntry(ctx, modelPriceFromRecord(entry))
		if upsertErr != nil {
			WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, upsertErr.Error())
			return
		}
		applied = append(applied, renderControlPlanePricebookEntry(saved))
	}
	for _, update := range diff.Updated {
		if !keyFilter.allows(update.Entry.Provider, update.Entry.Model) {
			continue
		}
		saved, upsertErr := h.controlPlane.UpsertPricebookEntry(ctx, modelPriceFromRecord(update.Entry))
		if upsertErr != nil {
			WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, upsertErr.Error())
			return
		}
		applied = append(applied, renderControlPlanePricebookEntry(saved))
	}

	out := PricebookImportDiff{
		FetchedAt: diff.FetchedAt,
		Applied:   applied,
		Unchanged: diff.Unchanged,
		Skipped:   diff.Skipped,
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_pricebook_import_diff",
		"data":   out,
	})
}

func (h *Handler) computePricebookImportDiff(ctx context.Context) (PricebookImportDiff, error) {
	imported, err := pricebookImportFetcher(ctx)
	if err != nil {
		return PricebookImportDiff{}, err
	}

	state, err := h.controlPlane.Snapshot(ctx)
	if err != nil {
		return PricebookImportDiff{}, err
	}

	type currentRow struct {
		entry  config.ModelPriceConfig
		source string
	}
	current := make(map[string]currentRow, len(state.Pricebook))
	for _, entry := range state.Pricebook {
		key := pricebookKey(entry.Provider, entry.Model)
		source := entry.Source
		if source == "" {
			source = config.PricebookSourceManual
		}
		current[key] = currentRow{entry: entry, source: source}
	}

	diff := PricebookImportDiff{FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, entry := range imported {
		key := pricebookKey(entry.Provider, entry.Model)
		existing, ok := current[key]
		if !ok {
			diff.Added = append(diff.Added, renderControlPlanePricebookEntry(entry))
			continue
		}
		// Manual rows are operator-protected: an import never overwrites
		// them. The UI surfaces these in a "skipped" list so the operator
		// understands why their LiteLLM-listed model didn't move.
		if existing.source == config.PricebookSourceManual {
			diff.Skipped = append(diff.Skipped, renderControlPlanePricebookEntry(existing.entry))
			continue
		}
		// Imported row already in place — only counts as an update if a
		// price actually changed. Otherwise it's a no-op.
		if pricebookPricesEqual(existing.entry, entry) {
			diff.Unchanged++
			continue
		}
		diff.Updated = append(diff.Updated, PricebookImportUpdateRecord{
			Entry:    renderControlPlanePricebookEntry(entry),
			Previous: renderControlPlanePricebookEntry(existing.entry),
		})
	}
	return diff, nil
}

func pricebookKey(provider, model string) string {
	return provider + "/" + model
}

func pricebookPricesEqual(a, b config.ModelPriceConfig) bool {
	return a.InputMicrosUSDPerMillionTokens == b.InputMicrosUSDPerMillionTokens &&
		a.OutputMicrosUSDPerMillionTokens == b.OutputMicrosUSDPerMillionTokens &&
		a.CachedInputMicrosUSDPerMillionTokens == b.CachedInputMicrosUSDPerMillionTokens
}

func modelPriceFromRecord(r ControlPlanePricebookRecord) config.ModelPriceConfig {
	source := r.Source
	if source == "" {
		source = config.PricebookSourceImported
	}
	return config.ModelPriceConfig{
		Provider:                             r.Provider,
		Model:                                r.Model,
		InputMicrosUSDPerMillionTokens:       r.InputMicrosUSDPerMillionTokens,
		OutputMicrosUSDPerMillionTokens:      r.OutputMicrosUSDPerMillionTokens,
		CachedInputMicrosUSDPerMillionTokens: r.CachedInputMicrosUSDPerMillionTokens,
		Source:                               source,
	}
}

// pricebookKeyFilterSet is a small helper so we don't have to special-case
// "empty filter == everything" at every call site.
type pricebookKeyFilterSet struct {
	keys map[string]struct{}
}

func pricebookKeyFilter(keys []string) pricebookKeyFilterSet {
	if len(keys) == 0 {
		return pricebookKeyFilterSet{}
	}
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		set[k] = struct{}{}
	}
	return pricebookKeyFilterSet{keys: set}
}

func (f pricebookKeyFilterSet) allows(provider, model string) bool {
	if f.keys == nil {
		return true
	}
	_, ok := f.keys[pricebookKey(provider, model)]
	return ok
}
