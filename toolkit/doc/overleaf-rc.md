# Configuration: `overleaf.rc`

This document describes the variables that are supported in the `config/overleaf.rc` file.
This file consists of variable definitions in the form `NAME=value`. Lines beginning with `#` are treated as comments.

Note: we recommend that you re-create the docker containers after changing anything in `overleaf.rc` or `variables.env`, by running `bin/docker-compose down`, followed by `bin/up`

## Variables


### `PROJECT_NAME`

Sets the value of the `--project-name` flag supplied to `docker-compose`.
This is useful when running multiple instances of Overleaf on one host, as each instance can have a different project name.

- Default: overleaf


### `OVERLEAF_DATA_PATH`

Sets the path to the directory that will be mounted into the main `sharelatex` container, and used to store project and compile data. This can be either a full path (beginning with a `/`), or relative to the base directory of the toolkit.

- Default: data/sharelatex

### `OVERLEAF_LOG_PATH`

Sets the path to the directory that will be mounted into the main `sharelatex` container, and used for making application logs available on the Docker host. This can be either a full path (beginning with a `/`), or relative to the base directory of the toolkit.

Remove the config entry to disable the bind-mount. When not set, logs will be discarded when recreating the container.

- Default: not set

### `OVERLEAF_LISTEN_IP`

Sets the host IP address(es) that the container will bind to. For example, if this is set to `0.0.0.0`, then the web interface will be available on any host IP address.

Since https://github.com/overleaf/toolkit/pull/77 the listen mode of the application container was changed to `localhost` only, so the value of `OVERLEAF_LISTEN_IP` must be set to the public IP address for direct container access.

Setting `OVERLEAF_LISTEN_IP` to either `0.0.0.0` or the external IP of your host will typically cause errors when used in conjunction with the [TLS Proxy](tls-proxy.md).

- Default: `127.0.0.1`

### `OVERLEAF_PORT`

Sets the host port that the container will bind to. For example, if this is set to `8099` and `OVERLEAF_LISTEN_IP` is set to `127.0.0.1`, then the web interface will be available on `http://localhost:8099`.

- Default: 80

### Image versions (single file)

All container image names/tags/versions live in **`lib/images.env`**
(single source of truth): `OLLITEX_IMAGE` (server, default
`sharelatex/sharelatex:ext-6.3.0-port`), `MONGO_IMAGE`, `REDIS_IMAGE`,
`NGINX_IMAGE`, `TYPST_IMAGE` (digest-pinned), `TEXLIVE_IMAGE` (optional),
`LANGUAGE_TOOL_IMAGE` (optional).

A user may still override the server image per install with
`OVERLEAF_IMAGE_NAME` (name only; the tag comes from
`config/version`).


### `SIBLING_CONTAINERS_ENABLED`

When set to `true`, tells the toolkit to use the "Sibling Containers" technique
for compiling projects in separate sandboxes, using a separate docker container for
each project. See [the legacy documentation on Sandboxed Compiles](https://github.com/sharelatex/sharelatex/wiki/Server-Pro:-sandboxed-compiles) for more information.

Pre-pulls the TeX Live + pinned Typst images.

- Default: false


### `DOCKER_SOCKET_PATH`

Sets the path to the docker socket on the host machine (the machine running the toolkit code). When `SIBLING_CONTAINERS_ENABLED` is `true`, the socket will be mounted into the container, to allow the compiler service to spawn new docker containers on the host.

Requires `SIBLING_CONTAINERS_ENABLED=true`

- Default: /var/run/docker.sock

### `LANGUAGE_TOOL_ENABLED`

Set to `true` to run the LanguageTool grammar-checking container.
Language models: `bin/languagetool-ngrams` (official per-language
downloads; admin toggle + endpoint: /hub → Admin → Site → Grammar).
See [language-toolkit.md](./language-toolkit.md).

- Default: false

### `LANGUAGE_TOOL_PORT`

Host port for the LanguageTool service (service + admin URL default
`http://languagetool:<port>`; override with `LANGUAGETOOL_URL`).

- Default: 8010

### `MONGO_ENABLED`