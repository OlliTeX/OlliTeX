# `dropboxinterface` (Go) — the Dropbox sync bridge (token-protected)

> 1:1 Go drop-in for **`services/dropboxinterface`** (Node). Internal bridge
> between the Overleaf web app and the Dropbox API v2, authenticated with the
> shared service token (`go/pbhttp.RequireServiceToken`).

## What it does

A user-linked Dropbox project can mirror its files to/from Dropbox. The web
app drives these routes (all behind the shared token):

| Route | Purpose |
| --- | --- |
| `/check` | verify the linked Dropbox credentials |
| `/list` | list a Dropbox folder |
| `/file` | download a file's content |
| `/mkdir` | create a folder |
| `/move` | move/rename a path |
| `/health` | liveness |

It speaks Dropbox API v2 (metadata + content endpoints,
`DROPBOX_API_BASE` / `DROPBOX_CONTENT_BASE`) exactly as the Node client does,
including token refresh handling (`tokens.go`) and the path/content mapping
(`mapping.go`).

## 1:1 mapping

| Go file | Node original |
| --- | --- |
| `handler.go` | the route handlers + token gate |
| `client.go` | the Dropbox API v2 HTTP client |
| `tokens.go` | access-token refresh flow |
| `mapping.go` | node-path ↔ Dropbox-path mapping |
| `dropbox.go` | config + handler wiring |
| `*_test.go` | the locked contract (including the 503/401 shapes) |

## Interface

- **Port** — `DROPBOXINTERFACE_PORT` (no default; set by the CE compose)
- **Env** — `DROPBOXINTERFACE_PORT`, `DROPBOX_API_BASE`,
  `DROPBOX_CONTENT_BASE`, `SHARED_SERVICE_TOKEN`
- **Auth** — shared service token header, constant-time compared; the Dropbox
  side uses the user's stored access token

## Build / test / run

```sh
go build ./go/services/dropboxinterface
go test  ./go/services/dropboxinterface
go run   ./cmd/dropboxinterface
```

## Status

Converted, tests green. Cutover in the CE image awaits the owner call.
