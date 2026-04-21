package gateway

import (
	"errors"
	"testing"
)

func TestTraceErrorAttrsIncludesOTelShapedErrorFields(t *testing.T) {
	t.Parallel()

	attrs := traceErrorAttrs("provider", errorKindProviderCallFailed, errors.New("boom"), map[string]any{
		"gen_ai.provider.name": "openai",
	})

	if got := attrs["hecate.phase"]; got != "provider" {
		t.Fatalf("hecate.phase = %v, want provider", got)
	}
	if got := attrs["hecate.error.kind"]; got != errorKindProviderCallFailed {
		t.Fatalf("hecate.error.kind = %v, want %q", got, errorKindProviderCallFailed)
	}
	if got := attrs["error.type"]; got != errorKindProviderCallFailed {
		t.Fatalf("error.type = %v, want %q", got, errorKindProviderCallFailed)
	}
	if got := attrs["error.message"]; got != "boom" {
		t.Fatalf("error.message = %v, want boom", got)
	}
	if got := attrs["gen_ai.provider.name"]; got != "openai" {
		t.Fatalf("gen_ai.provider.name = %v, want openai", got)
	}
}
