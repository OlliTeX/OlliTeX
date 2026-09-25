# Quick-Start Guide (OlliTeX)

## Prerequisites

- bash
- docker (recent stable; compose v2 plugin)
- for the optional operator console: python3 with `curses`
  (stock on Debian/Ubuntu/RHEL)

## Install

OlliTeX ships in the `tools/toolkit` directory of the repository:

```sh
$ cd <repo>/tools/toolkit
```

For the rest of this guide, all commands run from that directory.

## Take a look around

```
bin            # the scripts: up, start, stop, logs, doctor, images, ollitextui, ...
config         # YOUR local config (created by bin/init)
data           # persistent volumes (mongo, redis, project files, LT n-grams)
doc            # this documentation
lib            # seeds + compose fragments + images.env (image versions)
```

## Initialise

```sh
$ bin/init          # creates config/overleaf.rc + config/variables.env
```

| File | What it holds |
|------|---------------|
| `config/overleaf.rc` | stack toggles (mongo/redis/LT/TLS, sandboxed compiles, ports) |
| `config/variables.env` | server env: SMTP, LDAP, S3, bootstrap secrets |
| `lib/images.env` | **all container image versions (single source of truth)** |

## Start

```sh
$ bin/up -d         # pull images + start detached
$ bin/logs -f       # watch the log (Ctrl-C detaches the log, not the stack)
```

## Create the admin account

Open `http://<host>/register`, fill in the admin credentials and register.
Log in at `http://<host>/login`. You land in the **hub**
(`http://<host>/hub#/projects`) — projects, My settings, and (for admins)
the Admin area in one place.

Create your first project from the hub (the New-project menu includes
blank projects, the translated Example projects for TeX **and** Typst,
and templates). Editor deep links keep working directly:
`http://<host>/editor/<project-id>`.

## Everyday ops

```sh
$ bin/logs -f web clsi        # follow the main services
$ bin/doctor                  # health + config audit
$ bin/status                  # what is running
$ bin/backup-config -m zip    # config snapshot
$ bin/ollitextui              # optional: one-screen console (TUI)
$ bin/images                  # configured vs. installed images
$ bin/images --pull           # pre-pull everything (before upgrades)
```

## Optional add-ons

- **TLS**: `bin/init --tls` → [tls-proxy.md](./tls-proxy.md)
- **LanguageTool**: [language-toolkit.md](./language-toolkit.md)
- **Sandboxed compiles**: set `SIBLING_CONTAINERS_ENABLED=true` in
  `config/overleaf.rc` → [sandboxed-compiles.md](./sandboxed-compiles.md)

## Upgrades

```sh
$ bin/backup-config -m zip
$ bin/images --pull        # or edit lib/images.env first
$ bin/up -d
```

See [upgrading.md](./upgrading.md) for details.
