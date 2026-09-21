# `go/libraries` — the shared Go library port (Node `libraries/*`)

The 1:1 Go ports of the Node `libraries/` runtime packages, as tracked by
`go/libraries/HANDOFF.md` (L01–L16, all ≥85% coverage; the strict LIB-16 gate
re-proves the whole set on every run).

| package | Node original | one line |
| --- | --- | --- |
| `accesstoken` | `libraries/access-token` | token signing/verification (HS256 + encryptor seam) |
| `fetchutils` | `libraries/fetch-utils` | HTTP request wrapper with the Node agent/timeout/destroy semantics |
| `mongoutils` | `libraries/mongoutils` (utils.js + batched-update + objectid-helpers) | batched-update engine + ObjectId utilities |
| `mongowrapper` | `libraries/mongowrapper` | connection-string wrapper + health semantics |
| `notifprefs` | `libraries/notificationPreferences` | preferences schema contract (defaults + normalization) |
| `oerror` | `libraries/oerror` | OError type family (code/message semantics) |
| `ologger` | `libraries/logger` | logging manager + serializers + level checker + GCP seam |
| `ometrics` | `libraries/metrics` | event-loop/memory/open-sockets/mongodb metric surfaces |
| `otc` | `libraries/overleaf-editor-core` | the editor core: files, ops, transforms, origins (incl. DMP interop) |
| `persistors` | `libraries/persistors` | file persistors (GCS/S3/fake) + project-key + migration |
| `rangestracker` | `libraries/ranges-tracker` | OT ranges state machine (zod-validated mutations) |
| `rediswrapper` | `libraries/redis-wrapper` | redis client + locker + web-locker + health |
| `settings` | `libraries/settings` | the typed Settings singleton |
| `streamutils` | `libraries/stream-utils` | stream helpers |
| `validtools` | `libraries/valid-tools` | the zod-equivalent validation toolkit (schemas used across services) |

Two non-LIB packages live next door at the `go/` level: `minimatch`
(`libraries/minimatch`, glob matcher) and `s3x` (a minimal S3 client used by
`persistors` and the gitbridge swap store).

Build/test: `go build ./go/... && go test ./go/libraries/...` (plus the
strict gate in the HANDOFF doc). Node oracles: `libraries/<pkg>` unit suites
on Node v22 (the pinned message/shape/byte contracts in the Go tests).
The `persistors/loc` and `*/testdata` folders are fixtures, not code.
