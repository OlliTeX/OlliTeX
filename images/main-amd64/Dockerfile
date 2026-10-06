# syntax=docker/dockerfile:1-labs
# ---------------------------------------------
# Ollitex Community Edition (overleaf/overleaf)
# ---------------------------------------------
# Build-system re-arch (2026-09): the Go microservice binaries are compiled
# IN DOCKER on the ollitex Go builder (cgo-sqlite; musl alpine:3.24 builder
# since the 2026-09-28 cutover) instead of copied from the host
# (`COPY bin/`). The app base is alpine:3.24 (node 24 + runit + my_init) since
# the 2026-09-28 cutover (owner: alpine if it works; gate green + smaller
# image: 6.43GB vs 6.7GB); the ubuntu base file is retired in junk/. The Node
# services (clsi, …) and the webpack/pug
# front-end build still run here, on the base's node 24.

ARG OVERLEAF_BASE_TAG=ollitex/base:latest

# ALPINE SPIKE (2026-09-28, TODO da07f9fe): overridable Go builder tag so the
# same stage can compile the 17 Go services on the musl builder
# (ollitex/golang-builder-amd64-alpine:1.27.1) or the glibc one (default).
ARG GO_BUILDER_TAG=ollitex/golang-builder-amd64-alpine:1.27.1

# ---- Stage 1: compile the 12 Go services (ollitex Go builder, host cache) ----
FROM ${GO_BUILDER_TAG} AS gobuilder
WORKDIR /src
COPY go.mod go.sum ./
COPY go/ ./go/
COPY cmd/ ./cmd/
RUN --mount=type=cache,id=ol-go-mod,target=/gocache/mod \
    --mount=type=cache,id=ol-go-build,target=/gocache/build \
    set -e; mkdir -p /usr/local/bin/go-services; \
    for s in filestore notifications chat docstore web seaweed-migrate configdb cronmail collab historyv1 project-history clsitex clsitypst; do \
      CGO_ENABLED=1 GOOS=linux go build -o /usr/local/bin/go-services/$s ./cmd/$s; \
    done; \
    echo "built go-services: $(ls /usr/local/bin/go-services | tr '\n' ' ')"

# ---- Stage 2 (final): the app (ubuntu:26.04 base: node 24 + runit + my_init) ----
FROM $OVERLEAF_BASE_TAG

WORKDIR /overleaf

# Add required source files for yarn install
# -------------------------------------------
# (2026-10-05: services/* retired — the Node clsi_typst was the last child
# and the Go clsitypst conversion is live; the junk/services-web webpack
# build chain was CONSOLIDATED into frontend/ — owner directive 2026-10-05:
# NO junk/ content in the image.)
COPY --parents libraries/*/package.json .yarn/patches/ package.json frontend/package.json yarn.lock .yarnrc.yml .pnp.register.mjs /overleaf/
COPY images/main-amd64/genScript.js images/main-amd64/services.js /overleaf/

# Pre-install yarn via corepack so it is available at runtime for all users
# -------------------------------------------------------------------------
# Corepack setup, shared between all the images.
# (Note: the official Dockerfile sets no custom PATH in 6.3.0 — dependency resolution
# happens through the PnP runtime loaded at runtime via NODE_OPTIONS.)
ENV COREPACK_HOME=/opt/corepack
RUN corepack enable && corepack install -g yarn@4.18.0
ENV COREPACK_ENABLE_NETWORK=0

# Install yarn dependencies
# -------------------------
RUN --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/root/.yarn/berry/cache,id=build-images-yarn-cache \
    --mount=type=cache,target=/usr/local/share/.cache/yarn,id=build-images-yarn-fallback-cache \
    --mount=type=tmpfs,target=/tmp node genScript install | bash

# Add the actual source files
# P7 step 5/6 + frontend consolidation: the web app source (frontend/),
# locales/, and static public/ live at the MONOREPO ROOT. webpack runs from
# /overleaf/frontend (the consolidated build host) and emits bundles into
# ../public -> /overleaf/public; the Node+Go runtimes read /overleaf/locales.
# All three root dirs are copied alongside services/.
# (2026-10-05: services/ retired — the Go clsitypst service replaced the
# Node clsi_typst; junk/ is NO LONGER copied into the image — the webpack
# build chain now lives in frontend/.)
COPY --parents libraries/ docs/RELEASE_NOTES.md frontend/ locales/ public/ /overleaf/

# Pre-stage the PnP cache zips for the three git-sourced codemirror dev deps
# (byte-identical to the zips shipped in the official 6.3.0 image; checksums
# match yarn.lock, so the compile-time yarn install takes a cache hit and the
# parallel Yarn-Classic re-pack bootstrap -- which races on the shared classic
# yarn cache and fails under buildkit with 'cm-buildhelper: not found' (127) --
# is never triggered).
COPY .yarn/cache /overleaf/.yarn/cache

# GENERATION INTEGRITY (2026-09-26, F1 flip): the webpack/babel bundle
# caches are mounted as **tmpfs**, not persistent caches: a persistent cache
# carries module-state from whichever build dirtied it last (local + image
# lines intermixed) — a replay can restore an OLD generation of chunk files
# whose internal chunk IDs do not exist in the freshly built runtime
# (observed: login entry requiring chunk 1772/7189 against a runtime whose
# ID space has no such IDs → entry silently never executes → dead editor
# AND login). tmpfs = every compile starts from an empty cache, so each
# image always carries ONE self-consistent webpack generation, and the
# baked slot-resolver (public/manifest.json at serve time) needs nothing
# else. (Plain `rm -rf` on the mount point itself fails with "Device or
# resource busy" — a mounted dir cannot be removed, only its contents, and
# the contents of a persistent cache would just be re-served next time.)
RUN rm -rf /overleaf/public/js /overleaf/public/minjs /overleaf/public/stylesheets \
    /overleaf/public/manifest.json

RUN --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/root/.yarn/berry/cache,id=build-images-yarn-cache \
    --mount=type=cache,target=/usr/local/share/.cache/yarn,id=build-images-yarn-fallback-cache \
    --mount=type=tmpfs,target=/overleaf/frontend/node_modules/.cache \
    --mount=type=tmpfs,target=/overleaf/frontend/.cache \
    --mount=type=tmpfs,target=/tmp \
    node genScript compile | bash

# PnP: official 6.3.0 sets this after the build steps so runtime uses the PnP linker
# (quoted legacy form: Docker 29's parser mangles the JSON array form, and bare
# classic form cannot hold a multi-word value)
ENV NODE_OPTIONS="--require /overleaf/.pnp.cjs --import /overleaf/.pnp.register.mjs"

# Copy runit service startup scripts to its location
# --------------------------------------------------
ADD images/main-amd64/runit /etc/service

# P7 cutover (2026-09-24): after the Go web drop-in, `web-overleaf` and
# `web-api-overleaf` are the CANONICAL web services (they exec
# /usr/local/bin/go-services/web). The three `web-go-*` entries are A/B *shadow*
# experiments left over from the flip work — they must NOT be enabled by
# default (web-go-overleaf/web-go-flip run non-executable ./run → runit
# "access denied" crash-loop + log spam; web-go-api-overleaf is a redundant
# shadow instance). Their definitions are preserved under /etc/service.disabled
# for MANUAL re-enable (flips: images/main-amd64/nginx/flips/*) if a future A/B is
# needed.
RUN mkdir -p /etc/service.disabled \
 && for s in web-go-overleaf web-go-flip web-go-api-overleaf; do \
      [ -d /etc/service/$s ] && mv /etc/service/$s /etc/service.disabled/; \
    done

# Copy runit global settings to its location
# ------------------------------------------
ADD images/main-amd64/config/env.sh /etc/overleaf/env.sh

# Admin-managed env fragments (hub → Site → Storage etc.): www-data (the web
# service user) writes the fragments on save; every runit service sources
# them at start via /etc/overleaf/env.sh. Pre-created so no runtime fs race.
RUN mkdir -p /etc/overleaf/env.d && chown www-data:www-data /etc/overleaf/env.d && chmod 775 /etc/overleaf/env.d

# Baked Go-native service defaults (history flips, vendor data-plane, S3
# backends, CLSI working dirs) — ${VAR:-...}-guarded, compose env wins.
# Fragment number 30 keeps alphabetical/numeric ordering ahead of any
# operator-written runtime fragments.
ADD images/main-amd64/config/env.d/30-go-native-services.sh /etc/overleaf/env.d/30-go-native-services.sh

# CLSI working-dir base must exist inside the image layout (the data
# volume /var/lib/overleaf overlays this; pre-create for first boot).
RUN mkdir -p /var/lib/overleaf/data/compiles /var/lib/overleaf/data/output \
            /var/lib/overleaf/data/cache /var/lib/overleaf/data/uploads \
    && chown -R www-data:www-data /var/lib/overleaf/data \
    && chmod -R 775 /var/lib/overleaf/data

# Go microservice binaries — the CANONICAL implementations of the
# microservices (chat, docstore, filestore, notifications,
# linked-url-proxy); the Node versions were removed from the tree after the
# Node==Go contract gates went green (GO_CUTOVER_PLAN Phase C/D,
# 2026-09-16). Built on the host by `make go-build` (offline box — no
# golang stage in the image) and shipped whole; the runit scripts exec
# /usr/local/bin/go-services/<svc> unconditionally.
# 2026-09-29 (TPDS->web merge): webdavinterface/dropboxinterface/
# githubinterface are no longer standalone services — the Go web absorbs
# those surfaces in-process (features/webdav, features/dropbox, the tpds*
# routes); their runit dirs + cmds were junked (junk/runit-* + junk/cmd-*),
# the go/services/* packages remain as oracle references + contract tests.
# Go microservice binaries (chat, docstore, filestore, notifications,
# linked-url-proxy, web, seaweed-migrate, configdb) — the CANONICAL
# implementations. Now compiled in the `gobuilder` stage above (ollitex Go
# builder, host-persisted Go cache) — no host `make go-build` / `bin/` needed.
# The runit scripts exec /usr/local/bin/go-services/<svc> unconditionally.
# (webdav/dropbox/github interfaces: retired 2026-09-29 to in-web features;
# datamanipulator: library-only, no standalone service binary.)
COPY --from=gobuilder /usr/local/bin/go-services /usr/local/bin/go-services
RUN chmod 755 /usr/local/bin/go-services/*

# Configure nginx
# ---------------
ADD images/main-amd64/nginx/nginx.conf.template /etc/nginx/templates/nginx.conf.template
ADD images/main-amd64/nginx/overleaf.conf.template /etc/nginx/templates/overleaf.conf.template
ADD images/main-amd64/nginx/clsi-nginx.conf /etc/nginx/sites-enabled/clsi-nginx.conf
# Ollitex Go web flips (WEB_GO_PLAN.md): exact-path location blocks that
# the web-go-flip runit service injects into the overleaf vhost ONLY when
# FLIP_GO_WEB_P0=1 (default off → inert).
ADD images/main-amd64/nginx/flips /usr/local/share/overleaf-flips
RUN chmod 644 /usr/local/share/overleaf-flips/web-p0.conf
# B (owner queue 2026-09-30, def #1 hardening): this image runs the nginx
# workers as www-data (user directive), but the base image ships
# /var/lib/nginx owned nginx:nginx 700 → www-data cannot create the
# proxy/client-body temp files there → responses spilling past
# proxy_buffers (~32KB) FAIL intermittently (nginx [crit] Permission
# denied while reading upstream; client gets a 200 + partial body + full
# Content-Length = browser ERR_CONTENT_LENGTH_MISMATCH — the dead-editor
# defect). Give the runtime user ownership of the temp tree.
RUN chown -R www-data:www-data /var/lib/nginx && chmod 755 /var/lib/nginx


# Configure log rotation
# ----------------------
ADD images/main-amd64/logrotate/overleaf /etc/logrotate.d/overleaf
RUN chmod 644 /etc/logrotate.d/overleaf

# Configure cron tasks
# ----------------------
ADD images/main-amd64/cron /overleaf/cron
ADD images/main-amd64/config/crontab-history /etc/cron.d/crontab-history
RUN chmod 600 /etc/cron.d/crontab-history
ADD images/main-amd64/config/crontab-deletion /etc/cron.d/crontab-deletion
RUN chmod 600 /etc/cron.d/crontab-deletion
# [IV] notifications: email dispatch cron (see images/main-amd64/cron/*-notification*.sh)
ADD images/main-amd64/config/crontab-notifications /etc/cron.d/crontab-notifications
RUN chmod 600 /etc/cron.d/crontab-notifications
# audit 002 (2026-09-30): instance-stats collector crontab (was never baked —
# the collector route also had to be re-implemented on the Go private API;
# see go/services/web/features/instancestats/collector.go). The images/main-amd64/runit/cron
# service (copied by the `ADD images/main-amd64/runit /etc/service` line above) keeps
# the cron DAEMON alive — it never ran in the image, which is why the crons
# (this one + history + deletion + notifications) were all dead.
ADD images/main-amd64/config/crontab-instance-stats /etc/cron.d/crontab-instance-stats
RUN chmod 600 /etc/cron.d/crontab-instance-stats

# Copy Phusion Image startup and shutdown scripts to their locations
# ------------------------------------------------------------------
COPY images/main-amd64/init_scripts/ /etc/my_init.d/
COPY images/main-amd64/init_preshutdown_scripts/ /etc/my_init.pre_shutdown.d/

# Copy app settings files
# -----------------------
COPY images/main-amd64/config/settings.js /etc/overleaf/settings.js

# (historical: Node history-v1 config COPY — retired with the Node
# history-v1/project-history/document-updater trees; Go services own
# :3100/:3054 now and carry their own config.)

# Copy grunt thin wrapper
# -----------------------
ADD images/main-amd64/grunt /usr/local/bin/grunt
RUN chmod +x /usr/local/bin/grunt

# Copy history helper scripts
# ---------------------------

# Copy Latexmkrc
# -----------------------
COPY images/main-amd64/config/latexmkrc /usr/local/share/latexmk/LatexMk

# File that controls open|closed status of the site
# -------------------------------------------------
ENV SITE_MAINTENANCE_FILE="/etc/overleaf/site_status"
RUN touch $SITE_MAINTENANCE_FILE

# Set Environment Variables
# --------------------------------
ENV OVERLEAF_CONFIG=/etc/overleaf/settings.js

ENV WEB_API_USER="overleaf"
ENV ADMIN_PRIVILEGE_AVAILABLE="true"

ENV OVERLEAF_APP_NAME="OlliTeX"

ENV OPTIMISE_PDF="true"
# Official 6.3.0 default for @overleaf/request-validation
ENV REQ_VALIDATION_MODE=enforce-log

# Phusion Image timeouts before sending SIGKILL to processes
# ----------------------------------------------------------
ENV KILL_PROCESS_TIMEOUT=55
ENV KILL_ALL_PROCESSES_TIMEOUT=55
ENV GRACEFUL_SHUTDOWN_DELAY_SECONDS=1

# DataManipulator runs as www-data and needs a writable project-tree root.
RUN install -d -o www-data -g www-data -m 700 /projects

ENV NODE_ENV="production"
ENV LOG_LEVEL="info"

ENV GIT_BRIDGE_HOST=git-bridge
ENV GIT_BRIDGE_PORT=8000
ENV GIT_BRIDGE_REPOSTORE_MAX_FILE_SIZE=52428800
# tex-autoformatter module: tex-fmt binary (vendored static x86_64 build,
# v0.5.7; see images/main-amd64/static/bin/README.md for the SHA256 provenance).
# Installed after the app copy so it persists regardless of node_modules
# reinstalls.
COPY images/main-amd64/static/bin/tex-fmt /usr/local/bin/tex-fmt
RUN chmod 755 /usr/local/bin/tex-fmt

# CLSI word count: vendored texcount (perl script + wrapper). Runs on the
# Alpine perl (perl-core deps only). See images/main-amd64/static/bin/README.md.
COPY images/main-amd64/static/bin/texcount /usr/bin/texcount
COPY images/main-amd64/static/bin/texcount.pl /usr/bin/texcount.pl
RUN chmod 755 /usr/bin/texcount && chmod 644 /usr/bin/texcount.pl

EXPOSE 80

ENTRYPOINT ["/sbin/my_init"]
