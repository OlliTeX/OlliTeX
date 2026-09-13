# `go/` — Go 1:1 ports of the Overleaf microservices

This directory contains **drop-in Go rewrites of the Node microservices** under
`services/`. The goal is not "a Go API that does the same job" — it is a **1:1
behavioural replacement**: the same port, the same environment variables, the
same MongoDB collections and on-disk files, the same HTTP status codes, the
same response bodies (byte-for-byte where the Node service is deterministic),
the same validation error strings. Any one of them can be swapped in place of
its Node original in the CE image without touching the web app or the other
services, and any one of them can be swapped back.

> **Rule of thumb:** when in doubt, the Node service at `services/<name>` is
> the spec. The Go package here is the implementation, and its test files are
> the locked contract.

## Layout

```
go/
├── mongoh/           # shared: Mongo connection lifecycle for the DB-backed services
├── pbhttp/           # shared: express.json body-limit semantics + shared HTTP bits
├── s3x/              # shared: zero-dep S3 (path-style) client for SeaweedFS — the s3 persistor backend
└── services/
    ├── chat/               # port 3010 — AI chat rooms & messages
    ├── notifications/      # port 3042 — notification centre (first/complete conversion)
    ├── docstore/           # port 3016 — text-document store + archive (fs **or** s3/SeaweedFS backend)
    ├── filestore/          # port 3009 — project/template file blobs (fs **or** s3/SeaweedFS backend)
    ├── linked-url-proxy/   # port 3066 — "import file from URL" proxy
    ├── datamanipulator/    # port env — file ops / sync engine helpers
    ├── dropboxinterface/   # port env — Dropbox sync bridge (token-protected)
    ├── githubinterface/    # port env — GitHub clone/push bridge (token-protected)
    └── webdavinterface/    # port 4002 — WebDAV sync bridge (token-protected)

cmd/
└── <service>/main.go   # one binary per service (same names as above)
    ├── docstore/           # + archive backend selection (fs | s3/SeaweedFS)
    ├── filestore/          # + store backend selection (fs | s3/SeaweedFS)
    └── seaweed-migrate/    # fs ↔ SeaweedFS conversion tool (to-seaweed / from-seaweed / list / health)
```

Go module: `ollitex` (repo root `go.mod`, Go 1.27). The only external
dependency is the official mongo-driver; everything else is standard library
(including `s3x` — no AWS SDK in the Go tree; the Node side keeps aws-sdk
for its in-tree S3 support).

## Shared packages

| Package | Purpose |
| --- | --- |
| `go/mongoh` | Derives the Mongo connection string (`MONGO_CONNECTION_STRING` / `MONGO_HOST` / `127.0.0.1`, database from the URI path, default `sharelatex`) and owns the connect/close lifecycle — exactly how the Node services build `settings.mongo.url`. It deliberately does **not** wrap CRUD: each service issues its own 1:1 queries so the semantics stay visible and checkable against the Node original. |
| `go/pbhttp` | `LimitBodyWith` reproduces `express.json({ limit })`: bound the body to N bytes, and on overflow return the **caller-chosen** status + text, because the Node services disagree (some 413 "request entity too large", some 500 "Oops, something went wrong"). Per-service parity instead of one-size-fits-all. |
| `go/s3x` | Minimal dependency-free S3 (path-style) client for SeaweedFS's S3 gateway — the transport behind the **s3 persistor backends** of filestore (`s3store.go`) and docstore (`s3archiver.go`). Put/Head/Get/Delete/List(paged)/CreateBucket; anonymous by default; keeps keys **verbatim** (Node `S3Persistor` semantics); translates the internal hex-md5 convention to the base64 `Content-MD5` wire format SeaweedFS enforces. Conversion for moving data between the flat-fs and s3 layouts lives in `cmd/seaweed-migrate`. |

## Per-service invariants (1:1 doctrine)

1. **Port & bind** — identical to the Node `settings.defaults.cjs` (`internal.<svc>.port`, `LISTEN_ADDRESS`).
2. **Env surface** — same env var names; same defaults (the Go `Config` docs quote the Node default file).
3. **Data plane** — same Mongo collections / same files; concurrent Node + Go can run side by side. Driver quirk worth knowing: the Go driver decodes a BSON subdocument into `any` as `primitive.D` (not `map[string]any`) and ObjectIDs as `primitive.ObjectID` — services normalise to a neutral tree before JSON output (`docstore` has a unit test pinning this).
4. **Error surface** — Node `NotFoundError` → 404 status text; `DocModifiedError`/`DocVersionDecrementedError` → 409 "Conflict"; everything else → 500 "Oops, something went wrong" (text/html), validation → locked issue strings with 404 when the `params` schema fails, else 400; unknown paths → Express fallback 404 `<h1>Cannot METHOD /path</h1>` page with `Content-Security-Policy: default-src 'none'` + `X-Content-Type-Options: nosniff`.
5. **Body limits** — per `express.json` limits (e.g. docstore: 12 MB `updateDoc`, 100 kb `patchDoc`), overflow → the Node service's exact overflow response.
6. **Tests** — HTTP contract tests hit the real `http.Handler` (httptest), so status codes, headers and bodies are asserted, not assumed. A subset is additionally verified live against the real mongod.

## Build, test, run

```sh
# from the repo root (the overleaf/ checkout)
go build ./go/... ./cmd/...
go test ./go/...
go vet ./go/services/<name>/...

# run one (examples)
go run ./cmd/docstore      # reads the same env as the Node service
go run ./cmd/chat
```

Binaries: `go build -o /tmp/<svc> ./cmd/<svc>` — identical env surface to the
Node container process, ready for a runit swap once the owner approves the
cutover (see each service README's Status section).

## Status overview

| Service | Port | Node original | Go package | Status |
| --- | --- | --- | --- | --- |
| chat | 3010 | `services/chat` | `go/services/chat` | converted, tests green |
| notifications | 3042 | `services/notifications` | `go/services/notifications` | converted (all 9 routes 1:1), tests green |
| docstore | 3016 | `services/docstore` | `go/services/docstore` | converted + **live-verified vs real mongod**; archive backend fs **or s3/SeaweedFS**; cutover pending owner call |
| filestore | 3009 | `services/filestore` | `go/services/filestore` | converted, tests green; backend fs **or s3/SeaweedFS** (s3 live-verified) |
| linked-url-proxy | 3066 | `services/linked-url-proxy` | `go/services/linked-url-proxy` | converted, tests green |
| datamanipulator | env | `services/datamanipulator` | `go/services/datamanipulator` | converted, tests green |
| dropboxinterface | env | `services/dropboxinterface` | `go/services/dropboxinterface` | converted, tests green |
| githubinterface | env | `services/githubinterface` | `go/services/githubinterface` | converted, tests green |
| webdavinterface | 4002 | `services/webdavinterface` | `go/services/webdavinterface` | converted, tests green |

`cmd/` mirrors this one binary per service. History-v1 (`services/document-updater`)
is the remaining Node service on the conversion list (TODO-4b0c99a9).

## What is deliberately NOT here

- No web server, no auth, no websocket relay — the web app stays Node.
- No SaaS backends: object storage speaks the **FS** persistor dialect of
  `object-persistor` (1:1 for `BACKEND=fs`); s3/gcs are out of scope exactly
  like the Node CE image's default configuration.
- No framework: no Router library, no ORM. Handlers are plain
  `http.HandlerFunc`s so every status line and header is hand-verified against
  the Node original.
