SHELL := /bin/sh

GOCACHE_DIR := $(CURDIR)/.gocache

.PHONY: test test-race coverage ui-coverage run dev ui-install ui-dev ui-build ui-test ui-test-e2e

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

ui-test:
	test -d ui/node_modules/@tailwindcss/vite || (echo "UI dependencies are out of date. Run 'make ui-install' first." && exit 1)
	cd ui && bun run test

ui-test-e2e:
	test -d ui/node_modules/@tailwindcss/vite || (echo "UI dependencies are out of date. Run 'make ui-install' first." && exit 1)
	cd ui && bun run test:e2e
