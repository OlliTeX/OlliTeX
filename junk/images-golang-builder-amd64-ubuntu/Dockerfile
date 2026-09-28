# ============================================================================
# ollitex/golang-builder-amd64 — Go 1.27.1 build toolchain for Ollitex.
#
# Base = ubuntu:26.04 (the SAME Ubuntu series as all Ollitex runtime images),
# so cgo binaries (mattn/go-sqlite3 is linked by cmd/web, cmd/configdb and
# the git-bridge) are compiled against the SAME glibc (2.43) the runtime
# uses. No host Go toolchain is required to build images.
#
# Repeated builds reuse a HOST-PERSISTENT module + build cache (BuildKit):
#     --mount=type=cache,target=/gocache/mod,id=ol-go-mod
#     --mount=type=cache,target=/gocache/build,id=ol-go-build
# So `go mod download` runs once per dependency (offline-tolerant after the
# first build; no vendoring).
#
# Local tag: ollitex/golang-builder-amd64:1.27.1  (+ :latest)
# Push (owner): docker push ollitex/golang-builder-amd64
# ============================================================================
FROM ubuntu:26.04

# cgo needs a C toolchain + libc headers; basic tools for source fetches.
# --no-install-recommends keeps the image lean.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates curl git gcc g++ libc6-dev make pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Exact Go 1.27.1 toolchain, sourced from the official image (pinned, so the
# toolchain matches go.mod's `go 1.27` exactly — no runtime toolchain swap).
COPY --from=golang:1.27.1-bookworm /usr/local/go /usr/local/go

ENV PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    GOTOOLCHAIN=local \
    CGO_ENABLED=1 \
    GOMODCACHE=/gocache/mod \
    GOCACHE=/gocache/build \
    GOFLAGS=-mod=mod

# Conventional, host-cache-mountable locations (see the --mount=type=cache
# targets used by the server-ce build so repeated builds stay fast offline).
RUN mkdir -p /gocache/mod /gocache/build

WORKDIR /build
# Default: keep the container for `docker run … go build …` or override CMD.
CMD ["sh"]
