SHELL := /usr/bin/env bash

GO ?= go
COMPOSE_BIN ?= $(shell if docker compose version >/dev/null 2>&1; then printf 'docker compose'; else printf 'docker-compose'; fi)
COMPOSE ?= $(COMPOSE_BIN) -f deploy/docker-compose.yaml
FRONTEND_DIR ?= /Users/nikpeskov/Projects/team-os
SEED_DIR ?= $(CURDIR)/.seed
SERVICE ?=

ifneq ($(wildcard .env),)
include .env
export
endif

SEED_DIR_ABS := $(abspath $(SEED_DIR))

MODULE_DIRS = pkg $(sort $(patsubst %/go.mod,%,$(wildcard services/*/go.mod)))

.DEFAULT_GOAL := help

.PHONY: help up down logs compose-config migrate seed export-fixtures gen test test-race lint fmt check-contract dev dev-keys ensure-env

help: ## Show available commands.
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

up: ensure-env ## Build and start the development stack.
	$(COMPOSE) up --build -d

ensure-env:
	@if [[ ! -f .env ]]; then $(MAKE) --no-print-directory dev-keys; fi

down: ## Stop the development stack.
	$(COMPOSE) down --remove-orphans

logs: ## Follow logs from the development stack.
	$(COMPOSE) logs -f

compose-config: ## Validate and render the Compose configuration.
	$(COMPOSE) config --quiet

migrate: ensure-env ## Apply all currently registered service migrations.
	$(COMPOSE) run --rm company-migrate
	$(COMPOSE) run --rm kb-migrate
	$(COMPOSE) run --rm tasks-migrate

seed: ## Load already exported JSON fixtures from SEED_DIR.
	@test -d "$(SEED_DIR_ABS)" || { echo "SEED_DIR does not exist: $(SEED_DIR_ABS)" >&2; exit 1; }
	@for service in company kb tasks academy notifications; do \
		[[ -d "services/$$service/cmd/seed" ]] || continue; \
		$(GO) run "./services/$$service/cmd/seed" --fixtures "$(SEED_DIR_ABS)"; \
	done

export-fixtures: ## Optionally run a frontend fixture exporter when one exists.
	@test -d "$(FRONTEND_DIR)" || { echo "FRONTEND_DIR does not exist: $(FRONTEND_DIR)" >&2; exit 1; }
	@test -f "$(FRONTEND_DIR)/scripts/export-fixtures.ts" || { echo "Missing $(FRONTEND_DIR)/scripts/export-fixtures.ts" >&2; exit 1; }
	@mkdir -p "$(SEED_DIR_ABS)"
	@cd "$(FRONTEND_DIR)" && npx --yes tsx scripts/export-fixtures.ts --output "$(SEED_DIR_ABS)"

gen: ## Generate configured protobuf, OpenAPI and sqlc artifacts.
	@if [[ -f contracts/buf.gen.yaml ]]; then buf generate contracts --template contracts/buf.gen.yaml; fi
	@if [[ -f contracts/openapi/teamos.yaml && -d services/gateway/internal/api ]]; then \
		oapi-codegen -generate types,chi-server,spec -package api \
			-o services/gateway/internal/api/teamos.gen.go contracts/openapi/teamos.yaml; \
	fi
	@for config in services/*/sqlc.yaml; do \
		[[ -f "$$config" ]] || continue; \
		sqlc generate -f "$$config"; \
	done
	@if [[ -d tools/generate ]]; then $(GO) run ./tools/generate; fi

test: ## Run unit tests in every Go module.
	@set -e; for module in $(MODULE_DIRS); do echo "==> go test $$module"; (cd "$$module" && GOWORK=off $(GO) test ./...); done

test-race: ## Run Go tests with the race detector in every module.
	@set -e; for module in $(MODULE_DIRS); do echo "==> go test -race $$module"; (cd "$$module" && GOWORK=off $(GO) test -race ./...); done

lint: ## Run golangci-lint in every Go module.
	@command -v golangci-lint >/dev/null || { echo "golangci-lint is required" >&2; exit 1; }
	@set -e; for module in $(MODULE_DIRS); do echo "==> golangci-lint $$module"; (cd "$$module" && GOWORK=off golangci-lint run --config "$(CURDIR)/.golangci.yaml" ./...); done

fmt: ## Format Go code in every module.
	@set -e; for module in $(MODULE_DIRS); do (cd "$$module" && $(GO) fmt ./...); done

check-contract: ## Validate contracts and compare with the frontend when the sync tool exists.
	@npx --yes @redocly/cli@1.34.2 lint contracts/openapi/teamos.yaml
	@cd contracts && buf lint && buf build
	@if [[ -d tools/sync-contract ]]; then \
		FRONTEND_DIR="$(FRONTEND_DIR)" $(GO) run ./tools/sync-contract; \
	else \
		echo "tools/sync-contract is not implemented yet; schema validation completed"; \
	fi

dev: ## Run one service locally: make dev SERVICE=company.
	@test -n "$(SERVICE)" || { echo "SERVICE is required" >&2; exit 1; }
	@test -f "services/$(SERVICE)/go.mod" || { echo "Unknown service: $(SERVICE)" >&2; exit 1; }
	$(GO) run "./services/$(SERVICE)/cmd/$(SERVICE)"

dev-keys: ## Create .env with a fresh Ed25519 development key pair.
	@command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }
	@test ! -e .env || { echo ".env already exists; refusing to overwrite it" >&2; exit 1; }
	@tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
	openssl genpkey -algorithm ED25519 -out "$$tmp/private.pem" >/dev/null 2>&1; \
	openssl pkey -in "$$tmp/private.pem" -pubout -out "$$tmp/public.pem" >/dev/null 2>&1; \
	private="$$(openssl base64 -A -in "$$tmp/private.pem")"; \
	public="$$(openssl base64 -A -in "$$tmp/public.pem")"; \
	awk -v private="$$private" -v public="$$public" \
		'/^COMPANY_JWT_PRIVATE_KEY=/{print "COMPANY_JWT_PRIVATE_KEY=" private; next} /^GATEWAY_JWT_PUBLIC_KEY=/{print "GATEWAY_JWT_PUBLIC_KEY=" public; next} {print}' \
		.env.example > .env; \
	echo "Created .env with development-only signing keys"
