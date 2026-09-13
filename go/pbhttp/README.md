# `go/pbhttp` — shared HTTP semantics for the Go service ports

Small shared package that captures the **framework behaviour the Node
services get for free from Express** and which each Go port must reproduce by
hand, so the four sync/bridge services don't each re-derive (and re-mis-derive)
it.

## Body limits — `express.json({ limit })` 1:1

`express.json` parses the JSON body eagerly and, when the body exceeds the
limit, surfaces body-parser's `entity.too.large`. What the client then sees
depends on the **service's own error handling** — some let it fall through to
Express' default **413 "request entity too large"**, others catch everything
and force **500 "Oops, something went wrong"**. So the limit and the
overflow response are a *per-service* decision, not a global one.

| Helper | Meaning |
| --- | --- |
| `LimitBodyWith(h, limit, code, text)` | bound the body to `limit` bytes; on overflow emit the **caller-chosen** `code` + `text`. This is the primitive each service uses to match its Node original exactly. |
| `LimitBody(h, limit)` | generic alias when the service's overflow is the Express default. |
| `LimitBodyJSON(h, limit, code, message)` | JSON-flavoured overflow body, for services whose handler renders JSON errors. |
| `methodHasBody(method)` | only POST/PUT/PATCH/DELETE bodies are bounded — matching Express, which does not limit GET/HEAD. |

## Service-token gate (the sync/bridge services)

The dropbox / github / webdav / datamanipulator services are internal and
authenticated with a shared token. `pbhttp` provides:

- `RequireServiceToken(expected, warn)` — middleware that compares the
  incoming token to the expected one and 401/403s on mismatch, with an
  optional once-per-process `warn` callback (the Node services log a token
  mismatch warning).
- `TimingSafeEqual(a, b)` — constant-time string compare so the token check
  does not leak via timing.

## Status/error plumbing

Replicates the tiny "error envelope" the Node services throw and render:

| Symbol | Role |
| --- | --- |
| `HTTPStatusError` / `HTTPStatus` / `HTTPStatusErr` | a status + message (and optional cause) carried as an `error`, like the Node `HttpError`. |
| `StatusOf(err)` / `MessageOf(err)` | recover the status/message from an error chain. |
| `WriteStatus(w, code, msg)` / `WritePlainText(w, code, text)` | send the exact status + text body the Node service does. |
| `WriteJSON(w, code, v)` / `WriteJSONErr(w, code, v)` | send a JSON body with the matching content-type. |
| `FmtStatusError(status, format, ...)` | build a `HTTPStatusError` from a format string. |

## Helpers

- `ResolveHostDNS(hostname)` / `FirstAddr(addrs)` — resolve a host and pick a
  first address (used by linked-url-proxy before it dials an upstream).
- `ContainsAny(list, s)` — small membership helper shared by the sync services.

## Build / test

```sh
go build ./go/pbhttp
go test  ./go/pbhttp
```
