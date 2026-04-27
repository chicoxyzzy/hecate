SHELL := /bin/sh

GOCACHE_DIR := $(CURDIR)/.gocache

.PHONY: test test-race coverage ui-coverage build run serve dev ui-install ui-dev ui-build ui-test ui-test-e2e test-docker-smoke reset-dev reset-docker

# build produces a single self-contained hecate binary with the UI bundle
# embedded. The UI is built first so //go:embed picks up the real assets;
# without this step the binary still runs but serves a "UI not built"
# fallback page instead of the React app.
build: ui-build
	mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go build -o hecate ./cmd/hecate

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
	GOCACHE="$(GOCACHE_DIR)" go run ./cmd/hecate

# serve runs the pre-built ./hecate binary. It first stops any existing
# process listening on :8080 (the previous run that the operator forgot to
# Ctrl-C) so a stale "address already in use" never blocks a restart.
# It also sources .env so providers configured there are available, matching
# the `make dev` workflow.
serve:
	@test -x ./hecate || (echo "hecate binary not found — run 'make build' first." && exit 1)
	@pid=$$(lsof -ti:8080 2>/dev/null); \
	if [ -n "$$pid" ]; then \
	  echo "stopping existing hecate on :8080 (pid $$pid)"; \
	  kill $$pid; \
	  sleep 0.3; \
	fi
	set -a; \
	[ -f ./.env ] && . ./.env; \
	set +a; \
	./hecate

dev:
	mkdir -p "$(GOCACHE_DIR)"
	set -a; \
	. ./.env; \
	set +a; \
	GOCACHE="$(GOCACHE_DIR)" go run ./cmd/hecate

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

# test-docker-smoke spins up `docker compose` with the production image
# and verifies /healthz, /v1/models auth, and the bootstrap volume round
# trip. Runs against a separate compose project name so it can't collide
# with a developer's already-running `docker compose up`. Requires Docker.
test-docker-smoke:
	mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go test -tags 'e2e docker' -count=1 -timeout 5m ./e2e/...

# reset-dev wipes local dev state back to first-run: stops the hecate on
# :8080 and deletes the data directory (which holds the bootstrap file
# with the admin token + AES-GCM key) so the next start regenerates
# fresh secrets. Memory-backed control plane is already wiped on
# restart; if you've pointed Hecate at postgres or redis, drop those
# out yourself.
reset-dev:
	@pid=$$(lsof -ti:8080 2>/dev/null); \
	if [ -n "$$pid" ]; then \
	  echo "stopping existing hecate on :8080 (pid $$pid)"; \
	  kill $$pid; \
	  sleep 0.3; \
	fi
	rm -rf .data
	rm -f hecate.bootstrap.json
	@echo "Local dev state reset. Next 'make run'/'make serve' regenerates the admin token."
	@echo "Clear hecate.* keys from your browser's localStorage to re-prompt the UI."

# reset-docker wipes the docker compose stack: stops + removes containers
# and removes the hecate-data, postgres-data, and ollama-models named
# volumes so the next 'docker compose up' re-bootstraps from scratch.
# --profile full activates the optional services so their volumes are
# also caught by 'down -v'.
reset-docker:
	docker compose --profile full down -v --remove-orphans
	@echo "Docker stack reset. Next 'docker compose up' regenerates the admin token."
	@echo "Clear hecate.* keys from your browser's localStorage to re-prompt the UI."
