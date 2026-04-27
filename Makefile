SHELL := /bin/sh

GOCACHE_DIR := $(CURDIR)/.gocache

.PHONY: test test-race coverage ui-coverage build run serve dev ui-install ui-dev ui-build ui-test ui-test-e2e test-docker-smoke reset-dev reset-docker screenshots

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
	@echo "On next page load, the UI auto-detects the rejected stale token and re-prompts."

# screenshots is the one-shot end-to-end capture workflow:
# reset → build (if needed) → start hecate in the background → wait
# for /healthz → run the bun capture script → stop hecate. Everything
# is reset to a clean state on entry and torn down on exit, so two
# successive `make screenshots` calls always produce identical files.
#
# ollama on :11434 with `llama3.1:8b` pulled produces the realistic
# chat turn shown in the README hero; HECATE_SKIP_OLLAMA=1 lets you
# run the workflow without it (chat session will be empty).
screenshots:
	@test -d ui/node_modules/@playwright/test || (echo "UI dependencies missing. Run 'make ui-install' first." && exit 1)
	@pid=$$(lsof -ti:8080 2>/dev/null); [ -n "$$pid" ] && (echo "stopping existing :8080 (pid $$pid)"; kill $$pid; sleep 0.3) || true
	@$(MAKE) --no-print-directory reset-dev > /dev/null
	@test -x ./hecate || $(MAKE) --no-print-directory build
	@mkdir -p .data
	@echo "starting hecate in background…"
	@./hecate > .data/screenshots-gateway.log 2>&1 & echo $$! > .data/screenshots-gateway.pid
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
	  curl -sf http://127.0.0.1:8080/healthz > /dev/null && break; \
	  sleep 0.3; \
	done
	@cd ui && bun run capture-screenshots; \
	  status=$$?; \
	  cd ..; \
	  kill $$(cat .data/screenshots-gateway.pid 2>/dev/null) 2>/dev/null || true; \
	  rm -f .data/screenshots-gateway.pid; \
	  echo "gateway stopped — screenshots are in docs/screenshots/"; \
	  exit $$status

# reset-docker wipes the docker compose stack: stops + removes containers
# and removes the hecate-data, postgres-data, and ollama-models named
# volumes so the next 'docker compose up' re-bootstraps from scratch.
# --profile full activates the optional services so their volumes are
# also caught by 'down -v'.
reset-docker:
	docker compose --profile full down -v --remove-orphans
	@echo "Docker stack reset. Next 'docker compose up' regenerates the admin token."
	@echo "On next page load, the UI auto-detects the rejected stale token and re-prompts."
