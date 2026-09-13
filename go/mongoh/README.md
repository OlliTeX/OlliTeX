# `go/mongoh` — shared Mongo helper for the Go service ports

Thin shared package for the Go services that talk to the shared Overleaf
MongoDB (chat, notifications, docstore, and history-v1 when it lands). It
centralises exactly the part of the plumbing all of them share:

1. **Connection-string derivation** — the same resolution order the Node
   services use:

   ```
   MONGO_CONNECTION_STRING
   → mongodb://$MONGO_HOST/sharelatex
   → mongodb://127.0.0.1/sharelatex
   ```

   with the database taken from the URI path (default `sharelatex`), mirroring
   how each Node service builds `settings.mongo.url` and calls
   `mongoClient.db()` (no explicit name ⇒ the URI path).

2. **Connect / close lifecycle** — `Connect(ctx, Options)` with
   `Options.WithDefaults()`, the same ping-before-use check the Node services
   effectively perform.

What it deliberately does **not** do: no CRUD wrappers, no schemas, no ODM.
Each service issues its own 1:1 queries through
`go.mongodb.org/mongo-driver` so the filter shapes, projections and write
concerns stay visible next to the Node originals and easy to verify.

## Driver quirk the services inherit (read once)

- A BSON **subdocument decoded into a Go `any` field is `primitive.D`** (and
  arrays are `primitive.A`), *not* `map[string]any` / `[]any`.
- ObjectIDs decode as `primitive.ObjectID`; dates as `time.Time`.

Services therefore keep a small neutral-tree normaliser (docstore:
`normalizeTree` in `tree.go`, pinned by `TestNormalizeTreeDriverShapes`)
before map assertions, JSON writing or equality checks. If a new service
reads nested docs into `any`, reuse that pattern — the first symptom of
skipping it is "missing keys" in JSON responses.

## API surface

| Symbol | Role |
| --- | --- |
| `DefaultURI()` | `mongodb://127.0.0.1/sharelatex` (the Node fallback) |
| `DBFromURI(uri, fallback)` | extract the database from the URI path |
| `Options` / `WithDefaults()` | connection options with Node-equivalent defaults |
| `Connect(ctx, Options)` | dial + ping; returns `*mongo.Client` |

## Build / test

```sh
go build ./go/mongoh
go test  ./go/mongoh
```
