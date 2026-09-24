# `go/services/web/features` — one package per Node route surface

Each package is a 1:1 Go port of a slice of the Node web router (a module, a
controller family, or a leaf surface). All were flipped oracle-pinned and are
locked by parity gates under `tests/e2e/specs/parity/`. The phase letter
(prefixed `P#`) is the WEB_GO_PLAN unit that owns it.

| package | surface | unit |
| --- | --- | --- |
| `adminusers` | `/admin/user/*` admin user management (`/admin/user/create`, list/set) | P3.x |
| `authpages` | login/logout/register/password auth surface | P1/P2 |
| `compile` | editor COMPILE control plane (compile state, logs, cancel) | P5.2a |
| `devcsrf` | `GET /dev/csrf` dev token helper | — |
| `dropbox` | `/user/dropbox/*` + `/project/new/dropbox` Dropbox sync/import | P6.10 |
| `editorpages` | editor PAGE routes (open/editor shell) | P5.1a |
| `gitbridge` | the web-side Git Bridge bridge (OAuth, projects, blobs) | P6.x |
| `healthcheck` | `/health_check` + `/status` liveness | — |
| `hub` | the `ollitex-hub` module server surface | P6.1 |
| `instancestats` | instance-stats API | P3.2 |
| `languagetool` | LanguageTool proxy surface | P6.15 |
| `launchpad` | first-admin bootstrap (`/launchpad` + register/test-email) | P6.20 |
| `library` | bib-editor Library surface | P6.5 |
| `llmsettings` | LLM settings/providers/usage (user + admin) | P6.4a |
| `mendeley` | Mendeley sync surface | P6.8 |
| `notifications` | `/notifications/preferences` + notification-preferences + test-email | P6.14 |
| `orcidpicker` | ORCID picker/OAuth surface | P6.7 |
| `pageshells` | page-shell (PSH) redirect surface | P6.18 |
| `passwordreset` | CE password-reset flow | P2 |
| `projectlist` | `GET /user/projects` (project list) | P4.1 |
| `registrationpage` | CE registration-page flow | P3.4 |
| `serveradmin` | CE ServerAdmin leaf (system-message CRUD, editor gate) | P3.1 |
| `sitesettings` | admin "Manage Site" SiteSettings leaf | P3.6 |
| `staticpages` | NonCE StaticPages surface | P1 |
| `status` | `/status` endpoints (plainTextResponse family) | — |
| `systemmessages` | `GET /system/messages` | P1 |
| `templates` | template-gallery surface | P6.13 |
| `texfmt` | tex-autoformatter proxy | P6.17 |
| `tokenaccess` | token access + link-sharing consent | P2 |
| `trackchanges` | tracked-changes thread/resolve surface | P6.12 |
| `userpages` | user settings + sessions family | P3.3 |
| `webdav` | `/user/webdav/*` + `/project/:id/webdav/*` + webdav import | P6.9 |
| `zotero` | Zotero sync surface | P6.6 |

Each feature `Feature(app core.App)` returns `core.Feature{Name, Routes}`; the
binary registers them all in `cmd/web/main.go` (repo root). Handlers return
`func(*core.Cxt, *core.Res)`. Read a package's doc comment for its Node
source-of-truth mapping and the exact response pins.
