package models

import "testing"

func TestCanonicalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "base alias remains unchanged",
			input: "gpt-4o-mini",
			want:  "gpt-4o-mini",
		},
		{
			name:  "dated version falls back to canonical alias",
			input: "gpt-4o-mini-2024-07-18",
			want:  "gpt-4o-mini",
		},
		{
			name:  "dated 4.1 model falls back to canonical alias",
			input: "gpt-4.1-mini-2025-02-11",
			want:  "gpt-4.1-mini",
		},
		{
			name:  "non dated suffix remains unchanged",
			input: "gpt-4o-realtime-preview",
			want:  "gpt-4o-realtime-preview",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Canonicalize(tt.input)
			if got != tt.want {
				t.Fatalf("Canonicalize() = %q, want %q", got, tt.want)
			}
		})
	}
}
