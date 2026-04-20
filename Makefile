SHELL := /bin/sh

GOCACHE_DIR := $(CURDIR)/.gocache

.PHONY: test run dev ui-install ui-dev ui-build

test:
	mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go test ./...

run:
	mkdir -p "$(GOCACHE_DIR)"
	GOCACHE="$(GOCACHE_DIR)" go run ./cmd/gateway

dev:
	mkdir -p "$(GOCACHE_DIR)"
	set -a; \
	. ./.env; \
	set +a; \
	GOCACHE="$(GOCACHE_DIR)" go run ./cmd/gateway

ui-install:
	cd ui && pnpm install

ui-dev:
	test -d ui/node_modules/@tailwindcss/vite || (echo "UI dependencies are out of date. Run 'make ui-install' first." && exit 1)
	cd ui && pnpm dev

ui-build:
	test -d ui/node_modules/@tailwindcss/vite || (echo "UI dependencies are out of date. Run 'make ui-install' first." && exit 1)
	cd ui && pnpm build
