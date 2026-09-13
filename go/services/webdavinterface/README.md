# `webdavinterface` (Go) — the WebDAV sync bridge (token-protected)

> 1:1 Go drop-in for **`services/webdavinterface`** (Node). Internal bridge
> between Overleaf and a user-linked WebDAV server, authenticated with the
> shared service token (`go/pbhttp.RequireServiceToken`).

## What it does

Lets a project mirror files to/from a WebDAV endpoint (Nextcloud, ownCloud,
etc.). The web app drives these routes:

| Route | Purpose |
| --- | --- |
| `/check` | verify the WebDAV credentials/connection |
| `/list` | list a remote folder (PROPFIND) |
| `/file` | GET/PUT file content |
| `/mkdir` | MKCOL |
| `/move` | MOVE |
| `/health` | liveness |

It implements the raw WebDAV verbs directly (`verbs.go`), builds and parses
` Multistatus` responses (`multistatus.go`), and sanitises remote paths/href
encoding exactly as the Node client (`sanitize.go`), over
`http(s)` with the user's per-request URL + basic-auth, and the same
503-on-unreachable behaviour.

## 1:1 mapping

| Go file | Node original |
| --- | --- |
| `handler.go` | route handlers + token gate |
| `client.go` | the WebDAV client |
| `verbs.go` | PROPFIND/GET/PUT/MKCOL/MOVE |
| `multistatus.go` | WebDAV `Multistatus` XML parse/build |
| `sanitize.go` | path/href sanitisation |
| `webdav.go` | config + wiring |
| `*_test.go` | the locked contract (incl. the 503/401 shapes) |

## Interface

- **Port** — `WEBDAVINTERFACE_PORT` (default 4002)
- **Env** — `WEBDAVINTERFACE_PORT`, `SHARED_SERVICE_TOKEN`
- **Auth** — shared service token (constant-time); the WebDAV side uses the
  user's URL + credentials from the project config

## Build / test / run

```sh
go build ./go/services/webdavinterface
go test  ./go/services/webdavinterface
go run   ./cmd/webdavinterface
```

## Status

Converted, tests green. Cutover in the CE image awaits the owner call.
