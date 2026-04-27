package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hecate/agent-runtime/internal/controlplane"
)

func TestSystemPromptResolver_AllFourLayersConcatenated(t *testing.T) {
	// Composition order is broadest-first: global, tenant, workspace,
	// per-task. Pinning so a refactor that flips the order is caught.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("Workspace says hi."), 0o644); err != nil {
		t.Fatal(err)
	}
	cp := controlplane.NewMemoryStore()
	if _, err := cp.UpsertTenant(context.Background(), controlplane.Tenant{
		ID:           "team-a",
		Name:         "team-a",
		Enabled:      true,
		SystemPrompt: "Tenant says: be careful.",
	}); err != nil {
		t.Fatalf("UpsertTenant: %v", err)
	}

	resolver := buildSystemPromptResolver("Global default.", cp)
	got := resolver(context.Background(), "team-a", "Per-task override.", dir)

	wantParts := []string{"Global default.", "Tenant says: be careful.", "Workspace says hi.", "Per-task override."}
	for i, p := range wantParts {
		if !strings.Contains(got, p) {
			t.Errorf("layer %d %q missing from composed prompt: %s", i+1, p, got)
		}
	}
	// Ordering: each layer's substring must come strictly before the next.
	prev := -1
	for _, p := range wantParts {
		idx := strings.Index(got, p)
		if idx <= prev {
			t.Errorf("layer %q at idx=%d, want > %d (previous): %s", p, idx, prev, got)
		}
		prev = idx
	}
}

func TestSystemPromptResolver_EmptyLayersSkipped(t *testing.T) {
	// Only the global default exists — output is just that, no
	// stray separators or empty-layer noise.
	resolver := buildSystemPromptResolver("Just global.", controlplane.NewMemoryStore())
	got := resolver(context.Background(), "", "", "")
	if got != "Just global." {
		t.Errorf("got %q, want %q", got, "Just global.")
	}
}

func TestSystemPromptResolver_NoLayersAtAllReturnsEmpty(t *testing.T) {
	// All four layers empty = empty result. Agent loop interprets
	// this as "no system message" rather than emitting a role:system
	// message with empty content.
	resolver := buildSystemPromptResolver("", controlplane.NewMemoryStore())
	got := resolver(context.Background(), "", "", "")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestSystemPromptResolver_PerTaskBeatsTenantBeatsGlobal_ButComposes(t *testing.T) {
	// Composition is concatenation, not override — every non-empty
	// layer contributes. Verifies the tenant prompt and per-task
	// prompt both appear when both are set.
	cp := controlplane.NewMemoryStore()
	_, _ = cp.UpsertTenant(context.Background(), controlplane.Tenant{
		ID: "team-a", Name: "team-a", Enabled: true, SystemPrompt: "TENANT",
	})
	resolver := buildSystemPromptResolver("GLOBAL", cp)
	got := resolver(context.Background(), "team-a", "TASK", "")
	if !strings.Contains(got, "GLOBAL") || !strings.Contains(got, "TENANT") || !strings.Contains(got, "TASK") {
		t.Errorf("composition lost a layer: %q", got)
	}
}

func TestSystemPromptResolver_AGENTS_md_Fallback(t *testing.T) {
	// CLAUDE.md and AGENTS.md are both honored. When CLAUDE.md is
	// absent, AGENTS.md (Codex CLI convention) is read as the
	// workspace layer — no developer needs to switch conventions to
	// use Hecate.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("From AGENTS.md."), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := buildSystemPromptResolver("", controlplane.NewMemoryStore())
	got := resolver(context.Background(), "", "", dir)
	if got != "From AGENTS.md." {
		t.Errorf("got %q, want 'From AGENTS.md.'", got)
	}
}

func TestSystemPromptResolver_CLAUDE_md_TakesPrecedence(t *testing.T) {
	// Both files present → CLAUDE.md wins (it's first in the
	// preference list). Operators can override by deleting the one
	// they don't want.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("From CLAUDE.md."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("From AGENTS.md."), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := buildSystemPromptResolver("", controlplane.NewMemoryStore())
	got := resolver(context.Background(), "", "", dir)
	if !strings.Contains(got, "CLAUDE.md") || strings.Contains(got, "AGENTS.md") {
		t.Errorf("CLAUDE.md should win; got: %q", got)
	}
}

func TestSystemPromptResolver_WorkspaceFileTooLargeIsTruncated(t *testing.T) {
	// A pathologically large CLAUDE.md (or one accidentally
	// containing the whole codebase) must not blow up the prompt
	// budget. Truncate to agentWorkspacePromptMaxBytes.
	dir := t.TempDir()
	huge := strings.Repeat("x", agentWorkspacePromptMaxBytes*2)
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver := buildSystemPromptResolver("", controlplane.NewMemoryStore())
	got := resolver(context.Background(), "", "", dir)
	if len(got) > agentWorkspacePromptMaxBytes {
		t.Errorf("len = %d, want <= %d", len(got), agentWorkspacePromptMaxBytes)
	}
}

func TestSystemPromptResolver_TenantLookupErrorFallsBack(t *testing.T) {
	// Unknown tenant id returns empty for that layer — the rest of
	// the composition still works. Lets the gateway survive a
	// momentary controlplane glitch without failing the run.
	resolver := buildSystemPromptResolver("GLOBAL", controlplane.NewMemoryStore())
	got := resolver(context.Background(), "tenant-that-doesnt-exist", "", "")
	if got != "GLOBAL" {
		t.Errorf("got %q, want 'GLOBAL'", got)
	}
}
