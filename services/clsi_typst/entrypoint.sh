#!/bin/sh

# add the node user to the docker group on the host
DOCKER_GROUP=$(stat -c '%g' /var/run/docker.sock)
groupadd --non-unique --gid "${DOCKER_GROUP}" dockeronhost
usermod -aG dockeronhost node

# compatibility: initial volume setup
mkdir -p /overleaf/services/clsi_typst/cache && chown node:node /overleaf/services/clsi_typst/cache
mkdir -p /overleaf/services/clsi_typst/compiles && chown node:node /overleaf/services/clsi_typst/compiles
mkdir -p /overleaf/services/clsi_typst/output && chown node:node /overleaf/services/clsi_typst/output
mkdir -p /overleaf/services/clsi_typst/uploads && chown node:node /overleaf/services/clsi_typst/uploads

exec runuser -u node -- "$@"
