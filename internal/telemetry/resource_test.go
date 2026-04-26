package telemetry

import (
	"context"
	"strings"
	"testing"
)

func TestBuildResourcePopulatesServiceIdentity(t *testing.T) {
	res, err := BuildResource(context.Background(), ResourceOptions{
		ServiceName:       "hecate-gateway-test",
		ServiceVersion:    "1.2.3",
		ServiceInstanceID: "instance-abc",
		DeploymentEnv:     "staging",
	})
	if err != nil {
		t.Fatalf("BuildResource: %v", err)
	}

	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}

	want := map[string]string{
		"service.name":                "hecate-gateway-test",
		"service.version":             "1.2.3",
		"service.instance.id":         "instance-abc",
		"deployment.environment.name": "staging",
	}
	for key, expected := range want {
		if got[key] != expected {
			t.Errorf("attribute %q = %q, want %q", key, got[key], expected)
		}
	}

	// Built-in detectors must contribute telemetry.sdk.* attributes so
	// backends can identify which SDK produced the data.
	if got["telemetry.sdk.name"] == "" {
		t.Error("expected telemetry.sdk.name to be populated by WithTelemetrySDK detector")
	}
}

func TestBuildResourceGeneratesInstanceIDByDefault(t *testing.T) {
	res, err := BuildResource(context.Background(), ResourceOptions{ServiceName: "hecate"})
	if err != nil {
		t.Fatalf("BuildResource: %v", err)
	}

	for _, kv := range res.Attributes() {
		if string(kv.Key) == "service.instance.id" {
			if v := kv.Value.AsString(); v == "" || strings.ContainsAny(v, " \t\n") {
				t.Errorf("generated service.instance.id is not a valid identifier: %q", v)
			}
			return
		}
	}
	t.Error("service.instance.id not present in resource attributes")
}

func TestBuildResourceFallsBackToDefaultServiceName(t *testing.T) {
	res, err := BuildResource(context.Background(), ResourceOptions{})
	if err != nil {
		t.Fatalf("BuildResource: %v", err)
	}

	for _, kv := range res.Attributes() {
		if string(kv.Key) == "service.name" {
			if got := kv.Value.AsString(); got != ServiceName {
				t.Errorf("service.name = %q, want default %q", got, ServiceName)
			}
			return
		}
	}
	t.Error("service.name attribute missing")
}
