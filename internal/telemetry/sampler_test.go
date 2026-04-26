package telemetry

import (
	"strings"
	"testing"
)

func TestBuildSamplerByName(t *testing.T) {
	cases := []struct {
		name        string
		arg         float64
		wantPrefix  string
		description string
	}{
		{"", 0, "ParentBased{root:AlwaysOnSampler", "default"},
		{"always_on", 0, "AlwaysOnSampler", "always_on"},
		{"always_off", 0, "AlwaysOffSampler", "always_off"},
		{"traceidratio", 0.25, "TraceIDRatioBased{0.25}", "traceidratio with 0.25"},
		{"parentbased_always_on", 0, "ParentBased{root:AlwaysOnSampler", "parent always on"},
		{"parentbased_always_off", 0, "ParentBased{root:AlwaysOffSampler", "parent always off"},
		{"parentbased_traceidratio", 0.1, "ParentBased{root:TraceIDRatioBased{0.1}", "parent ratio 0.1"},
		// Unrecognized names fall back to the safe default. We do this rather
		// than erroring because OTEL_TRACES_SAMPLER values evolve and a typo
		// shouldn't take traces down silently in production.
		{"bogus", 0, "ParentBased{root:AlwaysOnSampler", "unknown name fallback"},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			sampler := BuildSampler(tc.name, tc.arg)
			if got := sampler.Description(); !strings.HasPrefix(got, tc.wantPrefix) {
				t.Errorf("BuildSampler(%q, %v).Description() = %q, want prefix %q", tc.name, tc.arg, got, tc.wantPrefix)
			}
		})
	}
}
