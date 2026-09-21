# `go/libraries/mongowrapper` — mongoose-wrapper façade

Go counterpart of **`libraries/mongoose-wrapper`** (npm
`@overleaf/mongoose-wrapper`). The Node package is a **six-line pure
re-export** of mongoose:

```js
export default mongoose
export const Schema = mongoose.Schema
export const model = mongoose.model
export const connect = mongoose.connect
export const connection = mongoose.connection
```

so a driver swap is one place. This façade reproduces that shape over the
official **`go.mongodb.org/mongo-driver`** (already a repo dependency, shared
with `go/mongoh`). **No logic is added** — the Node wrapper adds none — so
nothing here is observable beyond the driver it names.

## The API (Node export name → Go)
| Node export | Go | Purpose |
| --- | --- | --- |
| `connect(uri)` | `func Connect(ctx context.Context, uri string) (*Connection, error)` | connect + derive the default database from the URI path |
| `connection` | `type Connection struct{ Client *mongo.Client; … }` | the live client + the default database; `.Close(ctx)` the disconnect |
| `model(name)` | `(*Connection).Model(name string) *mongo.Collection` | the collection for a model name (Node `model`) |
| `Schema` | `type Schema` + `func NewSchema(map[string]any) *Schema` | the schema definition holder |
| `schema.index(name, keys, opts)` | `(*Schema).AddIndex(keys bson.D, name string, opts options.IndexOptions) *Schema` | declare an index |
| `schema.indexes()` | `(*Schema).Apply(ctx, coll *mongo.Collection) error` | create the declared indexes on a collection |
| — | `type IndexSpec` | an index spec value |

## Conventions / gotchas
- **`Connect` derives the database** from the URI path (Node builds
  `settings.mongo.url` the same way — see `go/mongoh` for the shared
  connection-string derivation this mirrors).
- **`Model(name)`** returns a `*mongo.Collection` named for the model — there is
  no mongoose document-layer here (the Node services use raw collections for
  the same reason); the schema/index surface is the only mongoose
  functionality the services rely on.
- **`Apply`** creates the indexes once; it's the `schema.index()` materialisation.

## Testing & coverage
`go test ./go/libraries/mongowrapper/ -count=1 -cover` — the URI/database-name
derivation and the schema/index plumbing are unit-tested (live-mongo batched
update lives in `mongoutils`). **Coverage: 92.5%** (above the 85% gate).

## Dependencies
`go.mongodb.org/mongo-driver` (+ `ollitex/go/mongoh` for shared URI derivation).
