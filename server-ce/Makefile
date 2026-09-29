# Makefile

MONOREPO_ROOT := ../
HERE=$(shell pwd)
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

# Which base Dockerfile build-base uses. Alpine:3.24 is the CANONICAL base
# (cutover 2026-09-28, owner directive: alpine if it works — e2e gate GREEN,
# image smaller: 6.43GB vs 6.7GB ubuntu). The ubuntu file is retired in
# junk/Dockerfile-base-ubuntu26.04; override BASE_FILE to restore it.
BASE_FILE ?= Dockerfile-base

# Which Go builder the app image compiles on (cutover 2026-09-28: the musl
# alpine:3.24 builder is the default; the glibc one is retired in
# junk/images-golang-builder-amd64-ubuntu and can be passed back via
# GO_BUILDER_TAG if ever needed).
GO_BUILDER_TAG ?= ollitex/golang-builder-amd64-alpine:1.27.1


all: build-base build-community build-pandoc build-pdftocairo build-png2pdf

clean:
	-docker rmi --force $(OVERLEAF_BASE_TAG) $(OVERLEAF_TAG) $(OVERLEAF_PANDOC_TAG) $(OVERLEAF_PDFTOCAIRO_TAG) $(OVERLEAF_PNG2PDF_TAG)

refresh-cache: refresh-cache-branch
refresh-cache-branch:
	docker inspect $(OVERLEAF_BASE_BRANCH) > /dev/null && docker pull $(OVERLEAF_BASE_BRANCH) || true
	docker inspect $(OVERLEAF_BRANCH) > /dev/null && docker pull $(OVERLEAF_BRANCH) || true
	docker inspect $(OVERLEAF_PANDOC_BRANCH) > /dev/null && docker pull $(OVERLEAF_PANDOC_BRANCH) || true
	docker inspect $(OVERLEAF_PDFTOCAIRO_BRANCH) > /dev/null && docker pull $(OVERLEAF_PDFTOCAIRO_BRANCH) || true
	docker inspect $(OVERLEAF_PNG2PDF_BRANCH) > /dev/null && docker pull $(OVERLEAF_PNG2PDF_BRANCH) || true
refresh-cache: refresh-cache-latest
refresh-cache-latest:
	docker inspect $(OVERLEAF_BASE_LATEST) > /dev/null && docker pull $(OVERLEAF_BASE_LATEST) || true
	docker inspect $(OVERLEAF_LATEST) > /dev/null && docker pull $(OVERLEAF_LATEST) || true
	docker inspect $(OVERLEAF_PANDOC_LATEST) > /dev/null && docker pull $(OVERLEAF_PANDOC_LATEST) || true
	docker inspect $(OVERLEAF_PDFTOCAIRO_LATEST) > /dev/null && docker pull $(OVERLEAF_PDFTOCAIRO_LATEST) || true
	docker inspect $(OVERLEAF_PNG2PDF_LATEST) > /dev/null && docker pull $(OVERLEAF_PNG2PDF_LATEST) || true

build-base:
	cp .dockerignore $(MONOREPO_ROOT)
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --file $(BASE_FILE) \
	  --pull \
	  $(CACHE_FROM_BASE) \
	  --tag $(OVERLEAF_BASE_TAG) \
	  --tag $(OVERLEAF_BASE_BRANCH) \
	  --network=host \
	  $(MONOREPO_ROOT)


build-community:
	cp .dockerignore $(MONOREPO_ROOT)
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --build-arg OVERLEAF_BASE_TAG \
	  --build-arg GO_BUILDER_TAG \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  $(CACHE_FROM_COMMUNITY) \
	  --file Dockerfile \
	  --tag $(OVERLEAF_TAG) \
	  --tag $(OVERLEAF_BRANCH) \
	  --network=host \
	  $(MONOREPO_ROOT)

build-pandoc:
	cp .dockerignore $(MONOREPO_ROOT)
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --build-arg OVERLEAF_BASE_TAG \
	  --build-arg GO_BUILDER_TAG \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  $(CACHE_FROM_PANDOC) \
	  --file Dockerfile-pandoc \
	  --tag $(OVERLEAF_PANDOC_TAG) \
	  --tag $(OVERLEAF_PANDOC_BRANCH) \
	  --network=host \
	  $(MONOREPO_ROOT)

# pdftocairo replacement for quay.io/sharelatex/pdftocairo:24.02 (clsitex
# PDF→JPEG). Dockerfile-pdftocairo is standalone alpine with no COPY, so
# no .dockerignore needed.
build-pdftocairo:
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  $(CACHE_FROM_PDFTOCAIRO) \
	  --file Dockerfile-pdftocairo \
	  --tag $(OVERLEAF_PDFTOCAIRO_TAG) \
	  --tag $(OVERLEAF_PDFTOCAIRO_BRANCH) \
	  --network=host \
	  $(MONOREPO_ROOT)

# png2pdf replacement for quay.io/sharelatex/png2pdf:2026-06-24 (clsitex
# PNG->PDF slow-PNG optimisation). Dockerfile-png2pdf: python:3.14-alpine
# + img2pdf (pinned git commit) + the png2pdf.sh CLI shim (COPYs from
# server-ce/, so a .dockerignore at the root context is needed like the
# other COPY-based targets).
build-png2pdf:
	cp .dockerignore $(MONOREPO_ROOT)
	docker build \
	  --build-arg BUILDKIT_INLINE_CACHE=1 \
	  --progress=plain \
	  --label "com.overleaf.ce.revision=$(MONOREPO_REVISION)" \
	  $(CACHE_FROM_PNG2PDF) \
	  --file Dockerfile-png2pdf \
	  --tag $(OVERLEAF_PNG2PDF_TAG) \
	  --tag $(OVERLEAF_PNG2PDF_BRANCH) \
	  --network=host \
	  $(MONOREPO_ROOT)

# [V] git-bridge (D14): the Java service was deleted 2026-09-26 — the 1:1
# Go replacement builds via go/services/gitbridge/build.sh (ollitex/git-bridge).

push:
	docker push $(OVERLEAF_BASE_TAG)
	docker push $(OVERLEAF_BASE_BRANCH)
	docker push $(OVERLEAF_TAG)
	docker push $(OVERLEAF_BRANCH)
	docker push $(OVERLEAF_PANDOC_TAG)
	docker push $(OVERLEAF_PDFTOCAIRO_TAG)
	docker push $(OVERLEAF_PNG2PDF_TAG)

SHELLCHECK_OPTS = \
	--shell=bash \
	--external-sources \
	--exclude=SC1091
SHELLCHECK_COLOR := $(if $(CI),--color=never,--color)
SHELLCHECK_FILES := { git ls-files "*.sh" -z; git grep -Plz "\A\#\!.*bash"; } | sort -zu

shellcheck:
	@$(SHELLCHECK_FILES) | xargs -0 -r docker run --rm -v $(HERE):/mnt -w /mnt \
		koalaman/shellcheck:stable $(SHELLCHECK_OPTS) $(SHELLCHECK_COLOR)

shellcheck_fix:
	@$(SHELLCHECK_FILES) | while IFS= read -r -d '' file; do \
		diff=$$(docker run --rm -v $(HERE):/mnt -w /mnt koalaman/shellcheck:stable $(SHELLCHECK_OPTS) --format=diff "$$file" 2>/dev/null); \
		if [ -n "$$diff" ] && ! echo "$$diff" | patch -p1 >/dev/null 2>&1; then echo "\033[31m$$file\033[0m"; \
		elif [ -n "$$diff" ]; then echo "$$file"; \
		else echo "\033[2m$$file\033[0m"; fi \
	done

.PHONY: all \
	build-base build-community \
	build-pandoc build-pdftocairo \
	build-png2pdf \
	shellcheck shellcheck_fix
