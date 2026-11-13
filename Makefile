# Turnbacks developer commands. Run `make help` for a list.
SHELL := /bin/sh

GO_PKGS        := ./cmd/... ./internal/... ./db/... ./api/... ./tools/... ./web
GOLANGCI       := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Ports are discovered at run time by scripts/dev.sh (nothing is assumed free).
# Preferred starting points can be set: make dev DB_PORT=5433 API_PORT=9000 WEB_PORT=5200
DEV            := sh scripts/dev.sh
export DB_PORT API_PORT WEB_PORT E2E_PORT

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ---- Development ----------------------------------------------------------

.PHONY: setup
setup: ## Install frontend dependencies and Playwright's browser
	cd web && npm ci && npx playwright install chromium

.PHONY: db-up
db-up: ## Start (or reuse) local PostgreSQL on a free port; prints the port
	@$(DEV) db

.PHONY: db-down
db-down: ## Stop local PostgreSQL (data is kept in a volume)
	docker compose down

.PHONY: dev
dev: ## Run PostgreSQL, the API and the Vite dev server on free ports (you are admin locally)
	@$(DEV) run

.PHONY: seed
seed: ## Load six months of demonstration data (development only)
	@$(DEV) turnbacks seed

.PHONY: seed-purge
seed-purge: ## Remove all demonstration data
	@$(DEV) turnbacks seed --purge

## ---- Code generation ------------------------------------------------------

.PHONY: generate
generate: sqlc api-types ## Run all code generators

.PHONY: sqlc
sqlc: ## Generate type-safe Go from db/queries (requires sqlc)
	sqlc generate

.PHONY: api-types
api-types: ## Generate TypeScript API types from api/openapi.yaml
	cd web && npm run gen:api

## ---- Database -------------------------------------------------------------

.PHONY: migrate-up
migrate-up: ## Apply pending migrations
	@$(DEV) turnbacks migrate up

.PHONY: migrate-down
migrate-down: ## Roll back the latest migration
	@$(DEV) turnbacks migrate down

.PHONY: migrate-status
migrate-status: ## Show migration status
	@$(DEV) turnbacks migrate status

## ---- Build ----------------------------------------------------------------

.PHONY: web
web: ## Build the frontend into web/dist
	cd web && npm run build

.PHONY: build
build: web cli ## Build the server (SPA embedded) and the tb CLI into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/turnbacks ./cmd/turnbacks

.PHONY: cli
cli: ## Build the tb command-line client into bin/tb
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/tb ./cmd/tb

.PHONY: cli-all
cli-all: ## Cross-compile tb for macOS, Linux and Windows into bin/
	@for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do \
	  os=$${target%/*}; arch=$${target#*/}; ext=$$( [ $$os = windows ] && echo .exe ); \
	  echo "bin/tb-$$os-$$arch$$ext"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/tb-$$os-$$arch$$ext ./cmd/tb || exit 1; \
	done

.PHONY: install-cli
install-cli: cli ## Install tb to ~/.local/bin with bash and zsh completions
	install -d $(HOME)/.local/bin $(HOME)/.local/share/bash-completion/completions $(HOME)/.local/share/zsh/site-functions
	install -m 0755 bin/tb $(HOME)/.local/bin/tb
	bin/tb completion bash > $(HOME)/.local/share/bash-completion/completions/tb
	bin/tb completion zsh > $(HOME)/.local/share/zsh/site-functions/_tb
	@echo "Installed. For zsh add to ~/.zshrc (before compinit): fpath=(~/.local/share/zsh/site-functions \$$fpath)"

.PHONY: docker
docker: ## Build the production container image
	docker build --build-arg VERSION=$(VERSION) -t turnbacks:$(VERSION) -t turnbacks:latest .

## ---- Admin certificates (docs/admin-certificates.md) ------------------------

CA_DIR ?= ca

.PHONY: ca-init
ca-init: ## Create the admin client-certificate CA in $(CA_DIR)/
	go run ./cmd/turnbacks ca init --dir $(CA_DIR)

.PHONY: ca-issue
ca-issue: ## Issue an admin certificate: make ca-issue NAME=jane [DAYS=365]
	@test -n "$(NAME)" || { echo "usage: make ca-issue NAME=<admin-name> [DAYS=365]" >&2; exit 2; }
	go run ./cmd/turnbacks ca issue --dir $(CA_DIR) --days $(or $(DAYS),365) $(NAME)

## ---- Quality --------------------------------------------------------------

.PHONY: test
test: test-go test-web ## Run Go (unit + PostgreSQL integration) and frontend tests

.PHONY: test-go
test-go: ## Go tests; integration tests start PostgreSQL via testcontainers
	go test -race -count=1 $(GO_PKGS)

.PHONY: test-web
test-web: ## Vitest + React Testing Library
	cd web && npm test

.PHONY: e2e
e2e: build ## Playwright end-to-end tests against the production binary
	@$(DEV) e2e

.PHONY: lint
lint: ## Lint Go (golangci-lint), TypeScript (tsc, eslint) and formatting
	$(GOLANGCI) run ./...
	cd web && npx tsc -b && npm run lint && npm run format:check

.PHONY: fmt
fmt: ## Format Go and frontend code
	gofmt -w cmd internal db api web/embed.go
	cd web && npm run format

.PHONY: check
check: lint test ## Everything CI runs (except e2e)

.PHONY: clean
clean: ## Remove build outputs and web/node_modules (run make setup afterwards)
	rm -rf bin web/node_modules web/dist/assets web/dist/*.html web/dist/*.js web/test-results web/playwright-report
