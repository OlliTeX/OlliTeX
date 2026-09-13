# `linked-url-proxy` (Go) — "import file from URL", port 3066

> 1:1 Go drop-in for **`services/linked-url-proxy`** (Node, port 3066).
> Split from the Node single-file service into files mirroring that layout.

## What it does

Lets a user import a file into their project from an external URL. The web
app asks the proxy to fetch an arbitrary URL; the proxy:

1. resolves the target host (system resolver, `dns.lookup`-equivalent),
2. enforces the **blocked-network ACL** (private ranges by policy; invalid
   CIDR entries yield a request-time 500, 1:1 with Node),
3. honours `OVERLEAF_LINKED_URL_ALLOWED_RESOURCES` (regex exemption from the
   IP block — how the CE image allows known-good hosts),
4. fetches with a bounded redirect chain (≤ 5), a 30 s timeout and a 50 MiB
   body cap,
5. streams the body (and content-type) back to the web app.

Effectively server-side `fetch` with the CE security policy baked in — that
policy behaviour is the contract, and the Go port reproduces it byte-for-byte,
including the exact error bodies.

## 1:1 mapping

| Go file | Node original |
| --- | --- |
| `config.go` | `config/settings.defaults.cjs` (defaults + env parsing, same names) |
| `proxy.go` | the fetch/redirect/stream loop (`app/src/...`) |
| `resolver.go` | DNS resolution + blocked-IP matching (incl. `OVERLEAF_LINKED_URL_ALLOWED_RESOURCES`) |
| `*_test.go` | the locked contract (block-list cases, redirect cap, size cap, ACL exemption) |

Shared bits (DNS resolve, body-limit middleware) come from `go/pbhttp`.

## Interface

- **Port** 3066, bind `LINKED_URL_PROXY_HOST` || `LISTEN_ADDRESS` || 127.0.0.1
- **Env** (per `config.go`, same names as the Node config):
  `OVERLEAF_LINKED_URL_BLOCKED_NETWORKS` (CIDR list),
  `OVERLEAF_LINKED_URL_ALLOWED_RESOURCES` (exemption regex),
  `MAX_UPLOAD_SIZE` (upstream body cap), `LINKED_URL_PROXY_HOST`
- **Fixed 1:1 defaults** (as in `settings.defaults.cjs`): ≤ 5 redirects,
  30 s fetch timeout, 50 MiB max upload, the Node service's User-Agent string
- **Outbound** — one HTTP(S) fetch to the user-supplied URL (upstream),
  streamed back to the web app

## Build / test / run

```sh
go build ./go/services/linked-url-proxy
go test  ./go/services/linked-url-proxy
go run   ./cmd/linked-url-proxy
```

## Status

Converted, tests green. Cutover in the CE image awaits the owner call.
