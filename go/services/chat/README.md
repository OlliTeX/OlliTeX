# `chat` (Go) — the AI-chat service, port 3010

> 1:1 Go drop-in for **`services/chat`** (Node, port 3010): same HTTP API,
> same shared MongoDB collections, same cross-service document reads/writes.

## What it does

Chat rooms and message threads per project/document. It is the backend the
`/editor` "Ask AI" panel (and the `llm` module's chat) talks to: create rooms,
list/fetch messages, send, delete, pin. It also performs the Node
`NotificationsManager`'s documented writes against **other** services'
collections, keeping parity with the Node service — `projects`, `users`,
`notificationsPreferences`, `notifications`, `emailNotifications` — so a
room/message event still lands in the notification centre exactly as before.

## 1:1 mapping

| Go file | Node original |
| --- | --- |
| `chat.go` | `services/chat/app.js` (route table + error handlers) |
| `handlers.go` | `Features/Messages/MessageHttpController.js` (+ `MessageHttpSchemas.js` validation strings) |
| `server.go` | route registration + `express.json` limits |
| `notify.go` | `Features/Notifications/NotificationsManager.js` (cross-collection writes) |
| `mongo.go` | `app/js/mongodb.js` + `MessageManager.js` queries |
| `chat_test.go`, `memstore_test.go` | the locked HTTP contract |

Route surface (21 routes): `GET /status`, plus the
`/project/:project_id/...` room & message routes (rooms CRUD, message list,
send, delete, …). The exact set is in `chat.go` and pinned by the tests.

## Interface

- **Port** 3010 (`LISTEN_ADDRESS` || 127.0.0.1)
- **Mongo** `sharelatex` via `go/mongoh` (same env surface as the Node service)
- **Env** — the same names the Node `services/chat/config` reads (see
  `cmd/chat/main.go`)
- **Auth** — none of its own; the web service fronts it (same as Node)

## Build / test / run

```sh
go build ./go/services/chat
go test  ./go/services/chat
go run   ./cmd/chat
```

## Status

Converted, tests green. Cutover in the CE image (runit swap of the Node
`chat` service) awaits the owner call, same as the other services.
