# `go/libraries/mongoutils` — mongo-utils (batched updates + test-DB helpers)

Go 1:1 port of **`libraries/mongo-utils`** (npm `@overleaf/mongo-utils`):
the **ObjectId time-span batched-update walker** (the migration workhorse for
updating large collections in `_id`-time windows) + the **test-database
guard / cleanup / drop** helpers. The repo already depends on the official
`go.mongodb.org/mongo-driver` (shared with `go/mongoh` / `mongowrapper`), so
this ports straight onto it.

## The API
| Symbol | Purpose |
| --- | --- |
| `func BatchedUpdate(ctx, collection, query bson.M, update any, projection bson.M, findOpts *options.FindOptions, batchOpts BatchedUpdateOptions) (int, error)` | walk the matching docs in `_id` time-spans (a `BatchMaxTimeSpanMs` window) and apply `update`, returning the count processed |
| `func BatchedUpdateWithResultHandling(same args)` | run the batched update and **exit** (Node `console.error` + `process.exit 0/1`) — for migration scripts |
| `type BatchedUpdateOptions` | `BatchMaxTimeSpanMs` (decimal string, default `ONE_MONTH_IN_MS` = 31 days), the `BatchRangeStart`/`BatchRangeEnd` bounds, etc. |
| `const ONE_MONTH_IN_MS = 1000*60*60*24*31` | the window length (Node uses **31 days**, not a calendar month) |
| `var IEdgeFuture` | a "just past now" ObjectId boundary (Node `ID_EDGE_FUTURE`) |
| `func ObjectIdFromInput(input string) (primitive.ObjectID, error)` / `func RenderObjectId(objectID primitive.ObjectID) string` | the ObjectID parse/render pair (Node `objectIdFromInput` / render) |
| `func EnsureTestDatabase(dbName string) error` | the test-DB guard — byte-for-byte Node `ensureTestDatabase` (refuses to run against a non-test database) |
| `func CleanupTestDatabase(ctx, client, dbName) error` | drop the test collections (Node `cleanupTestDatabase`) |
| `func DropTestDatabase(ctx, client, dbName) error` | drop the whole test database (Node `dropTestDatabase`) |
| `type Doc = map[string]any` | a document alias |

## How the batched update works
Because an ObjectID embeds a **timestamp** (first 4 bytes), the walker slices
the update into `_id` time-windows of `BatchMaxTimeSpanMs`, pulling the first
boundary one second into the past so the first entry passes `first._id >
ID_EDGE_PAST`. Node keeps this state in module-scope `let` variables (safe —
single-threaded); Go adds the **single-flight mutex** for the same guarantee.
`BatchedUpdateWithResultHandling` reproduces the Node script bootstrap
(run, log the count, `process.exit`).

## Conventions / gotchas
- **`EnsureTestDatabase` is a hard guard** — it checks the database name is the
  known test database and *refuses* otherwise (prevents a migration wiping a
  real DB). Byte-pinned to Node.
- **`ONE_MONTH_IN_MS` is 31 days** — do not "fix" it to a calendar month; the
  boundary math and the golden tests depend on the exact value.
- **`IEdgeFuture`** is computed at load time (`now + 1s`) — a *future*
  timestamp sentinel, not a fixed value.
- **Live-mongo tests** run against a real `mongod` (via the driver); they're
  skipped cleanly when no test database is reachable.

## Testing & coverage
`go test ./go/libraries/mongoutils/ -count=1 -cover` — the ObjectId helpers and
options/boundary math are unit-tested; the batched-update walker is live-tested
against `mongod` when available. **Coverage: 88.0%** (above the 85% gate).

## Dependencies
`go.mongodb.org/mongo-driver` (shared repo dependency).
