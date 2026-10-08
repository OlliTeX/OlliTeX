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
#   make image         rebuild the ollitex docker image set (base + app +
#                      pandoc/pdftocairo/png2pdf — from the images/ tree;
#                      the former build-images/Makefile merged here 2026-10-01)
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

WEB := frontend
SERVICES_WEB := services/web
E2E_DIR      := tests/e2e

TEST_STACK   ?= ol-e2e-overleaf-1
STACK_PORT   ?= 7420

# ----------------------------------------------------------------------------
# Docker image builds (merged from build-images/Makefile 2026-10-01, owner
# directive: one Makefile controls everything; retired to junk/).
# All images live under images/&lt;name&gt;/Dockerfile (2026-10-01 reorg) with
# THIS repo root as the build context.
# ----------------------------------------------------------------------------
export MONOREPO_REVISION := $(shell git rev-parse HEAD)
export BRANCH_NAME ?= $(shell git rev-parse --abbrev-ref HEAD)
export BRANCH_NAME_TAG_SAFE ?= $(subst /,--,$(BRANCH_NAME))
export OVERLEAF_BASE_BRANCH ?= ollitex/base:$(BRANCH_NAME_TAG_SAFE)
export OVERLEAF_BASE_LATEST ?= ollitex/base
export OVERLEAF_BASE_TAG ?= ollitex/base:$(BRANCH_NAME_TAG_SAFE)-$(MONOREPO_REVISION)
export OVERLEAF_BRANCH ?= ollitex/ollitex:$(BRANCH_NAME_TAG_SAFE)
export OVERLEAF_LATEST ?= ollitex/ollitex
export OVERLEAF_TAG ?= ollitex/ollitex:$(BRANCH_NAME_TAG_SAFE)-$(MONOREPO_REVISION)
export OVERLEAF_PANDOC_BRANCH ?= ollitex/pandoc:$(BRANCH_NAME_TAG_SAFE)
export OVERLEAF_PANDOC_LATEST ?= ollitex/pandoc
export OVERLEAF_PANDOC_TAG ?= ollitex/pandoc:$(BRANCH_NAME_TAG_SAFE)-$(MONOREPO_REVISION)
export OVERLEAF_PDFTOCAIRO_BRANCH ?= ollitex/pdftocairo:$(BRANCH_NAME_TAG_SAFE)
export OVERLEAF_PDFTOCAIRO_LATEST ?= ollitex/pdftocairo
export OVERLEAF_PDFTOCAIRO_TAG ?= ollitex/pdftocairo:$(BRANCH_NAME_TAG_SAFE)-$(MONOREPO_REVISION)
export OVERLEAF_PNG2PDF_BRANCH ?= ollitex/png2pdf:$(BRANCH_NAME_TAG_SAFE)
export OVERLEAF_PNG2PDF_LATEST ?= ollitex/png2pdf
export OVERLEAF_PNG2PDF_TAG ?= ollitex/png2pdf:$(BRANCH_NAME_TAG_SAFE)-$(MONOREPO_REVISION)
export OVERLEAF_TYPSF_BRANCH ?= ollitex/typst:$(BRANCH_NAME_TAG_SAFE)
export OVERLEAF_TYPSF_LATEST ?= ollitex/typst
export OVERLEAF_TYPSF_TAG ?= ollitex/typst:$(BRANCH_NAME_TAG_SAFE)-$(MONOREPO_REVISION)

# Optional extra --cache-from references (overridable). The branch tags only
# exist LOCALLY (they are never pushed to Docker Hub), so pulling them as cache
# sources fails with 404/"insufficient_scope" on Docker Hub. The builds rely
# on the local inline cache (BUILDKIT_INLINE_CACHE=1) + locally tagged images
# instead. Default: no remote cache-from at all (2026-08-31, build 49 failure).
CACHE_FROM_BASE ?=
CACHE_FROM_COMMUNITY ?=
CACHE_FROM_PANDOC ?=
CACHE_FROM_PDFTOCAIRO ?=
CACHE_FROM_PNG2PDF ?=
CACHE_FROM_TYPSF ?=

# Which base Dockerfile build-base uses. Alpine:3.24 is the CANONICAL base
# (cutover 2026-09-28, owner directive: alpine if it works — e2e gate GREEN,
# image smaller: 6.43GB vs 6.7GB ubuntu). The ubuntu file is retired in
# junk/Dockerfile-base-ubuntu26.04; override BASE_FILE to restore it.
# (moved to images/base-amd64 with the 2026-10-01 images/ reorg)
BASE_FILE ?= images/base-amd64/Dockerfile

# Which Go builder the app image compiles on (cutover 2026-09-28: the musl
# alpine:3.24 builder is the default; the glibc one is retired in
# junk/images-golang-builder-amd64-ubuntu and can be passed back via
# GO_BUILDER_TAG if ever needed).
GO_BUILDER_TAG ?= ollitex/golang-builder-amd64-alpine:1.27.1

.PHONY: images
images: build-base build-community build-pandoc build-pdftocairo build-png2pdf build-typst ## Build ALL ollitex docker images (base + app + pandoc/pdftocairo/png2pdf/typst)

.PHONY: refresh-cache
refresh-cache: refresh-cache-branch refresh-cache-latest ## Pull locally-tagged image refs as remote cache sources (best effort)

.PHONY: refresh-cache-branch
refresh-cache-branch:
	docker inspect $(OVERLEAF_BASE_BRANCH) > /dev/null && docker pull $(OVERLEAF_BASE_BRANCH) || true
	docker inspect $(OVERLEAF_BRANCH) > /dev/null && docker pull $(OVERLEAF_BRANCH) || true
	docker inspect $(OVERLEAF_PANDOC_BRANCH) > /dev/null && docker pull $(OVERLEAF_PANDOC_BRANCH) || true
	docker inspect $(OVERLEAF_PDFTOCAIRO_BRANCH) > /dev/null && docker pull $(OVERLEAF_PDFTOCAIRO_BRANCH) || true
	docker inspect $(OVERLEAF_PNG2PDF_BRANCH) > /dev/null && docker pull $(OVERLEAF_PNG2PDF_BRANCH) || true

.PHONY: refresh-cache-latest
refresh-cache-latest:
	docker inspect $(OVERLEAF_BASE_LATEST) > /dev/null && docker pull $(OVERLEAF_BASE_LATEST) || true
	docker inspect $(OVERLEAF_LATEST) > /dev/null && docker pull $(OVERLEAF_LATEST) || true
	docker inspect $(OVERLEAF_PANDOC_LATEST) > /dev/null && docker pull $(OVERLEAF_PANDOC_LATEST) || true
	docker inspect $(OVERLEAF_PDFTOCAIRO_LATEST) > /dev/null && docker pull $(OVERLEAF_PDFTOCAIRO_LATEST) || true
	docker inspect $(OVERLEAF_PNG2PDF_LATEST) > /dev/null && docker pull $(OVERLEAF_PNG2PDF_LATEST) || true

.PHONY: build-base
build-base: ## Build the ollitex/base image (alpine + TeX Live) from images/base-amd64
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --file $(BASE_FILE) \
	  --pull \
	  $(CACHE_FROM_BASE) \
	  --tag $(OVERLEAF_BASE_TAG) \
	  --tag $(OVERLEAF_BASE_BRANCH) \
	  --network=host \
	  .

TOOLKIT_TAG ?= ollitex/toolkit-tui:main

.PHONY: build-toolkit
build-toolkit: ## Build the toolkit TUI image (golang builder -> alpine 3.24, single static binary)
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --build-arg GO_BUILDER_TAG \
	  --build-arg MONOREPO_REVISION=$(MONOREPO_REVISION) \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  --file images/toolkit-amd64/Dockerfile \
	  --tag $(TOOLKIT_TAG) \
	  --network=host \
	  .

.PHONY: build-community
build-community: ## Build the ollitex/ollitex app image from images/main-amd64 (yarn + Go + webpack inside)
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --build-arg OVERLEAF_BASE_TAG \
	  --build-arg GO_BUILDER_TAG \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  $(CACHE_FROM_COMMUNITY) \
	  --file images/main-amd64/Dockerfile \
	  --tag $(OVERLEAF_TAG) \
	  --tag $(OVERLEAF_BRANCH) \
	  --network=host \
	  .

.PHONY: build-pandoc
build-pandoc: ## Build the ollitex/pandoc image from images/pandoc-amd64
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --build-arg OVERLEAF_BASE_TAG \
	  --build-arg GO_BUILDER_TAG \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  $(CACHE_FROM_PANDOC) \
	  --file images/pandoc-amd64/Dockerfile \
	  --tag $(OVERLEAF_PANDOC_TAG) \
	  --tag $(OVERLEAF_PANDOC_BRANCH) \
	  --network=host \
	  .

# pdftocairo replacement for quay.io/sharelatex/pdftocairo:24.02 (clsitex
# PDF→JPEG). Standalone alpine with no COPY, so no .dockerignore needed.
.PHONY: build-pdftocairo
build-pdftocairo: ## Build the ollitex/pdftocairo image from images/pdftocairo-amd64
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  $(CACHE_FROM_PDFTOCAIRO) \
	  --file images/pdftocairo-amd64/Dockerfile \
	  --tag $(OVERLEAF_PDFTOCAIRO_TAG) \
	  --tag $(OVERLEAF_PDFTOCAIRO_BRANCH) \
	  --network=host \
	  .

# png2pdf replacement for quay.io/sharelatex/png2pdf:2026-06-24 (clsitex
# PNG->PDF slow-PNG optimisation). python:3.14-alpine + img2pdf (pinned git
# commit) + the png2pdf.sh CLI shim (images/png2pdf-amd/png2pdf.sh, copied
# from the root context, which uses the root .dockerignore like the other
# COPY-based targets).
.PHONY: build-png2pdf
build-png2pdf: ## Build the ollitex/png2pdf image from images/png2pdf-amd
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  $(CACHE_FROM_PNG2PDF) \
	  --file images/png2pdf-amd/Dockerfile \
	  --tag $(OVERLEAF_PNG2PDF_TAG) \
	  --tag $(OVERLEAF_PNG2PDF_BRANCH) \
	  --network=host \
	  .

# typst compiler image (D21 sourcemap fork) for the Go clsi_typst service
# (go/services/clsitypst). Self-contained rust:alpine build that re-clones
# typst at a pinned SHA and applies the sourcemap patch
# (images/typst-amd64/patches/0001-clsi-sourcemap.patch, COPYd from the repo
# ROOT context — see that Dockerfile, where the path is therefore
# root-relative). No BASE/BUILDER build-args (unlike pandoc). The vanilla
# `pandoc/typst` image remains the service default (config
# DefaultDockerImage); this tag is the sync/sourcemap-capable fork the
# deployment selects via TYPST_IMAGE/TYPST_DOCKER_IMAGE when it wants
# click-to-source.
.PHONY: build-typst
build-typst: ## Build the ollitex/typst (D21 sourcemap fork) image from images/typst-amd64
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  $(CACHE_FROM_TYPSF) \
	  --file images/typst-amd64/Dockerfile \
	  --tag $(OVERLEAF_TYPSF_TAG) \
	  --tag $(OVERLEAF_TYPSF_BRANCH) \
	  --network=host \
	  .

.PHONY: clean-images
clean-images: ## Remove the locally-built ollitex docker image tags (docker rmi)
	-docker rmi --force $(OVERLEAF_BASE_TAG) $(OVERLEAF_TAG) $(OVERLEAF_PANDOC_TAG) $(OVERLEAF_PDFTOCAIRO_TAG) $(OVERLEAF_PNG2PDF_TAG) $(OVERLEAF_TYPSF_TAG)

.PHONY: image-push
image-push: ## Push the ollitex image tags (owner gate — run on purpose)
	docker push $(OVERLEAF_BASE_TAG)
	docker push $(OVERLEAF_BASE_BRANCH)
	docker push $(OVERLEAF_TAG)
	docker push $(OVERLEAF_BRANCH)
	docker push $(OVERLEAF_PANDOC_TAG)
	docker push $(OVERLEAF_PDFTOCAIRO_TAG)
	docker push $(OVERLEAF_PNG2PDF_TAG)
	docker push $(OVERLEAF_TYPSF_TAG)

SHELLCHECK_OPTS = \
	--shell=bash \
	--external-sources \
	--exclude=SC1091
SHELLCHECK_COLOR := $(if $(CI),--color=never,--color)
SHELLCHECK_FILES := { git ls-files "*.sh" -z; git grep -Plz "\A\#\!.*bash"; } | sort -zu

.PHONY: shellcheck
shellcheck: ## Shellcheck all shell scripts (dockerized koalaman/shellcheck)
	@echo "[shellcheck] $(subst :, ,$(notdir $(SHELLCHECK_FILES)))"
	$(SHELLCHECK_FILES) | xargs -0 -r docker run --rm -v $(CURDIR):/mnt -w /mnt \
		koalaman/shellcheck:stable $(SHELLCHECK_OPTS) $(SHELLCHECK_COLOR)

.PHONY: shellcheck-fix
shellcheck-fix: ## Shellcheck --format=diff applied per file (review output before accepting)
	@$(SHELLCHECK_FILES) | while IFS= read -r -d '' file; do \
		diff=$$(docker run --rm -v $(CURDIR):/mnt -w /mnt koalaman/shellcheck:stable $(SHELLCHECK_OPTS) --format=diff "$$file" 2>/dev/null); \
		if [ -n "$$diff" ] && ! echo "$$diff" | patch -p1 &>/dev/null 2>&1; then echo "\033[31m$$file\033[0m"; \
		elif [ -n "$$diff" ]; then echo "$$file"; \
		else echo "\033[2m$$file\033[0m"; fi \
	done

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z0-9._-]+:.*## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Webpack production bundle (frontend)
	cd $(WEB) && $(YARN) webpack:production

.PHONY: unit
unit: ## Full unit + integration suite (Node-app suite in services/web, frontend suite in frontend/)
	cd $(SERVICES_WEB) && $(YARN) test:unit
	cd $(WEB) && $(YARN) test:unit

.PHONY: hub
hub: ## /hub frontend integration suite only (vitest project HubFrontend)
	cd $(WEB) && $(YARN) vitest run --config vitest.config.js --project=HubFrontend

.PHONY: i18n
i18n: ## Translation linter: code -> locales/en.json -> extracted-translations (bundle chain)
	cd $(SERVICES_WEB) && node scripts/translations/i18n-lint.js

.PHONY: lint
lint: ## ESLint (zero-warning policy)
	cd $(WEB) && $(YARN) lint

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
	docker cp public/. $(TEST_STACK):/overleaf/public/
	docker restart $(TEST_STACK)
	@echo "waiting for http://localhost:$(STACK_PORT)/login …"
	@for i in $$(seq 1 40); do \
		code=$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:$(STACK_PORT)/login || true); \
		[ "$$code" = "200" ] && echo "test stack ready ($$code)" && exit 0; \
		sleep 3; \
	done; \
	echo "test stack not ready after 120s" && exit 1

.PHONY: image
image: go-build images ## Rebuild the app docker image set (base + ollitex + pandoc/pdftocairo/png2pdf); ensure Go binaries are fresh

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
	@echo "  1) docker push ollitex/ollitex:main   (+ ext tag if you want the alias)"
	@echo "  2) cd /data_1/docker/compose_cep && sh cycle_overleafserver.sh"
	@echo "  3) run the 17-point prod probe (https://psintern… login, admin)"

.PHONY: hooks-install
hooks-install: ## Install repo git hooks (pre-push fast gate) — `git config core.hooksPath hooks`
	git config core.hooksPath hooks
	@echo "git hooks enabled (core.hooksPath=hooks); run this on each fresh clone"

# ----------------------------------------------------------------------------
# Go microservice conversions (owner task 0-6, 2026-09-12).
#
# 1:1 Go ports of the Node.js microservices live under go/services/<name>/
# (one package per service, mirroring the Node module layout) with a shared
# helper package go/pbhttp; runnable entrypoints live under cmd/<service>/.
# The live Node folders remain services/<name>/. The Makefile targets follow
# the Forgejo Go convention (lint-go / fmt / tidy / test). Toolchain: Go 1.27
# (https://go.dev/dl/go1.27.1.linux-amd64.tar.gz). These are core-tool based
# (gofmt / go vet / go test) so they run offline.
# ----------------------------------------------------------------------------
GO      ?= go
GO_PKGS ?= ./go/... ./cmd/...

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
	@test -z "$$(gofmt -l go/ cmd/ 2>/dev/null)" || { echo "gofmt: reformat the files above (make fmt-go)"; exit 1; }
	$(GO) vet $(GO_PKGS)
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run $(GO_PKGS); else echo "(golangci-lint not installed — applied core go vet; optional: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest)"; fi

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: test-go
test-go: ## Run the Go service test suite (race detector + coverage)
	@if command -v pg_isready >/dev/null 2>/dev/null && pg_isready -h 127.0.0.1 -p 5432 -q 2>/dev/null; then \
		PGPASSWORD=overleaf psql -h 127.0.0.1 -p 5432 -U overleaf -d overleaf-history-v1 -c 'CREATE DATABASE configstore_test' >/dev/null 2>&1 || true; \
		echo "test-go: using e2e Postgres for the configstore PG gates (127.0.0.1:5432)"; \
		export CONFIGSTORE_TEST_PG_DSN='postgres://overleaf:overleaf@127.0.0.1:5432/configstore_test?sslmode=disable'; \
	fi; \
	$(GO) test -race -cover -count=1 $(GO_PKGS)
	@-if [ -z "$$CONFIGSTORE_TEST_PG_DSN" ]; then echo "(configstore PG gates skipped — no Postgres at 127.0.0.1:5432; 'make e2e-up' or a scratch postgres:18-alpine enables them)"; fi

.PHONY: test
test: test-go ## Run repo tests (Go services; node front-end uses 'unit'/'hub')

# (2026-09-28) the standalone services/{history-v1,document-updater}.go module
# targets are retired with their trees (junk/ + D41-DU/otc-reduction); the Go
# gate is `test-go` over the root module (./go/... ./cmd/...).

.PHONY: go-build
go-build: ## Build all Go service binaries into ./bin
	@mkdir -p bin
	# (AJ wave 2026-10-08) the standalone linked-url-proxy / webdavinterface /
	# dropboxinterface / githubinterface cmds were absorbed into the in-process
	# web feature plane (TPDS merge — go/services/web/features/{webdav,ghsync,
	# federation} consume the go/services/* clients directly); their cmd entry
	# points no longer exist.
	$(GO) build -o bin/datamanipulator ./cmd/datamanipulator
	$(GO) build -o bin/filestore ./cmd/filestore
	$(GO) build -o bin/notifications ./cmd/notifications
	$(GO) build -o bin/chat ./cmd/chat
	$(GO) build -o bin/docstore ./cmd/docstore
	$(GO) build -o bin/web ./cmd/web
	$(GO) build -o bin/seaweed-migrate ./cmd/seaweed-migrate  ## fs <-> SeaweedFS(S3) conversion + health tool
	$(GO) build -o bin/configdb ./cmd/configdb  ## operator CLI for the SQLite config DB (P7-post)
	$(GO) build -o bin/cronmail ./cmd/cronmail  ## scheduled notification-email dispatch (replaces the Node process_notifications cron)
	$(GO) build -o bin/collab ./cmd/collab  ## Yjs/Ygo collaboration service (ARC-9, D19)
	# (socket.io 0.9 / realtime bus RETIRED — the collab plane owns realtime)

.PHONY: go-test-cronmail
go-test-cronmail: ## cronmail gate (byte-exact oracle templates + claim/loop semantics)
	$(GO) build -buildvcs=false ./go/services/cronmail/... ./cmd/cronmail/ && $(GO) vet -buildvcs=false ./go/services/cronmail/... ./cmd/cronmail/ && test -z "$$(gofmt -l go/services/cronmail cmd/cronmail)" && $(GO) test -count=1 -race -buildvcs=false ./go/services/cronmail/...
.PHONY: go-test-collab
go-test-collab: ## collab gate (ARC-9: auth gate + CRDT convergence + persistence)
	$(GO) build -buildvcs=false ./go/services/collab/... ./cmd/collab/ && $(GO) vet -buildvcs=false ./go/services/collab/... ./cmd/collab/ && test -z "$$(gofmt -l go/services/collab cmd/collab)" && $(GO) test -count=1 -race -buildvcs=false ./go/services/collab/...
.PHONY: go-run-datamanipulator
go-run-datamanipulator: ## Run the datamanipulator CLI (dev)
	$(GO) run ./cmd/datamanipulator

.PHONY: go-run-filestore
go-run-filestore: ## Run the filestore Go service (dev)
	$(GO) run ./cmd/filestore

.PHONY: go-run-notifications
go-run-notifications: ## Run the notifications Go service (dev)
	$(GO) run ./cmd/notifications

.PHONY: go-run-chat
go-run-chat: ## Run the chat Go service (dev)
	$(GO) run ./cmd/chat

.PHONY: go-run-collab
go-run-collab: ## Run the Yjs/Ygo collaboration service (ARC-9, dev)
	$(GO) run ./cmd/collab

.DEFAULT_GOAL := help
