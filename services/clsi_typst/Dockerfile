# This file is a mirror of services/clsi/Dockerfile (auto-generated upstream);
# for clsi_typst the stage is trimmed: no texlive apt stage, and env points at
# clsi_typst's own dirs. Build from the repo root:
#   docker build -f services/clsi_typst/Dockerfile -t clsi_typst .
#
# Run (dev / acceptance):
#   docker run -it --rm \
#     -v /var/run/docker.sock:/var/run/docker.sock:ro \
#     -e SANDBOXED_COMPILES=true \
#     -e TYPST_DOCKER_IMAGE=pandoc/typst:3-alpine \
#     clsi_typst

FROM node:24.18.1 AS base

# Corepack setup, shared between all the images.
ENV COREPACK_HOME=/opt/corepack
RUN corepack enable && corepack install -g yarn@4.18.0
ENV COREPACK_ENABLE_NETWORK=0

WORKDIR /overleaf/services/clsi_typst
COPY services/clsi_typst/install_deps.sh /overleaf/services/clsi_typst/
RUN chmod 0755 ./install_deps.sh && ./install_deps.sh
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]
COPY services/clsi_typst/entrypoint.sh /

RUN mkdir /home/node/.config && chown node:node /home/node/.config

FROM base AS app

# Keep the workspace manifest + libs + this service identical to what the
# pnp install resolves against at runtime (mirrors clsi's Dockerfile).
COPY package.json yarn.lock .yarnrc.yml .pnp.register.mjs /overleaf/
COPY libraries/eslint-plugin/package.json /overleaf/libraries/eslint-plugin/package.json
COPY libraries/fetch-utils/package.json /overleaf/libraries/fetch-utils/package.json
COPY libraries/logger/package.json /overleaf/libraries/logger/package.json
COPY libraries/metrics/package.json /overleaf/libraries/metrics/package.json
COPY libraries/o-error/package.json /overleaf/libraries/o-error/package.json
COPY libraries/overleaf-editor-core/package.json /overleaf/libraries/overleaf-editor-core/package.json
COPY libraries/promise-utils/package.json /overleaf/libraries/promise-utils/package.json
COPY libraries/settings/package.json /overleaf/libraries/settings/package.json
COPY libraries/stream-utils/package.json /overleaf/libraries/stream-utils/package.json
COPY libraries/validation-tools/package.json /overleaf/libraries/validation-tools/package.json
COPY services/clsi/package.json /overleaf/services/clsi/package.json
COPY services/clsi_typst/package.json /overleaf/services/clsi_typst/package.json
COPY .yarn/patches/ /overleaf/.yarn/patches/
RUN cd /overleaf && yarn workspaces focus @overleaf/clsi @overleaf/clsi-typst overleaf
COPY libraries/eslint-plugin/ /overleaf/libraries/eslint-plugin/
COPY libraries/fetch-utils/ /overleaf/libraries/fetch-utils/
COPY libraries/logger/ /overleaf/libraries/logger/
COPY libraries/metrics/ /overleaf/libraries/metrics/
COPY libraries/o-error/ /overleaf/libraries/o-error/
COPY libraries/overleaf-editor-core/ /overleaf/libraries/overleaf-editor-core/
COPY libraries/promise-utils/ /overleaf/libraries/promise-utils/
COPY libraries/settings/ /overleaf/libraries/settings/
COPY libraries/stream-utils/ /overleaf/libraries/stream-utils/
COPY libraries/validation-tools/ /overleaf/libraries/validation-tools/COPY services/clsi/ /overleaf/services/clsi/
COPY services/clsi_typst/ /overleaf/services/clsi_typst/
ENV NODE_OPTIONS="--require /overleaf/.pnp.cjs --import /overleaf/.pnp.register.mjs"

# clsi_typst only ships its own dirs (no texlive/tex stage).
RUN mkdir -p cache compiles output uploads \
    && chown node:node cache compiles output uploads
CMD ["node", "--expose-gc", "--trace-warnings", "app.js"]
