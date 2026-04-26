package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// ResourceOptions describes the resource attributes that identify the running
// process to all telemetry signals (traces, metrics, logs). The same Resource
// is reused across signals so backends correlate them by `service.*` and
// `deployment.environment.name` without operators needing to align labels by
// hand.
type ResourceOptions struct {
	ServiceName       string
	ServiceVersion    string
	ServiceInstanceID string
	DeploymentEnv     string
	ExtraAttributes   []attribute.KeyValue
}

// BuildResource assembles the OTel Resource for this process. Order matters:
// later sources override earlier ones, so the final precedence is:
//
//  1. Built-in detectors (telemetry SDK info, host, process)
//  2. ResourceOptions fields supplied by the caller
//  3. The OTEL_RESOURCE_ATTRIBUTES / OTEL_SERVICE_NAME environment variables
//
// Honoring OTEL_RESOURCE_ATTRIBUTES last lets operators override fields without
// rebuilding the binary, which is the OpenTelemetry convention.
func BuildResource(ctx context.Context, opts ResourceOptions) (*resource.Resource, error) {
	if strings.TrimSpace(opts.ServiceName) == "" {
		opts.ServiceName = ServiceName
	}
	if strings.TrimSpace(opts.ServiceInstanceID) == "" {
		opts.ServiceInstanceID = generateInstanceID()
	}

	attrs := make([]attribute.KeyValue, 0, 4+len(opts.ExtraAttributes))
	attrs = append(attrs, semconv.ServiceName(opts.ServiceName))
	if v := strings.TrimSpace(opts.ServiceVersion); v != "" {
		attrs = append(attrs, semconv.ServiceVersion(v))
	}
	attrs = append(attrs, semconv.ServiceInstanceID(opts.ServiceInstanceID))
	if env := strings.TrimSpace(opts.DeploymentEnv); env != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(env))
	}
	attrs = append(attrs, opts.ExtraAttributes...)

	return resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
	)
}

func generateInstanceID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}
