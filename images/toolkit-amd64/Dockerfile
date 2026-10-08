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
# openssh-client: ssh-keygen for the key-pair bootstrap (owner directive:
# keys over passwords — ${PREFIX}/ssh mounted at /opt/ollitex/ssh), and
# the git/ssh clients for backup actions.
RUN apk add --no-cache \
      ca-certificates \
      docker-cli \
      docker-cli-compose \
      openssh-client \
      ncurses-libs \
      tzdata \
      unzip \
      wget

# The stack compose templates live next to the binary (absorbed from
# toolkit/lib; the TUI reads + renders them — no env file fallbacks).
# owner E 2026-10-06: the canonical stack file is IN the image (the TUI
# runner plans/operates it from /opt/ollitex); the ONE lib survivor is the
# monitoring bundles, laid out where materializeMonitoring expects them
# (the compose-file neighbor: /opt/ollitex/monitoring).
COPY toolkit/compose.yaml /opt/ollitex/compose.yaml
COPY toolkit/lib/monitoring/ /opt/ollitex/monitoring/

# The data-backup suite (owner: "for these I have no backup strategy yet" —
# now they do): toolkit/backup is the ONE backup surface, baked where the TUI
# can drive it (d = data backup, v = restore drill). The ${PREFIX}/data bind
# is /opt/ollitex/data, so BACKUP_ROOT resolves to the same host directory
# from both the container and the host.
COPY toolkit/backup/ /opt/ollitex/backup/
RUN chmod +x /opt/ollitex/backup/*.sh


# The one mounted folder (default; override --data-dir / OLLITEX_TOOLKIT_DATA_DIR).
ENV OLLITEX_TOOLKIT_DATA_DIR=/opt/ollitex/data \
    OLLITEX_TOOLKIT_COMPOSE_FILE=/opt/ollitex/compose.yaml \
    OLLITEX_TOOLKIT_PROJECT=ollitex \
    OLLITEX_TOOLKIT_SSH_LISTEN=:2222 \
    OLLITEX_TOOLKIT_USER=ollitex

# Local admin tool: runs as root so the MOUNTED docker socket (root-owned on
# hosts) is readable without group-adding; the single SSH user is 'ollitex'
# (auth is gated in-process — no host accounts involved).
RUN install -d /opt/ollitex/data

# Self-healing SSH key bootstrap (owner flow: key pair under ${PREFIX}/ssh):
# if the authorized_keys file/dir is empty, generate an ed25519 pair right
# there before serving — a fresh stack is key-ready with zero manual steps,
# and existing keys are never touched. (In-image so it survives one-shot
# container state and re-runs on every start.)
RUN printf '%s\n' \
    '#!/bin/sh' \
    'KFILE="${OLLITEX_TOOLKIT_SSH_KEYFILE:-/opt/ollitex/ssh/authorized_keys}"' \
    'KDIR=$(dirname "$KFILE")' \
    'if [ ! -f "$KFILE" ] && [ -z "$(ls "$KDIR" 2>/dev/null)" ]; then' \
    '  mkdir -p "$KDIR" && umask 077' \
    '  ssh-keygen -t ed25519 -f "$KDIR/id_ed25519" -N "" -C "ollitex-toolkit-bootstrap" >/dev/null 2>&1' \
    '  cp "$KDIR/id_ed25519.pub" "$KFILE"' \
    '  echo "toolkit: generated SSH key pair under $KDIR (ssh -i $KDIR/id_ed25519 ollitex@host)"' \
    'fi' \
    'exec /usr/local/bin/toolkit "$@"' \
    > /usr/local/bin/toolkit-entrypoint \
 && chmod 755 /usr/local/bin/toolkit-entrypoint

EXPOSE 2222

# ssh into the host with: ssh -p 2222 -i ${PREFIX}/ssh/id_ed25519 ollitex@<host>
# (publickey auth; a password may still be added via OLLITEX_TOOLKIT_PASSWORD
#  — the auth gate is fail-closed: no method active ⇒ no service.)
ENTRYPOINT ["/usr/local/bin/toolkit-entrypoint"]
CMD ["serve"]
