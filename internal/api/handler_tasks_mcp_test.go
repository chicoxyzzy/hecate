package api

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/hecate/agent-runtime/internal/secrets"
	"github.com/hecate/agent-runtime/pkg/types"
)

func newTestCipherForAPI(t *testing.T) secrets.Cipher {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), 32))
	c, err := secrets.NewAESGCMCipher(key)
	if err != nil {
		t.Fatalf("newTestCipherForAPI: %v", err)
	}
	return c
}

// TestIsMCPEnvRef pins the reference-detection helper used by both the
// storage path (encrypt-or-skip) and the render path (redact-or-show).
func TestIsMCPEnvRef(t *testing.T) {
	valid := []string{"$GITHUB_TOKEN", "$A", "$_UNDER", "$tok123"}
	for _, v := range valid {
		if !isMCPEnvRef(v) {
			t.Errorf("isMCPEnvRef(%q) = false, want true", v)
		}
	}
	invalid := []string{"", "$", "$1BAD", "$foo-bar", "LITERAL", "enc:x"}
	for _, v := range invalid {
		if isMCPEnvRef(v) {
			t.Errorf("isMCPEnvRef(%q) = true, want false", v)
		}
	}
}

// TestNormalizeMCPServerConfigs_Validation: structural errors (empty
// name/command, duplicates) are caught before any crypto work.
func TestNormalizeMCPServerConfigs_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		desc    string
		items   []MCPServerConfigItem
		wantErr string
	}{
		{
			desc:    "empty name",
			items:   []MCPServerConfigItem{{Name: "", Command: "npx"}},
			wantErr: "name is required",
		},
		{
			desc:    "empty command and url",
			items:   []MCPServerConfigItem{{Name: "fs", Command: ""}},
			wantErr: "either command or url is required",
		},
		{
			desc: "duplicate name",
			items: []MCPServerConfigItem{
				{Name: "fs", Command: "npx"},
				{Name: "fs", Command: "uvx"},
			},
			wantErr: "duplicate name",
		},
		{
			desc: "invalid approval_policy value",
			items: []MCPServerConfigItem{
				{Name: "gh", Command: "npx", ApprovalPolicy: "yolo"},
			},
			wantErr: "approval_policy",
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := normalizeMCPServerConfigs(tc.items, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want %q", err, tc.wantErr)
			}
		})
	}
}

// TestNormalizeMCPServerConfigs_RefPassesThrough: $VAR_NAME values are
// stored verbatim regardless of whether a cipher is configured.
func TestNormalizeMCPServerConfigs_RefPassesThrough(t *testing.T) {
	t.Parallel()
	for _, cipher := range []secrets.Cipher{nil, newTestCipherForAPI(t)} {
		items := []MCPServerConfigItem{
			{Name: "gh", Command: "npx", Env: map[string]string{"TOKEN": "$GITHUB_TOKEN"}},
		}
		out, err := normalizeMCPServerConfigs(items, cipher)
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if got := out[0].Env["TOKEN"]; got != "$GITHUB_TOKEN" {
			t.Errorf("cipher=%v: TOKEN = %q, want $GITHUB_TOKEN (ref must not be altered)", cipher, got)
		}
	}
}

// TestNormalizeMCPServerConfigs_NoCipherLiteralStoredAsIs: when cipher
// is nil, a literal value passes through unchanged so deploys that
// haven't configured a key continue to work.
func TestNormalizeMCPServerConfigs_NoCipherLiteralStoredAsIs(t *testing.T) {
	t.Parallel()
	items := []MCPServerConfigItem{
		{Name: "gh", Command: "npx", Env: map[string]string{"TOKEN": "plaintext-secret"}},
	}
	out, err := normalizeMCPServerConfigs(items, nil)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got := out[0].Env["TOKEN"]; got != "plaintext-secret" {
		t.Errorf("TOKEN = %q, want literal passthrough when cipher nil", got)
	}
}

// TestNormalizeMCPServerConfigs_CipherEncryptsLiteral: when a cipher is
// available, literal values are stored as "enc:<base64>" so the
// plaintext token never sits in the task blob.
func TestNormalizeMCPServerConfigs_CipherEncryptsLiteral(t *testing.T) {
	t.Parallel()
	cipher := newTestCipherForAPI(t)
	items := []MCPServerConfigItem{
		{Name: "gh", Command: "npx", Env: map[string]string{"TOKEN": "my-plaintext-token"}},
	}
	out, err := normalizeMCPServerConfigs(items, cipher)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	stored := out[0].Env["TOKEN"]
	if !strings.HasPrefix(stored, types.MCPEnvEncPrefix) {
		t.Fatalf("TOKEN = %q, want enc: prefix", stored)
	}
	// Decrypt and verify round-trip.
	plaintext, err := cipher.Decrypt(stored[len(types.MCPEnvEncPrefix):])
	if err != nil {
		t.Fatalf("decrypt stored value: %v", err)
	}
	if plaintext != "my-plaintext-token" {
		t.Errorf("round-trip plaintext = %q, want %q", plaintext, "my-plaintext-token")
	}
}

// TestNormalizeMCPServerConfigs_AlreadyEncryptedPassesThrough: idempotent
// — re-creating a task from an already-stored config must not double-encrypt.
func TestNormalizeMCPServerConfigs_AlreadyEncryptedPassesThrough(t *testing.T) {
	t.Parallel()
	cipher := newTestCipherForAPI(t)
	ct, _ := cipher.Encrypt("secret")
	already := types.MCPEnvEncPrefix + ct

	items := []MCPServerConfigItem{
		{Name: "gh", Command: "npx", Env: map[string]string{"TOKEN": already}},
	}
	out, err := normalizeMCPServerConfigs(items, cipher)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got := out[0].Env["TOKEN"]; got != already {
		t.Errorf("TOKEN = %q, want already-encrypted value unchanged", got)
	}
}

// TestRenderMCPServerConfigs_RefsShownVerbatim: $VAR_NAME references are
// safe to return in API responses — they name a variable, not a secret.
func TestRenderMCPServerConfigs_RefsShownVerbatim(t *testing.T) {
	t.Parallel()
	configs := []types.MCPServerConfig{
		{Name: "gh", Command: "npx", Env: map[string]string{"TOKEN": "$GITHUB_TOKEN"}},
	}
	out := renderMCPServerConfigs(configs)
	if got := out[0].Env["TOKEN"]; got != "$GITHUB_TOKEN" {
		t.Errorf("TOKEN = %q, want $GITHUB_TOKEN (ref must be shown)", got)
	}
}

// TestRenderMCPServerConfigs_EncryptedValueRedacted: enc: ciphertext must
// not leak through the task API — it becomes "[redacted]".
func TestRenderMCPServerConfigs_EncryptedValueRedacted(t *testing.T) {
	t.Parallel()
	configs := []types.MCPServerConfig{
		{Name: "gh", Command: "npx", Env: map[string]string{"TOKEN": types.MCPEnvEncPrefix + "someciphertext="}},
	}
	out := renderMCPServerConfigs(configs)
	if got := out[0].Env["TOKEN"]; got != "[redacted]" {
		t.Errorf("TOKEN = %q, want [redacted]", got)
	}
}

// TestRenderMCPServerConfigs_LiteralRedacted: bare literal values (stored
// when no cipher was configured) are also hidden, not returned to callers.
func TestRenderMCPServerConfigs_LiteralRedacted(t *testing.T) {
	t.Parallel()
	configs := []types.MCPServerConfig{
		{Name: "gh", Command: "npx", Env: map[string]string{"TOKEN": "plaintext-leaked"}},
	}
	out := renderMCPServerConfigs(configs)
	if got := out[0].Env["TOKEN"]; got != "[redacted]" {
		t.Errorf("TOKEN = %q, want [redacted] for literal values", got)
	}
}

// TestRenderMCPServerConfigs_EmptyEnvOmitted: no Env map on the item
// when the config has no env — avoids a non-nil empty map in the JSON.
func TestRenderMCPServerConfigs_EmptyEnvOmitted(t *testing.T) {
	t.Parallel()
	configs := []types.MCPServerConfig{{Name: "fs", Command: "uvx"}}
	out := renderMCPServerConfigs(configs)
	if out[0].Env != nil {
		t.Errorf("Env = %v, want nil for config with no env", out[0].Env)
	}
}

// TestNormalizeMCPServerConfigs_ApprovalPolicyAccepted: every recognized
// policy value (and the empty default) round-trips through normalize.
func TestNormalizeMCPServerConfigs_ApprovalPolicyAccepted(t *testing.T) {
	t.Parallel()
	cases := []string{"", types.MCPApprovalAuto, types.MCPApprovalRequireApproval, types.MCPApprovalBlock}
	for _, policy := range cases {
		t.Run("policy="+policy, func(t *testing.T) {
			items := []MCPServerConfigItem{
				{Name: "gh", Command: "npx", ApprovalPolicy: policy},
			}
			out, err := normalizeMCPServerConfigs(items, nil)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if got := out[0].ApprovalPolicy; got != policy {
				t.Errorf("ApprovalPolicy = %q, want %q", got, policy)
			}
		})
	}
}

// TestRenderMCPServerConfigs_ApprovalPolicyShownVerbatim: the policy is
// not a secret — it appears in API responses unchanged so the UI can
// render the operator's chosen gate accurately.
func TestRenderMCPServerConfigs_ApprovalPolicyShownVerbatim(t *testing.T) {
	t.Parallel()
	configs := []types.MCPServerConfig{
		{Name: "gh", Command: "npx", ApprovalPolicy: types.MCPApprovalRequireApproval},
	}
	out := renderMCPServerConfigs(configs)
	if got := out[0].ApprovalPolicy; got != types.MCPApprovalRequireApproval {
		t.Errorf("ApprovalPolicy = %q, want %q", got, types.MCPApprovalRequireApproval)
	}
}
