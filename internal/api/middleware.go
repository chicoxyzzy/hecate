package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/hecate/agent-runtime/internal/telemetry"
)

type middleware func(http.Handler) http.Handler

func Chain(handler http.Handler, middleware ...middleware) http.Handler {
	wrapped := handler
	for i := len(middleware) - 1; i >= 0; i-- {
		wrapped = middleware[i](wrapped)
	}
	return wrapped
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = newRequestID()
		}

		ctx := telemetry.WithRequestID(r.Context(), requestID)
		w.Header().Set("X-Request-Id", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func LoggingMiddleware(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			telemetry.Info(logger, r.Context(), "http.server.request",
				slog.String("event.name", "http.server.request"),
				slog.String(telemetry.AttrTraceID, rw.Header().Get("X-Trace-Id")),
				slog.String(telemetry.AttrSpanID, rw.Header().Get("X-Span-Id")),
				slog.String("http.request.method", r.Method),
				slog.String("url.path", r.URL.Path),
				slog.Int("http.response.status_code", rw.status),
				slog.Int64(telemetry.AttrHecateHTTPDurationMS, time.Since(start).Milliseconds()),
			)
		})
	}
}

func RecoveryMiddleware(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					telemetry.Error(logger, r.Context(), "http.server.panic",
						slog.String("event.name", "http.server.panic"),
						slog.String("exception.message", stringifyPanic(recovered)),
					)
					WriteError(w, http.StatusInternalServerError, "internal_error", "unexpected server error")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

func RequestIDFromContext(ctx context.Context) string {
	return telemetry.RequestIDFromContext(ctx)
}

func newRequestID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func stringifyPanic(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case error:
		return v.Error()
	default:
		return "panic"
	}
}
