# OlliTeX Toolkit

The deployment toolkit for **OlliTeX** — the CE-licensed LaTeX
collaborative editor built on the Overleaf 6.3.0 port with:

- the **/hub** unified workspace + admin UI (site settings, users,
  projects, templates, appearance, system messages, …)
- **Typst** compilation (pinned `pandoc/typst` image, offline-safe)
- **LanguageTool** grammar checking (optional service, this toolkit)
- LLM features, Zotero/WebDAV/GitHub modules, sandboxed compiles

## Quick start

```sh
cd toolkit
bin/init            # create config/ (edit values to taste)
bin/up -d           # pull + start the stack (OlliTeX + mongo + redis)
```

Your instance is now at `http://<host>:80` (see `OVERLEAF_LISTEN_IP` /
`OVERLEAF_PORT` in `config/overleaf.rc`). Day-to-day:

```sh
bin/logs -f         # follow logs
bin/status          # what is running
bin/ollitextui      # one-screen operator console (TUI, optional)
```

## Where things are configured

| What                                        | Where |
|---------------------------------------------|-------|
| **Image versions (all of them)**            | **`lib/images.env`** — one file, one place (Task "single version file") |
| Stack toggles (mongo/redis/LT/TLS)          | `config/overleaf.rc` |
| Server env vars (SMTP, LDAP, S3, …)         | `config/variables.env` |
| Branding, appearance, LLM, LanguageTool URL, notifications, templates | **in the web UI**: `/hub` (workspace) and `/hub → Admin` (site) |
| Admin users / projects / messages           | `/hub → Admin` |

Most of what used to be "admin panel" pages is now the hub — the
toolkit only handles what must exist before the UI does (bootstrap
secrets, SMTP, LDAP, storage, TLS).

## Optional services

- **LanguageTool (grammar)**: `doc/language-toolkit.md` — enable
  `LANGUAGE_TOOL_ENABLED=true`, run `bin/languagetool-ngrams --languages en,de`,
  `bin/up`. Admin toggle: /hub → Admin → Site → Grammar.
- **TLS (nginx proxy)**: `bin/init --tls` + `doc/tls-proxy.md`.
- **Sandboxed compiles**: `SIBLING_CONTAINERS_ENABLED=true` in
  `config/overleaf.rc` (mounts the Docker socket; the Typst + TeX Live
  compile images are pre-pulled by `bin/up`).
- **Operator TUI**: `bin/ollitextui` (thin wrapper over the same bin/
  scripts — nothing is lost, everything stays scriptable).

## Upgrading

```sh
git pull                       # toolkit code (if you track a git copy)
bin/images --pull              # pull the images named in lib/images.env
bin/backup-config -m zip       # back up config
bin/up -d                      # start on the new image
```

Data (mongo, project files) survives upgrades; see
`doc/upgrading.md` for notes.

## Docs

See `doc/README.md` for the full index (configuration, persistence,
sandboxed compiles, LDAP, TLS, …).

## License

OlliTeX builds on the Overleaf Community Edition (AGPL-3.0, see
`LICENSE`).
