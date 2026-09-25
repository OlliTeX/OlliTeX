#!/usr/bin/env bash
# Build the Go git-bridge service image (multi-stage):
#   1. gobuilder stage — ollitex Go builder (ubuntu:26.04 glibc + cgo) compiles
#      cmd/gitbridge (module ollitex) with a host-persisted Go cache.
#   2. runtime — ubuntu:26.04 + git (the bridge shells out to git) + the binary.
#
#   usage: build.sh [image-tag]        # default tag: ollitex/git-bridge:latest
#
# The image is config-agnostic: at run time mount a runtime.json (the same
# schema as the Java runtime.json) at /conf/runtime.json. See
# runtime.json.example for the expected shape.
set -euo pipefail
cd "$(dirname "$0")/../../.."                # -> monorepo root
TAG="${1:-ollitex/git-bridge:latest}"

echo ">> docker build -> $TAG (build context: monorepo root)"
DOCKER_BUILDKIT=1 docker build --progress=plain \
  --build-arg BUILDKIT_INLINE_CACHE=1 \
  --file go/services/gitbridge/Dockerfile -t "$TAG" .

echo ">> done: $TAG (config-agnostic; mount a runtime.json at /conf/runtime.json to run)"
docker image inspect "$TAG" --format 'image: {{.Id}}  size: {{.Size}}' || true
