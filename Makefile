# ============================================================================
# OlliTeX (overleaf-lab) — central Makefile
#
# "One Makefile controls everything" (Forgejo convention, 2026-09-07, owner).
# Every day-to-day operation — build, unit tests, hub integration suite,
# e2e, docker image, test-stack deploy — lives here with stable names.
#
#   make help          list targets
#   make ci            build + full unit + hub frontend suite (green gate)
#   make selftest      FULL local gate: lint + ci (our free replacement for
#                      hosted CI — this box IS the runner, 2026-09-16)
#   make release       selftest → docker image → (owner: push + cycle + probe)
#   make e2e           stack up + playwright suite
#   make wiki-shots    regenerate docs/wiki screenshots (needs the e2e stack)
#   make wiki-check    wiki docs gate (links + data-safety scan) for CI
#   make image         rebuild the server-ce docker image
#   make hooks-install install the repo git pre-push fast gate
#
# Notes:
#   * The repo uses Yarn PnP — always use `yarn`, never npm/npx inside
#     services/web. tests/e2e is an isolated npm package.
#   * Node >= 24 required (engines).
# ============================================================================

SHELL := /bin/bash
YARN  ?= yarn
export MAKEFLAGS := --no-print-directory

SERVICES_WEB := services/web
E2E_DIR      := tests/e2e
IMAGE_DIR    := server-ce

TEST_STACK   ?= ol-e2e-overleaf-1
STACK_PORT   ?= 7420

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z0-9._-]+:.*## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Webpack production bundle (services/web)
	cd $(SERVICES_WEB) && $(YARN) webpack:production

.PHONY: unit
unit: ## Full frontend unit + integration suite (vitest, all projects)
	cd $(SERVICES_WEB) && $(YARN) test:unit

.PHONY: hub
hub: ## /hub frontend integration suite only (vitest project HubFrontend)
	cd $(SERVICES_WEB) && $(YARN) vitest run --project=HubFrontend

.PHONY: i18n
i18n: ## Translation linter: code -> locales/en.json -> extracted-translations (bundle chain)
	cd $(SERVICES_WEB) && node scripts/translations/i18n-lint.js

.PHONY: lint
lint: ## ESLint (zero-warning policy)
	$(YARN) lint

.PHONY: format
format: ## Prettier check
	$(YARN) format

.PHONY: ci
ci: build i18n ## Green gate: build + i18n lint + full unit + hub suite
	$(MAKE) unit
	$(MAKE) hub

.PHONY: e2e-up
e2e-up: ## Start the dedicated test stack (tests/e2e)
	cd $(E2E_DIR) && npm run stack:up

.PHONY: e2e-down
e2e-down: ## Stop the dedicated test stack
	cd $(E2E_DIR) && npm run stack:down

.PHONY: e2e-logs
e2e-logs: ## Tail test-stack overleaf logs
	cd $(E2E_DIR) && npm run stack:logs

.PHONY: e2e
e2e: e2e-up ## Stack up + run the Playwright e2e suite
	cd $(E2E_DIR) && npm run test:tail

.PHONY: e2e-report
e2e-report: ## Open the Playwright HTML report
	cd $(E2E_DIR) && npm run report

.PHONY: deploy-test
deploy-test: build ## Deploy the bundle into the test stack + restart (web caches the manifest in memory)
	docker cp $(SERVICES_WEB)/public/. $(TEST_STACK):/overleaf/services/web/public/
	docker restart $(TEST_STACK)
	@echo "waiting for http://localhost:$(STACK_PORT)/login …"
	@for i in $$(seq 1 40); do \
		code=$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:$(STACK_PORT)/login || true); \
		[ "$$code" = "200" ] && echo "test stack ready ($$code)" && exit 0; \
		sleep 3; \
	done; \
	echo "test stack not ready after 120s" && exit 1

.PHONY: image
image: ## Rebuild the server-ce docker image (make all)
	cd $(IMAGE_DIR) && make all

.PHONY: clean
clean: ## Remove local caches (prettier/eslint) + Go binaries
	rm -rf ./.cache ./bin ./coverage-go.out

.PHONY: wiki-shots
wiki-shots: ## Regenerate the wiki screenshots + data-safety gate (needs the e2e stack up)
	cd $(E2E_DIR) && npm run wiki:shots

.PHONY: wiki-check
wiki-check: ## Wiki docs gate only (links + data-safety scan; for CI)
	cd $(E2E_DIR) && npm run wiki:check

.PHONY: all
all: ## Alias for ci
	$(MAKE) ci

# ----------------------------------------------------------------------------
# 2026-09-16 (owner task 11): the free CI replacement. No hosted runner, no
# minutes: this box runs everything. `make selftest` = the whole green gate
# locally; `make release` = gate + image, then the owner does the two manual
# promotion steps (push + prod cycle) on purpose.
# ----------------------------------------------------------------------------
.PHONY: selftest
selftest: ## Full local gate (no hosted CI): lint + build + i18n + unit + hub
	$(MAKE) lint
	$(MAKE) ci

.PHONY: release
release: selftest ## Gate + docker image; promotion (push/cycle/probe) stays a manual owner step
	$(MAKE) image
	@echo ""
	@echo "next (owner, on purpose):"
	@echo "  1) docker push sharelatex/sharelatex:main   (+ ext tag if you want the alias)"
	@echo "  2) cd /data_1/docker/compose_cep && sh cycle_overleafserver.sh"
	@echo "  3) run the 17-point prod probe (https://psintern… login, admin)"

.PHONY: hooks-install
hooks-install: ## Install repo git hooks (pre-push fast gate) — `git config core.hooksPath hooks`
	git config core.hooksPath hooks
	@echo "git hooks enabled (core.hooksPath=hooks); run this on each fresh clone"

# ----------------------------------------------------------------------------
# Go microservice conversions (owner task 0-6, 2026-09-12).
#
# 1:1 Go ports of the Node.js microservices live under services/<name>.go
# (package services); runnable entrypoints live under cmd/<service>/. The
# Makefile targets follow the Forgejo Go convention (lint-go / fmt / tidy /
# test). Toolchain: Go 1.27 (https://go.dev/dl/go1.27.1.linux-amd64.tar.gz).
# These are core-tool based (gofmt / go vet / go test) so they run offline.
# ----------------------------------------------------------------------------
GO      ?= go
GO_PKGS ?= ./services/... ./cmd/...

.PHONY: go-check
go-check: ## Verify the Go toolchain is present and >= 1.27
	@$(GO) version
	@$(GO) env GOVERSION

.PHONY: fmt-go
fmt-go: ## gofmt (write) on the Go services
	$(GO) fmt $(GO_PKGS)

.PHONY: fmt
fmt: fmt-go ## Forgejo-friendly alias for fmt-go

.PHONY: lint-go-vet
lint-go-vet: ## go vet on the Go services
	$(GO) vet $(GO_PKGS)

.PHONY: lint-go-fix
lint-go-fix: ## Apply auto-fixes (gofmt -w) to the Go services
	$(GO) fmt $(GO_PKGS)

.PHONY: lint-go
lint-go: ## Go lint gate: gofmt-clean + go vet (+ golangci-lint if installed)
	@test -z "$$(gofmt -l services/*.go 2>/dev/null)" || { echo "gofmt: reformat the files above (make fmt-go)"; exit 1; }
	$(GO) vet $(GO_PKGS)
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run $(GO_PKGS); else echo "(golangci-lint not installed — applied core go vet; optional: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest)"; fi

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: test-go
test-go: ## Run the Go service test suite (race detector + coverage)
	$(GO) test -race -cover -count=1 $(GO_PKGS)

.PHONY: test
test: test-go ## Run repo tests (Go services; node front-end uses 'unit'/'hub')

.PHONY: go-build
go-build: ## Build all Go service binaries into ./bin
	@mkdir -p bin
	$(GO) build -o bin/linked-url-proxy ./cmd/linked-url-proxy
	$(GO) build -o bin/webdavinterface ./cmd/webdavinterface
	$(GO) build -o bin/dropboxinterface ./cmd/dropboxinterface
	$(GO) build -o bin/githubinterface ./cmd/githubinterface
	$(GO) build -o bin/datamanipulator ./cmd/datamanipulator

.PHONY: go-run-linked-url-proxy
go-run-linked-url-proxy: ## Run the linked-url-proxy Go service (dev)
	$(GO) run ./cmd/linked-url-proxy

.PHONY: go-run-webdavinterface
go-run-webdavinterface: ## Run the webdavinterface Go service (dev)
	$(GO) run ./cmd/webdavinterface

.PHONY: go-run-dropboxinterface
go-run-dropboxinterface: ## Run the dropboxinterface Go service (dev)
	$(GO) run ./cmd/dropboxinterface

.PHONY: go-run-githubinterface
go-run-githubinterface: ## Run the githubinterface Go service (dev)
	$(GO) run ./cmd/githubinterface

.PHONY: go-run-datamanipulator
go-run-datamanipulator: ## Run the datamanipulator Go service (dev)
	$(GO) run ./cmd/datamanipulator

.DEFAULT_GOAL := help
