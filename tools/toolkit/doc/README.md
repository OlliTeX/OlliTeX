# OlliTeX Toolkit — documentation

| Topic | File |
|-------|------|
| Getting started | [Overview](./overview.md) · [Quick Start Guide](./quick-start-guide.md) · [Dependencies](./dependencies.md) |
| Toolkit mechanics | [The Doctor](./the-doctor.md) · [Docker-Compose services](./docker-compose.md) · [Toolkit→compose migration](./docker-compose-to-toolkit-migration.md) |
| Configuration | [Configuration overview](./configuration.md) · [overleaf.rc](./overleaf-rc.md) · [TLS proxy](./tls-proxy.md) |
| Services | **LanguageTool (grammar)**: [language-toolkit.md](./language-toolkit.md) · [Sandboxed compiles](./sandboxed-compiles.md) · [LDAP](./ldap.md) · [SAML](./saml.md) |
| Data | [Persistent data](./persistent-data.md) |
| Upgrades | [Upgrading](./upgrading.md) · [CE TexLive upgrade](./ce-upgrading-texlive.md) · [From 0.x](./upgrading-from-0.x.md) |

Notes:

- **Image versions**: one file — `lib/images.env`.
- **Most admin configuration lives in the web UI** since the hub wave:
  brand/appearance/LLM/LanguageTool URL/notifications/templates are all
  managed at `/hub` (workspace) and `/hub → Admin` (site). The toolkit
  files carry what must exist before the UI: bootstrap secrets, SMTP,
  LDAP, storage, TLS.
- The legacy CE docs remain useful: <https://github.com/overleaf/overleaf/wiki>.
