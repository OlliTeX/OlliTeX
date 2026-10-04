# OlliTeX Toolkit TUI — single static binary.
#
# Host contract (owner directive 2026-10-06): the host only needs
#   1. docker support (the docker socket, mounted into this container),
#   2. one mounted local folder (the data dir: host key, config backups,
#      journal),
#   3. the config store DSN (Postgres — the single source of truth).
# No docker env settings plane: settings live in the config store.
#
# Community/SaaS split: removed — this models THIS stack's features.
#
# Build: golang builder (musl alpine) → alpine:3.24 runtime.

ARG GO_BUILDER_TAG=ollitex/golang-builder-amd64-alpine:1.27.1
FROM ${GO_BUILDER_TAG} AS go

ARG MONOREPO_REVISION=dev
WORKDIR /src

# Module graph first (layer cache), then sources.
COPY go.mod go.sum ./
RUN go mod download

COPY go/ go/
COPY cmd/ cmd/
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -trimpath \
      -ldflags "-s -w -X main.ver=${MONOREPO_REVISION}" \
      -o /out/toolkit ./cmd/toolkit \
 && go build -trimpath \
      -ldflags "-s -w -X main.ver=${MONOREPO_REVISION}" \
      -o /out/configdb ./cmd/configdb

# ---- runtime: alpine 3.24 ---------------------------------------------------
FROM alpine:3.24

ARG MONOREPO_REVISION=dev
LABEL com.overleaf.ce.revision="${MONOREPO_REVISION}"

# TUI binary + the config CLI (same config store — the emergency/offline
# path stays one `docker exec` away).
COPY --from=go /out/toolkit /usr/local/bin/toolkit
COPY --from=go /out/configdb /usr/local/bin/configdb

# The official docker CLI + compose plugin: orchestration of the stack
# talks to the MOUNTED docker socket (the host provides it).
RUN apk add --no-cache \
      ca-certificates \
      docker-cli \
      docker-cli-compose \
      tzdata

# The stack compose templates live next to the binary (absorbed from
# toolkit/lib; the TUI reads + renders them — no env file fallbacks).
COPY toolkit/lib/ /opt/ollitex/compose-templates/

# The one mounted folder (default; override --data-dir / OLLITEX_TOOLKIT_DATA_DIR).
ENV OLLITEX_TOOLKIT_DATA_DIR=/opt/ollitex/data \
    OLLITEX_TOOLKIT_COMPOSE_FILE=/opt/ollitex/compose-templates/docker-compose.base.yml \
    OLLITEX_TOOLKIT_PROJECT=ollitex \
    OLLITEX_TOOLKIT_SSH_LISTEN=:2222 \
    OLLITEX_TOOLKIT_USER=ollitex

# Local admin tool: runs as root so the MOUNTED docker socket (root-owned on
# hosts) is readable without group-adding; the single SSH user is 'ollitex'
# (auth is gated in-process — no host accounts involved).
RUN install -d /opt/ollitex/data

EXPOSE 2222

# ssh into the host with: ssh -p 2222 ollitex@<host>
# (password: OLLITEX_TOOLKIT_PASSWORD) — then drive the TUI.
ENTRYPOINT ["/usr/local/bin/toolkit"]
CMD ["serve"]
