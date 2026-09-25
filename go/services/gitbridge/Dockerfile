# Ollitex Go git-bridge — the 1:1 Go replacement of the Java git-bridge
# (sharelatex/git-bridge).
#
#   * gobuilder stage (ollitex Go builder — ubuntu:26.04 glibc + cgo toolchain):
#     compiles cmd/gitbridge (self-contained in go/services/gitbridge +
#     cmd/gitbridge; links mattn/go-sqlite3 via cgo). Host-persisted Go cache
#     via --mount=type=cache (ids shared with the server build: ol-go-mod /
#     ol-go-build).
#   * runtime (ubuntu:26.04, glibc — matches the binary): + git (the bridge
#     shells out to git for the smart-HTTP write path) + ca-certificates.
#
#   * config: the bridge reads /conf/runtime.json (SAME schema as the Java
#     runtime.json). The image is config-agnostic — the deployment mounts a
#     runtime.json at /conf/runtime.json (see runtime.json.example + e2e).
#
# Build:  go/services/gitbridge/build.sh [image-tag]   (context = monorepo root)
# Run:    git_bridge serve /conf/runtime.json
# Registry: FROM ollitex/golang-builder-amd64 — local build uses the local tag;
#           the same name resolves from Docker Hub once ollitex/* is pushed.
FROM ollitex/golang-builder-amd64:1.27.1 AS gobuilder
WORKDIR /src
COPY go.mod go.sum ./
COPY go/services/gitbridge/ ./go/services/gitbridge/
COPY cmd/gitbridge/ ./cmd/gitbridge/
RUN --mount=type=cache,id=ol-go-mod,target=/gocache/mod \
    --mount=type=cache,id=ol-go-build,target=/gocache/build \
    CGO_ENABLED=1 GOOS=linux go build -o /out/git_bridge ./cmd/gitbridge

# Runtime: ubuntu:26.04 + git (glibc base matches the cgo-sqlite binary).
FROM ubuntu:26.04
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=gobuilder /out/git_bridge /usr/local/bin/git_bridge
ENV GIT_BRIDGE_CONFIG=/conf/runtime.json
ENTRYPOINT []
CMD ["git_bridge", "serve", "/conf/runtime.json"]
EXPOSE 8000
