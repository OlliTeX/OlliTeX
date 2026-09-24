# Ollitex Go git-bridge — the 1:1 Go replacement of the Java git-bridge
# (sharelatex/git-bridge). The service image is a thin wrapper: a glibc+git
# runtime base + the statically-linked-against-glibc Go binary (cmd/gitbridge).
#
#   * base: node:24 (Debian bookworm) — provides glibc (the Go binary links
#     glibc + cgo-sqlite) and `git` 2.39, which the bridge shells out to for
#     the smart-HTTP write path. No Maven/Java toolchain, no dependence on the
#     overleaf image, standard upstream tag.
#   * config: the Git-bridge reads /conf/runtime.json (the SAME schema as the
#     Java runtime.json). This image is config-agnostic — the deployment must
#     provide it by mounting a runtime.json at /conf/runtime.json (see
#     go/services/gitbridge/runtime.json.example and the e2e compose).
#
# Build:  go/services/gitbridge/build.sh [image-tag]   (default gitbridge-go:latest)
# Run:    git_bridge serve /conf/runtime.json
# Rollback: point the deployment back at sharelatex/git-bridge (<ver>) + its
#          /start.sh command (the Java image) — the Go image is a separate tag.
FROM node:24

COPY git_bridge /usr/local/bin/git_bridge
RUN chmod 755 /usr/local/bin/git_bridge

# runtime.json must be present at /conf (deployment-provided by default).
ENV GIT_BRIDGE_CONFIG=/conf/runtime.json
ENTRYPOINT []
CMD ["git_bridge", "serve", "/conf/runtime.json"]
EXPOSE 8000
