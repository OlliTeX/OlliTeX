# `go/libraries/fetchutils` — HTTP fetch helpers (node-compatible)

Go 1:1 drop-in port of **`libraries/fetch-utils`** (npm `@overleaf/fetch-utils`,
a **node-fetch 2 wrapper**): the `fetch*` variants the web + sync-bridge
services use, plus the connect-timeout/retry agent, the `request failed` /
`connect timeout` error types, and the over-timeout logger.

## The API
| Symbol | Purpose |
| --- | --- |
| `func FetchJson(ctx, url string, opts ...*Options) (any, error)` | GET/POST → parse JSON body |
| `func FetchStream(ctx, url string, opts ...*Options) (io.ReadCloser, error)` | stream the response body |
| `func FetchNothing(ctx, url string, opts ...*Options) (*http.Response, error)` | a request that only cares about success (2xx), discarding the body |
| `func FetchRedirect(ctx, url string, opts ...*Options) (string, error)` | follow the redirect and return the final Location |
| `func FetchString(ctx, url string, opts ...*Options) (string, error)` | return the body as a string |
| each `…WithResponse` variant | also return the `*http.Response` (status/headers) |
| `type Options struct{ Method string; Headers any; JSON any; Body io.Reader; BasicAuth *BasicAuth; Client *http.Client; Redirect string }` | the request options; `Client==nil` → `DefaultClient` |
| `func NewCustomHttpAgent(opts AgentOptions) (*http.Client, error)` / `NewCustomHttpsAgent(…)` | the `*http.Client` with the connect-timeout + **3-attempt retry** dialer (Node `CustomHttpAgent`/`CustomHttpsAgent`) |
| `type AgentOptions` | `ConnectTimeout`, `ConnectRetryInterval` (positive required) |
| `type BasicAuth struct{ User, Password string }` | HTTP basic auth |
| `func SetLogger(warn WarnFunc)` | the over-timeout watchdog sink |
| `type RequestFailedError` | `request failed` (`OError`) with info `{url, method, status, body?}`; non-2xx responses |
| `type ConnectTimeoutError` | `connect timeout` (`OError`) from the dialer |

## Node → Go conventions (documented deltas)
- **`AbortSignal` → `context.Context`.** Cancellation is driven by `ctx.Err()`.
- **Stream-destroy hooks are native in Go.** Closing the response body (or the
  context) tears the request down; destroying an unconsumed request reader
  closes it (pinned in tests).
- **Agent/timeout plumbing → `*http.Client`** with a retried dialer
  (`Connect` / `ConnectTLS` context, 3 attempts — node semantics). The TLS
  dialer is installed as `Transport.DialTLSContext` so the transport's own
  handshake isn't double-performed. The **last** dial error is surfaced; a
  timeout surfaces `ConnectTimeoutError`.
- **`setLogger({warn})` → `SetLogger(WarnFunc)`.** The 120s watchdog warns
  `{url, method, overTimeoutMs, stack}` with `"Fetch request did not complete
  within 120 seconds"` (the stack is Go `runtime` frames, Node's V8 stack).
- **`RequestFailedError` / `ConnectTimeoutError` embed `oerror.OError`** so they
  carry structured info and render with the Node names.

## Testing & coverage
`go test ./go/libraries/fetchutils/ -count=1 -cover` — oracle-pinned to the Node
`fetch-utils` suite (every `fetch*` variant, the 3-retry dialer, connect-timeout
error, the request-destroy path, the 120s logger warn). **Coverage: 94.9%**
(above the 85% gate).

> ⚠️ **Known flake:** `does_not_open_a_stray_connection_when_the_socket_errors_
> after_connect` is a pre-existing **timing-sensitive** test — it passes on
> re-run (3/3) and is unrelated to this package's logic. If the gate shows it
> red, re-run before investigating.

## Dependencies
Standard library (`net/http`, `context`) + `ollitex/go/libraries/oerror`
(for the typed error shapes).
