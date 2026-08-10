package codeintel

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatResultEnforcesCompleteUTF8OutputBudget(t *testing.T) {
	items := make([]Item, absoluteMaxResults)
	for index := range items {
		items[index] = Item{
			Path:    strings.Repeat("nested/", 300) + "source.go",
			Detail:  strings.Repeat("🙂 detail ", 500),
			Preview: strings.Repeat("🙂 preview ", 100),
		}
	}
	text := formatResult(Result{
		Operation: OpWorkspaceSymbols,
		Provider:  "fixture",
		Items:     items,
		Truncated: true,
	})
	if len(text) > maxResultTextBytes {
		t.Fatalf("formatted result bytes = %d, want at most %d", len(text), maxResultTextBytes)
	}
	if !utf8.ValidString(text) || !strings.HasSuffix(text, "... output truncated") {
		t.Fatalf("formatted result must end with a valid UTF-8 truncation marker")
	}
}

func TestFormatCapabilitiesIncludesVersionOperationsAndVerificationStage(t *testing.T) {
	text := formatResult(Result{
		Operation: OpCapabilities,
		Capabilities: []Capability{{
			Language:   "go",
			Provider:   "gopls",
			Version:    "0.20.0",
			Available:  true,
			Status:     "installed_unverified",
			Operations: []Operation{OpDefinition, OpReferences},
			Detail:     "trusted executable and version probe passed; LSP initialization is verified on query",
		}},
	})
	for _, want := range []string{
		"go: available via gopls version=0.20.0",
		"[installed_unverified]",
		"operations=definition,references",
		"LSP initialization is verified on query",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("formatted capabilities = %q, want %q", text, want)
		}
	}
}

func TestFormatCapabilitiesMakesUnavailableProvidersNonActionable(t *testing.T) {
	text := formatResult(Result{
		Operation: OpCapabilities,
		Capabilities: []Capability{
			{
				Language:   "go",
				Provider:   "gopls",
				Available:  false,
				Status:     "unavailable",
				Operations: []Operation{OpDefinition, OpReferences},
				Detail:     "not found on a trusted global PATH",
			},
			{
				Language:   "structural",
				Provider:   "ast-grep",
				Available:  false,
				Status:     "unavailable",
				Operations: []Operation{OpStructuralSearch},
				Detail:     "not found on a trusted global PATH",
			},
		},
	})
	for _, forbidden := range []string{"operations=definition", "operations=structural_search"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unavailable capability output = %q, must omit actionable %q", text, forbidden)
		}
	}
	for _, want := range []string{
		"go: unavailable via gopls [unavailable]",
		"Do not call semantic operations for this language",
		"structural: unavailable via ast-grep [unavailable]",
		"Do not call `code_intelligence` with `operation=structural_search`; use `grep`",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("unavailable capability output = %q, want %q", text, want)
		}
	}
}
