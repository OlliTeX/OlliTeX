// Package mongowrapper is the Go counterpart of `libraries/mongoose-wrapper`
// (npm `@overleaf/mongoose-wrapper`) — a SIX-LINE pure re-export:
//
//	import mongoose from 'mongoose'
//	export default mongoose
//	export const Schema = mongoose.Schema
//	export const model = mongoose.model
//	export const connect = mongoose.connect
//	export const connection = mongoose.connection
//
// Go has no package re-exporting; the package's value (a stable import point
// so a driver swap is one place) is provided by this façade: the Node export
// names (Connect, Connection, Model, Schema) map onto the Go mongo-driver
// surface (`go.mongodb.org/mongo-driver`, already a repo dependency — see
// go/mongoh). No logic is added (the Node wrapper adds none), so nothing
// here is observable beyond the driver it names.
package mongowrapper

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Connection is the Node `connection` export (the live client + the default
// database the connect() URI named).
type Connection struct {
	Client   *mongo.Client
	Database *mongo.Database
}

// Close is a Go lifecycle addition (Node mongoose.close() analogue for tests
// and shutdown).
func (c *Connection) Close(ctx context.Context) error {
	return c.Client.Disconnect(ctx)
}

// Connect mirrors `mongoose.connect(uri)` — dial + auth + ping (Node's
// connect resolves once the server is reachable), exposing the URI's default
// database (Node `connection = mongoose.connection`).
func Connect(ctx context.Context, uri string) (*Connection, error) {
	name, client, err := connectWithDBName(ctx, uri)
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}
	return &Connection{Client: client, Database: client.Database(name)}, nil
}

// Model mirrors `mongoose.model(name)` — the namespaced collection for the
// connection's default database (Node mongoose.model(name) resolves the
// model's collection inside the default db).
func (c *Connection) Model(name string) *mongo.Collection {
	return c.Database.Collection(name)
}

// Schema is the Go stand-in for the re-exported `mongoose.Schema` (a document
// shape definition: typed fields + index specs). It carries no driver logic
// — Apply materialises the indexes onto a collection (the observable
// effect a mongoose schema has on a collection).
type Schema struct {
	Fields  map[string]any
	Indexes []IndexSpec
}

// IndexSpec is one index the schema declares.
type IndexSpec struct {
	Keys bson.D
	Name string
	Opts options.IndexOptions
}

// NewSchema mirrors `new Schema(fields)`.
func NewSchema(fields map[string]any) *Schema {
	return &Schema{Fields: fields}
}

// AddIndex declares an index (mongoose `schema.index({...})`).
func (s *Schema) AddIndex(keys bson.D, name string, opts options.IndexOptions) *Schema {
	if name == "" {
		// mongoose auto-name: concatenates <key>_<dir> pairs.
		parts := make([]string, len(keys))
		for i, k := range keys {
			dir := "1"
			if v, ok := k.Value.(int32); ok && int(v) < 0 {
				dir = "-1"
			}
			parts[i] = k.Key + "_" + dir
		}
		name = ""
		for i, p := range parts {
			if i > 0 {
				name += "_"
			}
			name += p
		}
	}
	s.Indexes = append(s.Indexes, IndexSpec{Keys: keys, Name: name, Opts: opts})
	return s
}

// Apply mirrors the driver-visible half of a mongoose schema: create the
// declared indexes on the collection.
func (s *Schema) Apply(ctx context.Context, coll *mongo.Collection) error {
	if len(s.Indexes) == 0 {
		return nil
	}
	models := make([]mongo.IndexModel, 0, len(s.Indexes))
	for _, spec := range s.Indexes {
		opts := spec.Opts
		if opts.Name == nil {
			name := spec.Name
			opts.Name = &name
		}
		models = append(models, mongo.IndexModel{
			Keys:    spec.Keys,
			Options: &opts,
		})
	}
	_, err := coll.Indexes().CreateMany(ctx, models)
	return err
}
