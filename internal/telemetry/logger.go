package telemetry

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

const ServiceName = "hecate-gateway"

type contextKey string

const (
	requestIDContextKey contextKey = "telemetry.request_id"
	principalContextKey contextKey = "telemetry.principal"
	traceIDsContextKey  contextKey = "telemetry.trace_ids"
)

type Principal struct {
	Name     string
	Role     string
	TenantID string
	Source   string
	KeyID    string
}

type TraceIDs struct {
	TraceID string
	SpanID  string
}

func NewLogger(level string) *slog.Logger {
	options := &slog.HandlerOptions{Level: parseLevel(level)}
	return slog.New(slog.NewJSONHandler(os.Stdout, options)).With(
		slog.String("service.name", ServiceName),
	)
}

func parseLevel(level string) slog.Level {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if strings.TrimSpace(requestID) == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey).(string)
	return requestID
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey).(Principal)
	return principal, ok
}

func WithTraceIDs(ctx context.Context, traceID, spanID string) context.Context {
	if strings.TrimSpace(traceID) == "" && strings.TrimSpace(spanID) == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDsContextKey, TraceIDs{
		TraceID: strings.TrimSpace(traceID),
		SpanID:  strings.TrimSpace(spanID),
	})
}

func TraceIDsFromContext(ctx context.Context) TraceIDs {
	if ids, ok := ctx.Value(traceIDsContextKey).(TraceIDs); ok {
		return ids
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return TraceIDs{}
	}
	return TraceIDs{
		TraceID: spanCtx.TraceID().String(),
		SpanID:  spanCtx.SpanID().String(),
	}
}

func ContextAttrs(ctx context.Context) []slog.Attr {
	attrs := make([]slog.Attr, 0, 8)
	if requestID := RequestIDFromContext(ctx); requestID != "" {
		attrs = append(attrs, slog.String("request.id", requestID))
	}
	traceIDs := TraceIDsFromContext(ctx)
	if traceIDs.TraceID != "" {
		attrs = append(attrs, slog.String("trace_id", traceIDs.TraceID))
	}
	if traceIDs.SpanID != "" {
		attrs = append(attrs, slog.String("span_id", traceIDs.SpanID))
	}
	if principal, ok := PrincipalFromContext(ctx); ok {
		if principal.Name != "" {
			attrs = append(attrs, slog.String("enduser.id", principal.Name))
		}
		if principal.TenantID != "" {
			attrs = append(attrs, slog.String("tenant.id", principal.TenantID))
		}
		if principal.Role != "" {
			attrs = append(attrs, slog.String("hecate.auth.role", principal.Role))
		}
		if principal.Source != "" {
			attrs = append(attrs, slog.String("hecate.auth.source", principal.Source))
		}
		if principal.KeyID != "" {
			attrs = append(attrs, slog.String("hecate.auth.key_id", principal.KeyID))
		}
	}
	return attrs
}

func Info(logger *slog.Logger, ctx context.Context, msg string, attrs ...slog.Attr) {
	logger.LogAttrs(ctx, slog.LevelInfo, msg, append(ContextAttrs(ctx), attrs...)...)
}

func Warn(logger *slog.Logger, ctx context.Context, msg string, attrs ...slog.Attr) {
	logger.LogAttrs(ctx, slog.LevelWarn, msg, append(ContextAttrs(ctx), attrs...)...)
}

func Error(logger *slog.Logger, ctx context.Context, msg string, attrs ...slog.Attr) {
	logger.LogAttrs(ctx, slog.LevelError, msg, append(ContextAttrs(ctx), attrs...)...)
}
