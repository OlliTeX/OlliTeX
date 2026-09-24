#!/usr/bin/env bash
# Build the Go git-bridge binary + service image.
#
#   usage: build.sh [image-tag]        # default tag: gitbridge-go:latest
#
# Steps:
#   1. `go build` the git_bridge binary (module ollitex, cmd/gitbridge) into a
#      throwaway build context.
#   2. Copy the sibling Dockerfile (node:24 glibc+git base) into the context.
#   3. `docker build` the context into the tag.
#
# The image is config-agnostic: at run time mount a runtime.json (the same
# schema as the Java runtime.json) at /conf/runtime.json. See
# runtime.json.example for the expected shape.
set -euo pipefail
cd "$(dirname "$0")/../../.."                # -> monorepo root (go/services/gitbridge -> root)
TAG="${1:-gitbridge-go:latest}"

CTX="$(mktemp -d)"
trap 'rm -rf "$CTX"' EXIT

echo ">> building git_bridge binary (module ollitex)"
go build -o "$CTX/git_bridge" ./cmd/gitbridge

echo ">> staging build context"
cp go/services/gitbridge/Dockerfile "$CTX/Dockerfile"

echo ">> docker build -> $TAG"
docker build --progress=plain -t "$TAG" "$CTX"

echo ">> done: $TAG (config-agnostic; mount a runtime.json at /conf/runtime.json to run)"
docker image inspect "$TAG" --format 'image: {{.Id}}  size: {{.Size}}' || true
