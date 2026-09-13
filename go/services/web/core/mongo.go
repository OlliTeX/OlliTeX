package core

import (
	"context"
	"sync"

	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/mongoh"
)

// MongoLazy dials once per process (features share the client). The connection
// is established lazily so the shadow service boots green even when mongo is
// briefly unreachable (same tolerance as redis).
type MongoLazy struct {
	uri     string
	db      string
	once    sync.Once
	client  *mongo.Client
	connect error
}

func NewMongoLazy(uri string) *MongoLazy {
	if uri == "" {
		uri = mongoh.DefaultURI()
	}
	return &MongoLazy{uri: uri, db: mongoh.DBFromURI(uri, "sharelatex")}
}

// Client returns the shared *mongo.Client (first call dials + pings, 5s cap).
func (m *MongoLazy) Client(ctx context.Context) (*mongo.Client, error) {
	m.once.Do(func() {
		c, err := mongoh.Connect(ctx, mongoh.Options{URI: m.uri})
		m.client, m.connect = c, err
	})
	if m.connect != nil {
		return nil, m.connect
	}
	return m.client, nil
}

// DB returns the URI database (default "sharelatex").
func (m *MongoLazy) DB(ctx context.Context) (*mongo.Database, error) {
	c, err := m.Client(ctx)
	if err != nil {
		return nil, err
	}
	return c.Database(m.db), nil
}

// Close releases the pool at shutdown.
func (m *MongoLazy) Close(ctx context.Context) error {
	if m.client == nil {
		return nil
	}
	return m.client.Disconnect(ctx)
}
