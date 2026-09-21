package mongoutils

import (
	"context"
	"fmt"
	"os"
	"sync"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// EnsureTestDatabase mirrors the Node ensureTestDatabase guard, byte-for-byte:
//
//	const dbName = mongoClient.db().databaseName
//	const env = process.env.NODE_ENV
//	if (dbName !== 'test-overleaf' || env !== 'test') {
//		throw new Error(`Refusing to clear database '${dbName}' in environment '${env}'`)
//	}
//
// (in Go the driver does not expose the URI's default database name, so the
// caller passes the one it connected with — its go/mongoh Options.DB).
func EnsureTestDatabase(dbName string) error {
	env := os.Getenv("NODE_ENV")
	if dbName != "test-overleaf" || env != "test" {
		return fmt.Errorf("Refusing to clear database '%s' in environment '%s'", dbName, env)
	}
	return nil
}

// CleanupTestDatabase mirrors cleanupTestDatabase(mongoClient):
//
//	ensureTestDatabase(mongoClient)
//	const db = mongoClient.db()
//	const allCollections = await db.collections()
//	const collections = allCollections.filter(coll => coll.collectionName !== 'migrations')
//	await Promise.all(collections.map(coll => coll.deleteMany({})))
//
// This doesn't drop the collections, so indexes are preserved. (Node
// Promise.all parallelism → goroutines.)
func CleanupTestDatabase(ctx context.Context, client *mongo.Client, dbName string) error {
	if err := EnsureTestDatabase(dbName); err != nil {
		return err
	}

	db := client.Database(dbName)
	models, err := listCollectionModels(ctx, db)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(models))
	for _, model := range models {
		if model.Name == "migrations" {
			continue // preserved, Node filter
		}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if _, derr := db.Collection(name).DeleteMany(ctx, bson.D{}); derr != nil {
				errs <- derr
			}
		}(model.Name)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// DropTestDatabase mirrors dropTestDatabase(mongoClient):
//
//	ensureTestDatabase(mongoClient)
//	await mongoClient.db().dropDatabase()
//
// This drops the whole database, including indexes.
func DropTestDatabase(ctx context.Context, client *mongo.Client, dbName string) error {
	if err := EnsureTestDatabase(dbName); err != nil {
		return err
	}
	return client.Database(dbName).Drop(ctx)
}

// collectionModel mirrors Node's CollectionInfo {name, type} shape (all the
// helper needs is the name).
type collectionModel struct {
	Name string `bson:"name"`
	Type string `bson:"type"`
}

// listCollectionModels is `db.collections()`.
func listCollectionModels(ctx context.Context, db *mongo.Database) ([]collectionModel, error) {
	cursor, err := db.ListCollections(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []collectionModel
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
