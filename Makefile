SHELL := /bin/sh

GOCACHE_DIR := $(CURDIR)/.gocache

.PHONY: test test-race coverage ui-coverage build run serve dev ui-install ui-dev ui-build ui-test ui-test-e2e

# build produces a single self-contained gateway binary with the UI bundle
# embedded. The UI is built first so //go:embed picks up the real assets;
# without this step the binary still runs but serves a "UI not built"
# fallback page instead of the React app.
build: ui-build
	mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go build -o gateway ./cmd/gateway

test:
	mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go test ./...

test-race:
	mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go test -race ./...

coverage:
	mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go test -coverprofile=coverage.out ./...
	GOCACHE="$(GOCACHE_DIR)" go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html for line-level coverage."

ui-coverage:
	test -d ui/node_modules/@tailwindcss/vite || (echo "UI dependencies are out of date. Run 'make ui-install' first." && exit 1)
	cd ui && bun run test:coverage

run:
	mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go run ./cmd/gateway

# serve runs the pre-built ./gateway binary. It first stops any existing
# process listening on :8080 (the previous run that the operator forgot to
# Ctrl-C) so a stale "address already in use" never blocks a restart.
# It also sources .env so providers configured there are available, matching
# the `make dev` workflow.
serve:
	@test -x ./gateway || (echo "gateway binary not found — run 'make build' first." && exit 1)
	@pid=$$(lsof -ti:8080 2>/dev/null); \
	if [ -n "$$pid" ]; then \
	  echo "stopping existing gateway on :8080 (pid $$pid)"; \
	  kill $$pid; \
	  sleep 0.3; \
	fi
	set -a; \
	[ -f ./.env ] && . ./.env; \
	set +a; \
	./gateway

dev:
	mkdir -p "$(GOCACHE_DIR)"
	set -a; \
	. ./.env; \
	set +a; \
	GOCACHE="$(GOCACHE_DIR)" go run ./cmd/gateway

ui-install:
	cd ui && bun install

ui-dev:
	test -d ui/node_modules/@tailwindcss/vite || (echo "UI dependencies are out of date. Run 'make ui-install' first." && exit 1)
	cd ui && bun run dev

ui-build:
	test -d ui/node_modules/@tailwindcss/vite || (echo "UI dependencies are out of date. Run 'make ui-install' first." && exit 1)
	cd ui && bun run build
	# Vite empties ui/dist before building, which deletes the tracked
	# .gitkeep placeholder. Recreate it so the next `git status` doesn't
	# show the file as deleted and so a future `git clean` still leaves
	# something for //go:embed to grab.
	@touch ui/dist/.gitkeep

ui-test:
	test -d ui/node_modules/@tailwindcss/vite || (echo "UI dependencies are out of date. Run 'make ui-install' first." && exit 1)
	cd ui && bun run test

ui-test-e2e:
	test -d ui/node_modules/@tailwindcss/vite || (echo "UI dependencies are out of date. Run 'make ui-install' first." && exit 1)
	cd ui && bun run test:e2e
