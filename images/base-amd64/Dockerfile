# --------------------------------------------------
# Ollitex Base Image — CANONICAL (alpine:3.24). Cutover 2026-09-28 (owner:
# "if alpine works, cutover and retire the ubuntu version"): the alpine
# variant passed the full e2e gate on the live stack (smoke incl. compile→PDF
# via the sandbox, u101 history contract + 2-run stability, hub-storage-admin,
# consent, collab-yjs, realtime-bus) at a SMALLER image (6.43GB vs 6.7GB).
# The ubuntu:26.04 file is retired in junk/Dockerfile-base-ubuntu26.04.
#
# Feasibility probes (2026-09-28, both GREEN on alpine:3.24 / node:24-alpine):
#   - Go CGO (mattn/go-sqlite3, cmd/configdb) builds with gcc+musl-dev ✓
#   - node native deps bcrypt@6 + utf-8-validate@5 build via node-gyp AND
#     verify at runtime (bcrypt.compareSync true, utf-8-validate true) ✓
#   - alpine:3.24 ships nodejs-24.18.1 exactly = our engines floor
#     (package.json "node": ">=24.18.1") — no nodesource repo needed.
#
# Differences from the ubuntu base (intentional):
#   - no unattended-upgrades (build-time `apk update` only)
#   - no gnupg (no deb.nodesource GPG dearmor)
#   - python-is-python3 → explicit /usr/bin/python symlink
#   - my_init (images/base-amd64/my_init) requires python3 stdlib only — OK.
#   - runit from the `runit` apk package (sv/runsv/runsvdir) instead of
#     Debian's `runit` (runit-helper phusion layout). my_init is stdlib
#     python and drives runsvdir the same way.
#   - `time` provided by coreutils-style `time` package; `bsdmainutils`
#     Debian-only utilities are NOT used by OlliTeX runit scripts (verified:
#     scripts use /sbin/setuser, sv, bash) — not mapped.
#   - glibc-isms to WATCH in the e2e gate (spot-checks in the spike):
#       * bcrypt native path (login) — probe green
#       * utf-8-validate websocket path — probe green
#       * configdb/gitbridge CGO sqlite — probe green
#       * poppler-utils (pdftotext), optipng, qpdf binaries present
#       * runit service supervision under busybox sh (sv/runsvdir semantics)
# --------------------------------------------------
FROM alpine:3.24

ENV REBUILT_AFTER="2026-09-28-alpine-spike"

# Install dependencies (apk mapping of the ubuntu apt list)
# --------------------
RUN apk update \
    && apk add --no-cache \
        build-base \
        wget curl \
        net-tools unzip time \
        poppler-utils \
        optipng \
        strace \
        nginx \
        git \
        python3 \
        zlib-dev pcre2-dev \
        gettext \
        perl-libwww \
        ca-certificates \
        qpdf \
        runit \
        util-linux \
        nodejs \
        npm \
        bash \
    \
    && rm -rf /var/cache/apk/* \
    && ln -sf /usr/bin/python3 /usr/bin/python \
    && update-ca-certificates \
    && mkdir -p /etc/nginx/templates /etc/nginx/sites-enabled /etc/nginx/conf.d

# corepack + yarn parity with the ubuntu base (nodesource node bundles both;
# the alpine nodejs package ships NEITHER). Install the same corepack +
# yarn@4.18.0 into the SAME COREPACK_HOME the app Dockerfile uses, so the
# app-stage `corepack enable && corepack install -g yarn@4.18.0` is an
# idempotent no-op (yarn already present in the home, offline-tolerant).
ENV COREPACK_HOME=/opt/corepack
RUN npm i -g --no-audit --no-fund corepack \
    && corepack enable \
    && corepack install -g yarn@4.18.0 \
    && yarn --version

# Set up overleaf user and home directory. Alpine's nginx package PRE-CREATES
# the www-data group (gid 82) but no user — so only the user is created
# (uid 33 parity with ubuntu). Primary group defaults to nogroup, which is
# HARMLESS: the vendored /sbin/setuser drops privileges via
# `setpriv --reuid www-data --regid www-data --init-groups` (proven 2026-09-28:
# uid=33, gid=82, env preserved, files land www-data:www-data).
#
# util-linux (in the apk list above): REQUIRED — busybox's setpriv applet
# does NOT support --reuid/--regid (verified: "unrecognized option: reuid"),
# and every runit run script execs /sbin/setuser. The real util-linux setpriv
# wins PATH over the busybox applet (proven).
RUN adduser -S -D -u 33 -s /sbin/nologin www-data \
    && adduser -S -D -h /overleaf -s /sbin/nologin overleaf \
    && mkdir -p /var/lib/overleaf /var/log/overleaf /var/lib/overleaf/data/template_files \
    && chown -R www-data:www-data /var/lib/overleaf /var/log/overleaf

# Process manager entrypoint + lifecycle dirs (same vendored my_init as the
# ubuntu base — stdlib-only python3, drives runsvdir over /etc/service and
# the /etc/my_init*.d hooks the app relies on).
COPY images/base-amd64/my_init /sbin/my_init
RUN chmod 755 /sbin/my_init \
    && mkdir -p /etc/service /etc/my_init.d /etc/my_init.pre_shutdown.d /etc/my_init.post_shutdown.d \
    && install -d -m 700 -o root -g root /etc/container_environment

# phusion-compatible privilege-drop helper (POSIX sh — portable across bases).
COPY images/base-amd64/setuser /sbin/setuser
RUN chmod 755 /sbin/setuser

# TeX Live: intentionally NOT installed (owner #10, 2026-09-16) — same as
# the ubuntu base; sandboxed compiles run in the external texlive image.
