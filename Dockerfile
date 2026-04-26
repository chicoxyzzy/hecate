# syntax=docker/dockerfile:1.7
#
# Multi-stage build. Three layers:
#   1. ui-builder:  Bun compiles the React operator UI to ui/dist.
#   2. go-builder:  Go compiles cmd/gateway with //go:embed pulling in
#                   ui/dist from the previous stage. Result is one static
#                   binary with the UI embedded.
#   3. runtime:     distroless/static — ~2 MB base, no shell, runs as
#                   non-root. Suitable for production.
#
# Build:   docker build -t hecate:dev .
# Run:     docker run --rm -p 8080:8080 hecate:dev
#
# The runtime image needs no environment to start; it serves the API and
# UI on :8080 immediately. Provider configuration happens through the UI
# or by mounting a .env file into the container.

ARG GO_VERSION=1.26.2
ARG BUN_VERSION=1.3.13

# ── 1. UI build ─────────────────────────────────────────────────────────────

FROM oven/bun:${BUN_VERSION}-alpine AS ui-builder
WORKDIR /app/ui

# Copy lockfile + manifest first so dependency installation caches
# independently of source changes.
COPY ui/package.json ui/bun.lock ./
RUN bun install --frozen-lockfile

COPY ui/ ./
RUN bun run build

# ── 2. Go build ─────────────────────────────────────────────────────────────

FROM golang:${GO_VERSION}-alpine AS go-builder
WORKDIR /src

# Module download caches independently of source.
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download

# The full source must come in before the embed line in embed.go is
# resolved. ui/dist is replaced by the artifacts the previous stage built.
COPY . .
COPY --from=ui-builder /app/ui/dist ./ui/dist

# CGO_ENABLED=0 + -tags netgo + a static link give us a binary distroless
# can run unmodified. -ldflags trim symbols + path info to keep the image
# small and reproducible.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags='-s -w' \
    -o /out/gateway \
    ./cmd/gateway

# ── 3. Runtime ──────────────────────────────────────────────────────────────

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# Copy the static binary. distroless has no package manager, no shell — the
# only file we add is the gateway itself.
COPY --from=go-builder /out/gateway /usr/local/bin/gateway

# /data holds the auto-generated bootstrap secrets (control-plane encryption
# key + admin bearer token) and any file-backed control-plane state. It must
# be writable by the nonroot user; compose mounts a volume here so secrets
# survive container restarts. distroless has no `mkdir`, but VOLUME tells
# Docker to create the path with the container user as owner at first run.
ENV GATEWAY_ADDRESS=:8080 \
    GATEWAY_DATA_DIR=/data
VOLUME ["/data"]

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/gateway"]
