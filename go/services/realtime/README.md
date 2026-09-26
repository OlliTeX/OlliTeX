# real-time — the Overleaf event bus in Go (D28a)

Go replacement for the Node `services/real-time` socket.io-0.9 relay service
(port **3026**). The browser socket.io **0.9.17-overleaf-6** client
(`frontend/js/ide/connection/SocketIoShim.js`) is unchanged: the wire
protocol (live-captured strings pinned in `protocol_test.go`), the event
contract, the redis keys, and the ops/health endpoints are 1:1.

The **OT text-sync substrate is retired** (D25: text lives in the Yjs/Ygo
collab engine on port 3450) — `joinDoc`/`leaveDoc`/`applyOtUpdate` are not
implemented; only the collaboration-orchestration event bus remains.

## Files

| file | role |
|---|---|
| `protocol.go` | 0.9 wire codec: `type[:id[+]][:endpoint]?:data`, event payloads `{"name","args"}`, ack data `{id}[+]args`; pinned strings `1::`, `2::`, `5:::`, `6:::N+[...]` |
| `session.go` | session cookie pipeline (pct-decode → strip `s:` → HMAC-SHA256 unsign, `OVERLEAF_SESSION_SECRET` chain), user / anon-token resolution from the session doc |
| `webapi.go` | Go-web private join API (`POST /project/:pid/join`, basic auth) + document-updater flush (`DELETE ...?background=true`) |
| `users.go` | presence in redis (pinned keys `clients_in_project:*`, `connected_user:*`, `projectNotEmptySince:*`; TTLs 4 day / 15 min / 31 day; `getConnectedUsers` filters `client_age < 10 s`) + in-memory fake |
| `bus.go` | clients, join flow (drain → session → projectId → user → join → **doc grants** → presence → `joinProjectResponse`), `clientTracking.*`, `clientPong`, `debug`, drain/reconnect, ops accessors |
| `grantdocs.go` | join-time grant of every doc id in the join `project` model (replaces the retired `joinDoc` grants — F2 clients no longer emit `joinDoc`) |
| `server.go` | HTTP: `/socket.io/1` handshake (`SID:60:60:websocket,xhr-polling`), websocket transport (gorilla), xhr-polling transport, heartbeat 25 s / stale 60 s, ops + health + drain endpoints, `/socket.io/socket.io.js` (embedded 0.9.17-overleaf-6 bundle) |
| `redis.go` | go-redis adapter for `RedisLike` + `redisSession` (session docs `sess:<sid>`) |
| `static/` | the exact browser socket.io client bundle (MIT, `static/LICENSE`) |

`cmd/realtime/main.go` — process entrypoint (env per Node parity:
`REAL_TIME_REDIS_HOST/PORT/PASSWORD`, `WEB_API_HOST/PORT/USER/PASSWORD`,
`DOCUMENT_UPDATER_HOST`, `OVERLEAF_SESSION_SECRET`, `COOKIE_NAME`,
`LISTEN_ADDRESS`, `LOGLEVEL`).

## Pinned contract (live-captured, 2026-09-26, Node bus in prod)

- handshake → `SID:60:60:websocket,xhr-polling` (text/plain)
- open (ws or polling) → `1::`
- server events: `5:::{"name":"joinProjectResponse","args":[{publicId,project,permissionsLevel,protocolVersion:2}]}`
- `clientTracking.getConnectedUsers` (acks `5:1+::...`) → `6:::1+[null,[{client_id,last_updated_at,user_id,first_name,last_name,email,connected:true,cursorData?}...]]`
- `clientTracking.updatePosition {row?,column?,doc_id}` → room-broadcast `clientTracking.clientUpdated {id,user_id,email,name,row,column,doc_id}`
- disconnect → room `clientTracking.clientDisconnected <publicId>`
- drain → `reconnectGracefully`; new joins rejected `{"message":"retry"}`
- rejected joins: `invalid session` / `missing/bad ?projectId=...` / `not authorized` / `project not found` / `rate-limit hit when joining project`

## Gate

`make go-test-realtime` — build + vet + gofmt + `go test -race`:
wire pins, join flows (success/403/404/bad-secret/bad-cookie/missing pid),
presence + refresh + disconnect, cursor broadcast incl. ungranted-doc drop,
drain, ops APIs, and a full websocket-client end-to-end join→ack→broadcast
loop.

## Status

- [x] package + tests green
- [ ] runit switch `server-ce/runit/real-time-overleaf/run` → `bin/realtime`
- [ ] image rebuild + browser e2e (IDE join, 2-tab presence, drain)
- [x] Node `services/real-time` retirement (after e2e)
