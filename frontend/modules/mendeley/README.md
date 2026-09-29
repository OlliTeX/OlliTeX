# Mendeley reference connector

Imports BibTeX references from a Mendeley account (My Library or a group)
into project `.bib` linked files — the Mendeley half of the former
Third-Party-References module (tpr-webmodule) from the pro fork, ported to
6.3.0 in 2026-09-07. The Zotero half ships in `modules/zotero`; the shared
file-view chrome ("imported from …", refresh button/error) is provider-
aware and lives there (`reference-providers.ts`).

## How it works

- **OAuth 2.0** (authorization code + refresh tokens) against
  `api.mendeley.com`; tokens encrypted at rest per user
  (`refProviders.mendeley.encrypted`, same `AccessTokenEncryptor` as the
  zotero/webdav/dropbox connectors) and transparently refreshed.
- **Instance credentials** = a Mendeley OAuth app:
  `MENDELEY_CLIENT_ID` + `MENDELEY_CLIENT_SECRET` env (seeded into
  `Settings.mendeley` at boot, see `index.mjs`).
  Without them every endpoint still answers **graceful 2xx/4xx**
  (`/user/mendeley/status` → `{ configured:false }`) — **never 5xx**.
- **Linked files** use the core linked-file machinery
  (`LinkedFilesHandler`) exactly like zotero:
  `createLinkedFile` / `refreshLinkedFile` with
  `linkedFileData = { provider: 'mendeley', mendeleyGroupId?, importedAt, importedByUserId, importedByName }`.

## Endpoints

| Route | Purpose |
| --- | --- |
| `GET /user/mendeley/status` | `{ configured, connected }` (drives the widgets) |
| `GET /mendeley/groups` | user's Mendeley groups for the create-file modal |
| `GET /user/mendeley/oauth` | start OAuth (CSRF `state` in session) |
| `GET /user/mendeley/oauth/callback` | finish OAuth, store tokens encrypted |
| `POST /mendeley/unlink` | remove the user's stored tokens |
| linked-file create/refresh | core `/project/:id/linked_file` endpoints (provider `mendeley`) |

## Frontend surfaces (registered in `config/settings.defaults.js`)

- **New file → Mendeley** (`createFileModes`): `mendeley-create-file` +
  `file-tree-import-from-mendeley` (library select incl. My Library, file name)
- **References settings widget** (`referenceLinkingWidgets`):
  `mendeley-widget` (link via OAuth popup / unlink with confirm modal,
  "not configured" state)
- **Integrations panel card** (integrations panel card array):
  `mendeley-integration-card`

## i18n

All `mendeley*` keys exist in `services/web/locales/en.json` (devs edit
`en.json`; other locales follow the usual translation tooling).

## Env

| Var | Meaning |
| --- | --- |
| `MENDELEY_CLIENT_ID` | Mendeley OAuth app client id |
| `MENDELEY_CLIENT_SECRET` | Mendeley OAuth app client secret |
| `MENDELEY_PROXY_URL` | optional HTTP(S) proxy for server-side Mendeley calls |
