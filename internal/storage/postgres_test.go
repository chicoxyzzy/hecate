package storage

import "testing"

func TestSanitizeIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		fallback string
		want     string
	}{
		{name: "normalizes mixed input", input: " Hecate-Cache.Main ", fallback: "x", want: "hecate_cache_main"},
		{name: "drops unsupported chars", input: "hello@world!", fallback: "x", want: "hello_world"},
		{name: "falls back when empty", input: "   ", fallback: "public", want: "public"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeIdentifier(tt.input, tt.fallback); got != tt.want {
				t.Fatalf("sanitizeIdentifier(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPostgresClientTableNameAndQualifiedTable(t *testing.T) {
	t.Parallel()

	client := &PostgresClient{
		schema:      "runtime",
		tablePrefix: "hecate",
	}

	if got := client.TableName("cache-semantic"); got != "hecate_cache_semantic" {
		t.Fatalf("TableName() = %q, want hecate_cache_semantic", got)
	}
	if got := client.QualifiedTable("cache-semantic"); got != `"runtime"."hecate_cache_semantic"` {
		t.Fatalf("QualifiedTable() = %q", got)
	}
}

func TestPostgresClientNilHelpers(t *testing.T) {
	t.Parallel()

	var client *PostgresClient
	if got := client.DB(); got != nil {
		t.Fatal("DB() = non-nil, want nil")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}
