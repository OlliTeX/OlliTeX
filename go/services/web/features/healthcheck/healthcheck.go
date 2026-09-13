// Package healthcheck implements the web app /health_check endpoints
// (HealthCheckController.mjs + /status semantics). Pinned live (ol-e2e,
// 2026-09-16):
//
//	GET /health_check/redis → 200 "OK" (redis PING) | 500 "Internal Server Error"
//	GET /health_check/mongo → 200 | 500 ("Internal Server Error" when
//	                             SMOKE_TEST_USER_ID unset/lookup empty —
//	                             observed 500 on the e2e stack exactly)
//	GET /health_check/api   → checkActiveHandles (no-op, maxActiveHandles
//	                             unset) + checkApi (redis PING +
//	                             smokeTest.userId present + user email)
//
// All: res.sendStatus → text/plain; charset=utf-8, weak ETag, NO nosniff
// (distinguishable from the plainTextResponse /status — pinned).
//
// Note: the in-image nginx returns 404 'Not found' (application/octet-
// stream) for ^/health_check EXTERNALLY — that block stays; the flip
// uses a more specific `location ^~ /health_check/<x>` in the
// vhost-extras file so the Go app actually gets these paths.
package healthcheck

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/mongoh"
	"ollitex/go/services/web/core"
)

type deps struct {
	mongo *mongo.Client
}

func redisCheck(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if err := a.Redis.PING(); err != nil {
			res.SendStatus(500)
			return
		}
		res.SendStatus(200)
	}
}

func mongoUserEmail(ctx context.Context, client *mongo.Client, uri, userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	db := client.Database(mongoh.DBFromURI(uri, "sharelatex"))
	var doc struct {
		Email *string `bson:"email"`
	}
	err := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: userID}}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "", nil
		}
		return "", err
	}
	if doc.Email == nil {
		return "", nil
	}
	return *doc.Email, nil
}

func mongoCheck(a *core.App, d *deps) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		email, err := mongoUserEmail(ctx, d.mongo, a.Cfg.MongoURI, a.Cfg.SmokeTestUserID)
		if err != nil || email == "" {
			// Node: checkMongo — err OR email==null → res.sendStatus(500)
			res.SendStatus(500)
			return
		}
		res.SendStatus(200)
	}
}

func apiCheck(a *core.App, d *deps) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		// checkActiveHandles: no-op unless settings.maxActiveHandles > 0
		// (not set in server-ce) → straight to checkApi.
		if err := a.Redis.PING(); err != nil {
			res.SendStatus(500)
			return
		}
		if a.Cfg.SmokeTestUserID == "" {
			// Node: "smokeTest.userId is undefined" → 404
			res.SendStatus(404)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		email, err := mongoUserEmail(ctx, d.mongo, a.Cfg.MongoURI, a.Cfg.SmokeTestUserID)
		if err != nil || email == "" {
			res.SendStatus(500)
			return
		}
		res.SendStatus(200)
	}
}

// New registers the feature plus its mongo dependency (connected lazily
// by the caller via NewWithMongo for test injection).
func New(a *core.App) core.Feature {
	d := &deps{}
	if a.Cfg.MongoURI != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		client, err := mongoh.Connect(ctx, mongoh.Options{URI: a.Cfg.MongoURI})
		cancel()
		if err == nil {
			d.mongo = client
		}
	}
	// All three ride Node's privateApiRouter (WEB_GO_ROUTES.csv) → no
	// session middleware, no csrf, no login gate: NoSession parity.
	return core.Feature{
		Name: "healthcheck",
		Routes: []core.Route{
			{Method: "GET", Path: "/health_check/redis", NoLogin: true, NoSession: true, Handler: redisCheck(a)},
			{Method: "GET", Path: "/health_check/mongo", NoLogin: true, NoSession: true, Handler: mongoCheck(a, d)},
			{Method: "GET", Path: "/health_check/api", NoLogin: true, NoSession: true, Handler: apiCheck(a, d)},
		},
	}
}
