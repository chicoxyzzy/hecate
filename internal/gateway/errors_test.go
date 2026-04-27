package gateway

import (
	"errors"
	"fmt"
	"testing"
)

// TestUserFacingMessage pins the contract that the user-visible error
// string never includes the internal classification prefix. The
// classifications (`client error`, `request denied`) drive HTTP status
// routing — the body should carry only the underlying detail.
func TestUserFacingMessage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "client-error wrap is stripped",
			err:  fmt.Errorf("%w: api key is required for cloud provider anthropic", errClient),
			want: "api key is required for cloud provider anthropic",
		},
		{
			name: "denied wrap is stripped",
			err:  fmt.Errorf("%w: tenant lacks access to provider gemini", errDenied),
			want: "tenant lacks access to provider gemini",
		},
		{
			name: "plain error passes through unchanged",
			err:  errors.New("upstream timeout after 30s"),
			want: "upstream timeout after 30s",
		},
		{
			name: "nil error returns empty",
			err:  nil,
			want: "",
		},
		{
			name: "exact 'client error' (no colon) passes through — only the prefix-with-colon is stripped",
			err:  errClient,
			want: "client error",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := UserFacingMessage(tc.err)
			if got != tc.want {
				t.Errorf("UserFacingMessage(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
