# `notifications` (Go) — the notification centre, port 3042

> 1:1 Go drop-in for **`services/notifications`** (Node, port 3042). This was
> the first conversion and established the 1:1 doctrine the other services
> follow; see `../README.md`.

## What it does

Per-user notification rows (chat replies, share events, LLM events, …) plus
the key-based bulk primitives the web app and other services use
(`notifications` + `emailNotifications` collections). The patterns it set
(exact status bodies, `sendStatus` texts, Express-fallback 404s, token-free
internal API) are the ones the whole `go/services` tree now follows.

All 9 Node routes are covered 1:1 (verified against
`services/notifications/app.ts`): the user/key CRUD family + bulk delete,
`/status` and `/health_check`. It is a complete drop-in — "pilot" here means
*first in conversion order*, not partial.

## Routes (exact, from `server.go`)

`route` is `(method, path)`; dispatch mirrors Express (first match wins,
otherwise the method-specific 404 body):

| Method | Path | Handler |
| --- | --- | --- |
| POST | `/user/:user_id` | add notification |
| GET | `/user/:user_id` | list user notifications |
| DELETE | `/user/:user_id/notification/:notification_id` | remove one |
| DELETE | `/user/:user_id` | remove by key (query `key`) |
| DELETE | `/key/:key` | remove all by key |
| GET | `/key/:key/count` | count by key |
| DELETE | `/key/:key/bulk` | bulk-delete unread by key |
| GET | `/status` | liveness |
| GET | `/health_check` | Mongo-reachable check |

## 1:1 mapping

| Go file | Node original |
| --- | --- |
| `notifications.go` / `server.go` | `app.ts` + `app/js/Notifications.js` |
| `handlers.go` | `app/js/NotificationsController.ts` |
| `mongo.go` | `app/js/...` Mongo access (via `go/mongoh`) |
| `healthcheck.go` | the Node health-check handler |
| `notifications_test.go` | the locked HTTP contract |

The driver-shape → JSON normalisation (`sanitiseForJSON`: ObjectID→hex,
Date→ISO-8601, `primitive.M/D`→JSON) is what keeps a document read by the Go
service byte-equivalent (as JSON) to one read by the Node service.

## Interface

- **Port** 3042 (fixed by the Node service — no env override;
  `LISTEN_ADDRESS` || 127.0.0.1)
- **Mongo** `sharelatex` via `go/mongoh`
- **Env** — `MONGO_CONNECTION_STRING` (see `cmd/notifications/main.go`)

## Build / test / run

```sh
go build ./go/services/notifications
go test  ./go/services/notifications
go run   ./cmd/notifications
```

## Status

Converted (all 9 routes 1:1), tests green. Cutover in the CE image awaits the owner call.
