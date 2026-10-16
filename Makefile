SHELL := /bin/bash
GO ?= go
PNPM ?= pnpm
WEB := apps/web

.PHONY: help build test test-go test-web lint lint-go lint-web fmt dev-sim dev-api dev-web seed clean

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Compile le backend (statique, CGO off) et le frontend
	CGO_ENABLED=0 $(GO) build -o bin/ ./cmd/...
	$(PNPM) --dir $(WEB) build

test: test-go test-web ## Lance tous les tests

test-go: ## Tests Go avec détecteur de courses
	$(GO) test ./... -race

test-web: ## Tests frontend (vitest, run unique)
	$(PNPM) --dir $(WEB) test --run

lint: lint-go lint-web ## Lint backend + frontend

lint-go:
	$(GO) vet ./...

lint-web:
	$(PNPM) --dir $(WEB) lint

fmt: ## Formate le code Go
	$(GO) fmt ./...

dev-sim: ## Démarre le simulateur Fronius (127.0.0.1:8081)
	$(GO) run ./cmd/fronius-sim

dev-api: ## Démarre froniusd (API 127.0.0.1:8080 + collecteur)
	$(GO) run ./cmd/froniusd

dev-web: ## Démarre le serveur de développement Vite (127.0.0.1:5173)
	$(PNPM) --dir $(WEB) dev

seed: ## Remplit la base via le simulateur (7 jours d'historique)
	$(GO) run ./cmd/froniusd --backfill-days 7

clean: ## Supprime les artefacts générés
	rm -rf bin data coverage $(WEB)/dist
