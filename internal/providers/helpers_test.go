package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryUnconfigured(t *testing.T) {
	cases := []struct {
		name   string
		kind   Kind
		apiKey string
		want   bool
	}{
		{"cloud without api key", KindCloud, "", true},
		{"cloud with whitespace api key", KindCloud, "   ", true},
		{"cloud with valid api key", KindCloud, "sk-abc", false},
		{"local without key is configured", KindLocal, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := discoveryUnconfigured(tc.kind, tc.apiKey); got != tc.want {
				t.Errorf("discoveryUnconfigured(%s, %q) = %v, want %v", tc.kind, tc.apiKey, got, tc.want)
			}
		})
	}
}

func TestDiscoveryFailureTTL(t *testing.T) {
	t.Run("local connection failure uses short TTL", func(t *testing.T) {
		got := discoveryFailureTTL(KindLocal, errors.New("dial tcp: connection refused"))
		if got != capabilitiesLocalFailureTTL {
			t.Errorf("got %v, want %v (short TTL for local connection refused)", got, capabilitiesLocalFailureTTL)
		}
	})

	t.Run("non-local error uses standard failure TTL", func(t *testing.T) {
		got := discoveryFailureTTL(KindCloud, errors.New("upstream 500"))
		if got != capabilitiesFailureTTL {
			t.Errorf("got %v, want %v", got, capabilitiesFailureTTL)
		}
	})

	t.Run("local but not connection failure uses standard TTL", func(t *testing.T) {
		got := discoveryFailureTTL(KindLocal, errors.New("invalid response"))
		if got != capabilitiesFailureTTL {
			t.Errorf("got %v, want %v", got, capabilitiesFailureTTL)
		}
	})
}

func TestIsLocalConnectionFailure(t *testing.T) {
	if isLocalConnectionFailure(nil) {
		t.Error("nil error → expected false")
	}
	if !isLocalConnectionFailure(errors.New("dial tcp: connection refused")) {
		t.Error("connection refused string should match")
	}
	if isLocalConnectionFailure(errors.New("totally unrelated")) {
		t.Error("unrelated error should not match")
	}
}

func TestClassifyHealthError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"upstream 429", &UpstreamError{StatusCode: http.StatusTooManyRequests}, "rate_limit"},
		{"upstream 500", &UpstreamError{StatusCode: http.StatusInternalServerError}, "server_error"},
		{"upstream 502", &UpstreamError{StatusCode: http.StatusBadGateway}, "server_error"},
		{"upstream 503", &UpstreamError{StatusCode: http.StatusServiceUnavailable}, "server_error"},
		{"upstream 504", &UpstreamError{StatusCode: http.StatusGatewayTimeout}, "server_error"},
		{"upstream 401", &UpstreamError{StatusCode: http.StatusUnauthorized}, "other"},
		{"unrelated error", errors.New("nope"), "other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyHealthError(tc.err); got != tc.want {
				t.Errorf("classifyHealthError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestFormatHealthStateError(t *testing.T) {
	t.Run("empty state returns empty string", func(t *testing.T) {
		state := HealthState{}
		if got := FormatHealthStateError("openai", state); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("only LastError mentions transient failures", func(t *testing.T) {
		state := HealthState{Status: HealthStatusDegraded, LastError: "timeout"}
		got := FormatHealthStateError("openai", state)
		if !strings.Contains(got, "transient failures") {
			t.Errorf("missing 'transient failures': %q", got)
		}
		if !strings.Contains(got, "openai") {
			t.Errorf("missing provider name: %q", got)
		}
	})

	t.Run("only OpenUntil mentions cooling down", func(t *testing.T) {
		state := HealthState{Status: HealthStatusOpen, OpenUntil: time.Now().Add(time.Minute)}
		got := FormatHealthStateError("openai", state)
		if !strings.Contains(got, "cooling down") {
			t.Errorf("missing 'cooling down': %q", got)
		}
	})

	t.Run("both LastError and OpenUntil combine", func(t *testing.T) {
		state := HealthState{Status: HealthStatusOpen, OpenUntil: time.Now().Add(time.Minute), LastError: "boom"}
		got := FormatHealthStateError("openai", state)
		if !strings.Contains(got, "cooling down") || !strings.Contains(got, "boom") {
			t.Errorf("combined message missing parts: %q", got)
		}
	})
}

func TestNormalizeFieldNames(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"trims and dedupes", []string{"  api_key  ", "api_key", "base_url"}, []string{"api_key", "base_url"}},
		{"skips empty", []string{"", "  ", "model"}, []string{"model"}},
		{"preserves first-seen order", []string{"c", "a", "b", "a"}, []string{"c", "a", "b"}},
		{"empty input", nil, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeFieldNames(tc.in)
			if !equalStringSlicesProv(got, tc.want) {
				t.Errorf("normalizeFieldNames(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestPreviewSecret(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"a", "a"},                   // ≤2 chars: passthrough
		{"ab", "ab"},                 // 2 chars: passthrough
		{"abc", "ab...bc"},           // 3-8 chars: 2+...+2
		{"abcdefgh", "ab...gh"},      // 8 chars
		{"abcdefghi", "abcd...fghi"}, // 9+ chars: 4+...+4
		{"sk-1234567890abcdef", "sk-1...cdef"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := previewSecret(tc.in); got != tc.want {
				t.Errorf("previewSecret(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context.DeadlineExceeded", context.DeadlineExceeded, true},
		{"408 Request Timeout", &UpstreamError{StatusCode: http.StatusRequestTimeout}, true},
		{"429 Too Many Requests", &UpstreamError{StatusCode: http.StatusTooManyRequests}, true},
		{"500", &UpstreamError{StatusCode: http.StatusInternalServerError}, true},
		{"502", &UpstreamError{StatusCode: http.StatusBadGateway}, true},
		{"503", &UpstreamError{StatusCode: http.StatusServiceUnavailable}, true},
		{"504", &UpstreamError{StatusCode: http.StatusGatewayTimeout}, true},
		{"401 not retryable", &UpstreamError{StatusCode: http.StatusUnauthorized}, false},
		{"400 not retryable", &UpstreamError{StatusCode: http.StatusBadRequest}, false},
		{"plain error not retryable", errors.New("local logic"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryableError(tc.err); got != tc.want {
				t.Errorf("IsRetryableError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func equalStringSlicesProv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
