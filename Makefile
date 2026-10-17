SHELL := /bin/bash
GO ?= go
PNPM ?= pnpm
WEB := apps/web

.PHONY: help build test test-go test-web test-perf lint lint-go lint-web fmt dev-sim dev-api dev-device dev-web seed backup restore clean

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the backend (static, CGO off) and the frontend
	CGO_ENABLED=0 $(GO) build -o bin/ ./cmd/...
	$(PNPM) --dir $(WEB) build

docker: ## Build the container image (VERSION from git, else dev)
	docker build --build-arg VERSION=$$(git rev-parse --short HEAD 2>/dev/null || echo dev) -t fronius-monitoring:local .

test: test-go test-web ## Run all tests

test-go: ## Go tests with race detector
	$(GO) test ./... -race

test-web: ## Frontend tests (vitest, single run)
	$(PNPM) --dir $(WEB) test --run

test-perf: ## Measure the ADR 0006 criterion (one year of data, < 200 ms; ≈6.3 M rows)
	FRONIUS_PERF_YEAR=1 $(GO) test ./internal/storage/ -run TestOneYearTimeSeriesPerformance -count=1 -v -timeout 30m

lint: lint-go lint-web ## Lint backend + frontend

lint-go:
	$(GO) vet ./...

lint-web:
	$(PNPM) --dir $(WEB) lint

fmt: ## Format the Go code
	$(GO) fmt ./...

dev-sim: ## Start the Fronius simulator (127.0.0.1:8081)
	$(GO) run ./cmd/fronius-sim

dev-api: ## Start froniusd (API 127.0.0.1:8080 + collector)
	$(GO) run ./cmd/froniusd

dev-device: ## Run froniusd against the inverter (make dev-device DEVICE=http://192.0.2.10)
	@test -n "$(DEVICE)" || { echo 'usage: make dev-device DEVICE=http://<inverter>'; exit 2; }
	FRONIUS_SOLARAPI_URL=$(DEVICE) $(GO) run ./cmd/froniusd

dev-web: ## Start the Vite development server (127.0.0.1:5173)
	$(PNPM) --dir $(WEB) dev

seed: ## Fill the database via the simulator (7 days of history)
	$(GO) run ./cmd/froniusd --backfill-days 7

backup: ## Snapshot the database (FRONIUS_BACKUP=path to choose the file)
	$(GO) run ./cmd/fronius-backup save -out "$${FRONIUS_BACKUP:-data/fronius-$$(date -u +%Y%m%dT%H%M%SZ).db}"

restore: ## Replace the database from a backup (FRONIUS_BACKUP=path, stop froniusd first)
	$(GO) run ./cmd/fronius-backup restore -from "$(FRONIUS_BACKUP)" -yes

clean: ## Remove generated artefacts
	rm -rf bin data coverage $(WEB)/dist
