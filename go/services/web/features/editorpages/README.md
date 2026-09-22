# `editorpages` — web feature package

The editor PAGE routes (P5.1a flip unit): `GET /editor/:id` and legacy `GET /Project/:id` — the baked React editor shell (see views/).

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).

## U2 (2026-09-22) — editor-entry route family

Node/Express routing is case-INsensitive, so the family is broader than the
P5.1 port assumed — all pinned live against this stack and locked by
`tests/e2e/specs/parity/web-go-u2-editor.test.e2e.ts` (3-leg dual-port):

| Wire (Node truth) | Go source |
|---|---|
| 200 editor for `/editor\|/project` ANY case × id in either hex case (main + detach) | `editorPagePattern` |
| 404 JSON `{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}` for non-empty invalid id | `editorBadIdPattern` + `editorBadId` (res.JSON = Node's json CT + weak ETag) |
| 404 HTML page for `/editor/`, 301 `/hub#/projects.all` for `/Project/` (any case) | `projectlist.dashSlashPat`; empty id never matches the editor patterns |
| 302 `/login` anonymous (auth first) | global core login gate (routes are not NoLogin) |

The gate also runs the per-request page data through its oracle and found
five P5.1-era divergences, fixed here: `ol-ExposedSettings`
`canManageTemplatesMenu` = `templates.MenuGrant` (full DB ladder), navbar
`canDisplayProjectUrlLookup` = admin, navbar `showSignUpLink` = false in
this stack (SAML SSO enabled ⇒ the registration-page feature defaults off —
`boolFromEnv(??) ?? !(saml||ldap||oidc)`), `ol-showTemplatesServerPro` =
site-admin (templates section exists), and the `loading-screen-init-*`
class from the user theme (`initialLoadingScreenTheme`).
