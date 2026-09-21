// Package mongoutils is the 1:1 Go port of `libraries/mongo-utils`
// (npm `@overleaf/mongo-utils`): the test-database guard + cleanup/drop
// helpers (test-utils.js) and the ObjectId-time-span batched-update walker
// (batchedUpdate.js).
//
// Go seam: the repo already depends on `go.mongodb.org/mongo-driver` (see
// go/mongoh, and the Go services use it directly) — so the Node
// `mongodb`/`mongodb-legacy` driver objects are replaced by the Go driver's
// `*mongo.Client` / `*mongo.Collection` types, which are exactly what the
// consuming Go code holds. No new dependency.
//
// The Node wrapper's `mongoClient.db()` (the URI's default database) has no
// Go equivalent on `*mongo.Client` (the driver does not expose it), so the
// database name is passed explicitly by the caller — the same value the
// caller used to build the URI (go/mongoh's Options.DB).
package mongoutils
