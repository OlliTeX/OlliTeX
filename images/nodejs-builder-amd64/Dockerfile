# ============================================================================
# ollitex/nodejs-builder-amd64 — Node.js 24.18.1 + Yarn 4.18 build toolchain
# for the Ollitex web frontend (webpack, Pug precompile, native node modules).
#
# Base = ubuntu:26.04 (same series as the runtime images) so any compiled
# native module is linked against the runtime libc. Node 24.18.1 satisfies
# the repo's engines `node >= 24.18.1`. No host Node toolchain required.
#
# Local tag: ollitex/nodejs-builder-amd64:24.18.1  (+ :latest)
# Push (owner): docker push ollitex/nodejs-builder-amd64
# ============================================================================
FROM ubuntu:26.04

# C toolchain + python3/make for native node modules (canvas, node-sass,
# sqlite3, sharp, …). --no-install-recommends keeps the image lean.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates curl git gnupg build-essential python3 make g++ \
    && rm -rf /var/lib/apt/lists/*

# Exact Node 24.18.1 (>= 24.18.1), sourced from the official image.
# NOTE: /usr/local/bin/{npm,npx,corepack} in the node image are SYMLINKS into
# ../lib/node_modules/… whose entrypoints use RELATIVE requires. So copy the
# whole node_modules (structure preserved) + the node binary + headers, then
# recreate the bin symlinks — never COPY the dereferenced bin shims.
COPY --from=node:24.18 /usr/local/bin/node    /usr/local/bin/node
COPY --from=node:24.18 /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=node:24.18 /usr/local/include/node    /usr/local/include/node

ENV PATH=/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin \
    npm_config_update_notifier=false \
    YARN_ENABLE_GLOBAL_CACHE=false

# Recreate the bin symlinks (resolve inside node_modules) + activate a pinned
# Yarn 4.18.0 via corepack (the node image ships corepack 0.35 under
# node_modules/corepack). `corepack prepare` fetches Yarn once.
RUN ln -sf ../lib/node_modules/npm/bin/npm-cli.js        /usr/local/bin/npm \
    && ln -sf ../lib/node_modules/npm/bin/npx-cli.js      /usr/local/bin/npx \
    && ln -sf ../lib/node_modules/corepack/dist/corepack.js /usr/local/bin/corepack \
    && ln -sf /usr/local/bin/node      /usr/bin/node \
    && ln -sf /usr/local/bin/npm       /usr/bin/npm \
    && ln -sf /usr/local/bin/npx       /usr/bin/npx \
    && ln -sf /usr/local/bin/corepack  /usr/bin/corepack \
    && corepack enable \
    && corepack prepare yarn@4.18.0 --activate \
    && node -v && npm -v && yarn -v

WORKDIR /src
CMD ["sh"]
