# OlliTeX — forked/rebuilt Typst compiler image (D21).
#
#   build stage:   rust:alpine3.24 (musl, fully static), typst cloned from
#                  github (HTTPS, pinned SHA below) at the repo root into
#                  /app — the repo IS /app, no subdirectory.
#   runtime stage: alpine:3.24, static binary at /usr/local/bin/typst — the
#                  SAME path clsi_typst.go expects (it overrides the container
#                  PATH to include /usr/local/bin) and the path the vendored
#                  pandoc/typst image uses, so numeric uid/gid 33:33 runs it
#                  directly.
FROM rust:alpine3.24 AS build

# build-base = gcc, make, musl-dev; static linking -> no runtime shared deps.
RUN apk add --no-cache \
        build-base \
        pkgconf \
        cmake \
        git \
        ca-certificates \
        musl-dev \
        openssl-dev \
        openssl-libs-static \
        clang \
        lld

ENV CARGO_REGISTRIES_CRATES_IO_PROTOCOL=sparse

# Pin the fork base (0.15.1-with-extras tip) EXACTLY: a later upstream branch
# move must not silently change the build. Clone by SHA from the branch (the
# ref must exist first; the SHA is then verified below and checked out).
# HTTPS (no deploy key exists in the docker build context); the clone is
# fetched at build time, nothing is vendored in the repo.
ARG TYPST_REF=d101c2f507acd2e680a187735c5c201094e00077
RUN git clone --single-branch --branch 0.15.1-with-extras \
        https://github.com/typst/typst.git /app \
    && git -C /app rev-parse HEAD | grep -q "^${TYPST_REF}\$" \
    && git -C /app checkout -q "$TYPST_REF"
WORKDIR /app

# CLSI fork (D21): apply the sourcemap-sidecar patch before building.
# The vendored tree is a pure checkout (0.15.1-with-extras); every
# divergence from vanilla is in the single patch file at the image root.
# Overleaf build context is the REPO ROOT (root Makefile, like pandoc/pdftocairo/
# png2pdf), so the patch path is root-relative here (in the pre-golang staging
# area it was context-relative `patches/...` — see the source handoff).
COPY images/typst-amd64/patches/0001-clsi-sourcemap.patch /tmp/sourcemap.patch
RUN git -C /app apply /tmp/sourcemap.patch\
    && rm /tmp/sourcemap.patch

RUN --mount=type=cache,target=/root/.cargo/git/db \
    --mount=type=cache,target=/root/.cargo/registry/cache \
    --mount=type=cache,target=/root/.cargo/registry/index \
    CARGO_REGISTRIES_CRATES_IO_PROTOCOL=sparse \
    cargo fetch

RUN --mount=type=cache,target=/root/.cargo/git/db \
    --mount=type=cache,target=/root/.cargo/registry/cache \
    --mount=type=cache,target=/root/.cargo/registry/index \
    OPENSSL_NO_PKG_CONFIG=1 OPENSSL_STATIC=1 \
    OPENSSL_DIR="/usr" \
    cargo build -p typst-cli --release

# ---------------------------------------------------------------------------
# Runtime
# ---------------------------------------------------------------------------
FROM alpine:3.24

# Non-root `typst` user (uid/gid 1000) for `docker run --user typst` debugs;
# clsi runs containers as numeric uid/gid 33:33 (see config.go DockerUser).
RUN addgroup -g 1000 typst && \
    adduser -D -u 1000 -G typst typst

LABEL org.opencontainers.image.source="https://github.com/typst/typst"
LABEL org.opencontainers.image.description="OlliTeX Typst compiler (fork at v0.15.1, D21 sourcemap extension)"
LABEL ollitex.typst.pin="v0.15.1"
LABEL ollitex.typst.forum="D21 sourcemap sidecar (output.sourcemap.json)"

# The static binary needs no shared libraries (musl, static).
COPY --from=build /app/target/release/typst /usr/local/bin/typst
RUN chmod 0555 /usr/local/bin/typst

ENV PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

# NOTE: clsi_typst.go drives this image with its OWN entrypoint
# (/bin/sh -c '... typst compile ...'), overriding the image ENTRYPOINT.
# We set a sensible default anyway for interactive debugging.
ENTRYPOINT [ "/usr/local/bin/typst" ]
CMD [ "--help" ]
