SHELL := /bin/bash

MODULE      := github.com/dumhouse/clavis
BIN         := bin/clavis
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(BUILD_DATE)

.PHONY: all build web web-install dev server test test-race cover lint fmt fmt-check gen gen-api clean clean-all tools operator csi docs-site docs-dev

all: build

## build: compile the web app and the single clavis binary
build: web
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/clavis
	@echo "built $(BIN) $(VERSION)"

## web: install web dependencies and build the embedded SPA
web: web-install
	pnpm --dir web build

web-install:
	pnpm --dir web install --frozen-lockfile

## dev: Go server (proxying to Vite) with hot reload; open http://localhost:8080
dev:
	@trap 'kill %1 2>/dev/null || true' EXIT; \
	(cd web && pnpm dev) & \
	CLAVIS_DEV=true go run ./cmd/clavis server

## doctor: check the local toolchain, ports, and instance state
doctor:
	@bash scripts/doctor.sh

## server: run the built binary
server: build
	./$(BIN) server

## csi: build the Secrets Store CSI driver
csi:
	@mkdir -p bin
	cd csi && CGO_ENABLED=0 go build -trimpath -o ../bin/clavis-csi .

## operator: build the Kubernetes operator
operator:
	@mkdir -p bin
	cd operator && CGO_ENABLED=0 go build -trimpath -o ../bin/clavis-operator .

## docs-site: build the VitePress documentation site and landing page
docs-site:
	cd docs && pnpm install --frozen-lockfile && pnpm build

## docs-dev: preview the docs site at http://localhost:5174
docs-dev:
	cd docs && pnpm install --frozen-lockfile && pnpm dev

## test: unit and integration tests against SQLite
test:
	go test ./...

test-race:
	go test -race -shuffle=on ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## lint: Go vet + golangci-lint (if installed) + web lint
lint:
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; else echo "golangci-lint not installed; skipping"; fi
	@if [ -d web/node_modules ]; then pnpm --dir web lint; else echo "web/node_modules missing; run make web-install"; fi

fmt:
	gofmt -w cmd internal pkg web/embed.go
	@if [ -d web/node_modules ]; then pnpm --dir web format; fi

fmt-check:
	@test -z "$$(gofmt -l cmd internal pkg web/embed.go)" || { gofmt -l cmd internal pkg web/embed.go; exit 1; }
	@if [ -d web/node_modules ]; then pnpm --dir web format:check; fi

## gen: regenerate sqlc code (when installed) and the TypeScript API client
gen: gen-api
	@if command -v sqlc >/dev/null 2>&1; then sqlc generate; else echo "sqlc not installed; skipping"; fi

gen-api:
	@echo "generating web/src/api/schema.d.ts from a local server"
	@set -e; \
	DATA_DIR=$$(mktemp -d); \
	go build -o "$$DATA_DIR/clavis" ./cmd/clavis; \
	CLAVIS_DATA_DIR="$$DATA_DIR" CLAVIS_SERVER_ADDR=127.0.0.1:18081 "$$DATA_DIR/clavis" server >/dev/null 2>&1 & \
	PID=$$!; \
	trap 'kill $$PID 2>/dev/null || true; rm -rf "$$DATA_DIR"' EXIT; \
	for i in $$(seq 1 100); do \
		kill -0 $$PID 2>/dev/null || { echo "gen-api: server exited (is port 18081 in use?)"; exit 1; }; \
		curl -sf http://127.0.0.1:18081/healthz >/dev/null 2>&1 && break; sleep 0.1; done; \
	CLAVIS_OPENAPI_URL=http://127.0.0.1:18081/api/v1/openapi.json pnpm --dir web gen:api

## docs-cli: regenerate the CLI reference (docs/reference, docs and web JSON)
docs-cli:
	go run ./internal/tools/clidocs

## tools: install developer tooling
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/evilmartians/lefthook@latest

clean:
	rm -rf bin dist/dry-run csi/csi coverage.out coverage.html
	rm -rf web/dist && mkdir -p web/dist && touch web/dist/.gitkeep

clean-all: clean
	rm -rf web/node_modules docs/node_modules
