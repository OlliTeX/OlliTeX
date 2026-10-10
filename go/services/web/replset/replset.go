// Package replset — AG-era fix (owner 2026-10-09): single-member replica
// set bootstrap for the OlliTeX web service.
//
// Why: the web profile uses Mongo 6+ transactions (historyv1 Initialize),
// which demand a replica-set member (a standalone rejects multi-document
// transactions: "Transaction numbers are only allowed on a replica set
// member"). On a FRESH data dir the mongod is booted with --replSet but
// never initiated (the old overleaf CE had the mongosh bootstrap in its
// image init). On a DAEMON RESTART (or any container recreation where the
// stored self-host no longer resolves to itself) the single node can land
// in state REMOVED and never recover — every app write then fails with
// "node is not in primary or recovering state".
//
// This package self-heals at web boot: it checks the node's replica-set
// status and, when the node is NOT a healthy member, reconfigures (or
// initiates) the single-member set over 127.0.0.1 — always self-
// resolvable. Uses the app's own Mongo connection; no extra credentials.
package replset

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Mongo wire replication states.
const (
	StatePRIMARY   = 1
	StateSECONDARY = 2
)

// Boot runs the self-heal for the app's lazy Mongo client (the web main
// calls it as `go replset.Boot(app.Mongo)`). Non-fatal by contract:
// errors are logged, never returned — a transient mongo unavailability
// must not wedge the web boot.
// LazyClient is the narrow surface Boot needs from core.MongoLazy.
type LazyClient interface {
	Client(ctx context.Context) (*mongo.Client, error)
}

func Boot(ctx context.Context, lazy LazyClient) {
	client, err := lazy.Client(ctx)
	if err != nil {
		log.Printf("replset: mongo client unavailable: %v (skipping self-heal)", err)
		return
	}
	ensure(ctx, client)
}

func ensure(ctx context.Context, client *mongo.Client) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	admin := client.Database("admin")

	selfHost := "127.0.0.1:27017"
	cfg := bson.D{
		{Key: "_id", Value: "ollitexrs0"},
		{Key: "members", Value: bson.A{
			bson.D{{Key: "_id", Value: 0}, {Key: "host", Value: selfHost}},
		}},
	}

	// 1) Healthy? → nothing to do.
	if raw, err := run(admin, ctx, bson.D{{Key: "replSetGetStatus", Value: 1}}); err == nil {
		myState := topInt(raw, "myState")
		if (myState == StatePRIMARY || myState == StateSECONDARY) && memberCount(raw) >= 1 {
			return
		}
	}

	// 2) Stored config exists but node is REMOVED / not a healthy member →
	//    reconfig the single-member set. A REMOVED node refuses this (it is
	//    not writable) — fall through to initiate, then to the loud log.
	if raw, err := run(admin, ctx, bson.D{{Key: "replSetReconfig", Value: cfg}}); err == nil && okFlag(raw) {
		log.Printf("replset: reconfigured single-member ollitexrs0 over %s", selfHost)
		settleUntilPrimary(ctx, admin)
		return
	}

	// 3) No local config yet (fresh data dir) → initiate.
	if raw, err := run(admin, ctx, bson.D{{Key: "replSetInitiate", Value: cfg}}); err == nil && okFlag(raw) {
		log.Printf("replset: initiated single-member ollitexrs0 over %s", selfHost)
		settleUntilPrimary(ctx, admin)
		return
	}

	// 4) REMOVED with a stale stored config: both reconfig and initiate are
	//    refused. The clean reset is wiping the node's local replica-set
	//    state (the `local` db) — an operator/deploy decision. Log loudly;
	//    the web service boots regardless (reads may still work).
	log.Printf("replset: node not a healthy member of ollitexrs0; automated reconfig/initiate unavailable — operator reset of local replica-set state required")
}

func run(db *mongo.Database, ctx context.Context, cmd bson.D) (bson.Raw, error) {
	var raw bson.Raw
	err := db.RunCommand(ctx, cmd).Decode(&raw)
	return raw, err
}

func okFlag(raw bson.Raw) bool {
	if v, ok := raw.Lookup("ok").AsInt64OK(); ok {
		return v == 1
	}
	return false
}

func memberCount(raw bson.Raw) int {
	if val, ok := raw.Lookup("members").ArrayOK(); ok {
		if vs, err := val.Values(); err == nil {
			return len(vs)
		}
	}
	return 0
}

func topInt(raw bson.Raw, key string) int {
	if v, ok := raw.Lookup(key).AsInt64OK(); ok {
		return int(v)
	}
	return -1
}

func settleUntilPrimary(ctx context.Context, admin *mongo.Database) {
	deadline := time.Now().Add(15 * time.Second)
	for {
		if raw, err := run(admin, ctx, bson.D{{Key: "replSetGetStatus", Value: 1}}); err == nil {
			if topInt(raw, "myState") == StatePRIMARY {
				return
			}
		}
		if time.Now().After(deadline) {
			log.Printf("replset: primary not reached within 15s (election may still settle)")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
