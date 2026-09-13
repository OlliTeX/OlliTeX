// Command web is the Go replacement of services/web (WEB_GO_PLAN.md).
//
// One binary, two profiles — selected by ENABLED_SERVICES exactly like
// the Node app (server-ce/runit/web-overleaf/run sets web,
// web-api-overleaf/run sets api). P0 runs as the SHADOW service
// (web-go-overleaf) on a distinct port; the nginx vhost-extras flip
// table routes individual prefixes here. See WEB_GO_PLAN.md §3.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"ollitex/go/mongoh"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/authpages"
	"ollitex/go/services/web/features/devcsrf"
	"ollitex/go/services/web/features/healthcheck"
	"ollitex/go/services/web/features/staticpages"
	"ollitex/go/services/web/features/status"
	"ollitex/go/services/web/features/systemmessages"
	"ollitex/go/services/web/views"
)

func probeEpoch(uri string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, err := mongoh.Connect(ctx, mongoh.Options{URI: uri})
	if err != nil {
		log.Printf("EPOCH_PROBE connect: %v", err)
		return
	}
	defer c.Disconnect(ctx)
	db := c.Database(mongoh.DBFromURI(uri, "sharelatex"))
	u := struct {
		ID any `bson:"_id"`
	}{}
	if err2 := db.Collection("users").FindOne(ctx, bson.D{{Key: "email", Value: "e2e-user@e2e.test"}}).Decode(&u); err2 != nil {
		log.Printf("EPOCH_PROBE user: %v", err2)
		return
	}
	log.Printf("EPOCH_PROBE id=%v idType=%T", u.ID, u.ID)
	eh := struct {
		LoginEpoch any `bson:"loginEpoch"`
	}{}
	if err2 := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: u.ID}}).Decode(&eh); err2 != nil {
		log.Printf("EPOCH_PROBE epoch: %v", err2)
	} else {
		log.Printf("EPOCH_PROBE user=%v epoch=%v type=%T", u.ID, eh.LoginEpoch, eh.LoginEpoch)
	}
	filter := bson.D{{Key: "_id", Value: u.ID}}
	if eh.LoginEpoch != nil {
		filter = append(filter, bson.E{Key: "loginEpoch", Value: eh.LoginEpoch})
	}
	ur, perr := db.Collection("users").UpdateOne(ctx, filter, bson.D{{Key: "$inc", Value: bson.D{{Key: "loginEpoch", Value: 1}}}})
	log.Printf("EPOCH_PROBE update: matched=%d modified=%d ur=%v err=%v", func() int {
		if ur == nil {
			return -1
		}
		return int(ur.MatchedCount)
	}(), func() int {
		if ur == nil {
			return -1
		}
		return int(ur.ModifiedCount)
	}(), ur != nil, perr)
}

func main() {
	cfg, err := core.LoadConfig()
	if err != nil {
		log.Fatalf("webgo: %v", err)
	}

	if os.Getenv("WEB_GO_EPOCH_PROBE") == "1" {
		// one-shot diagnostic (loginEpoch bson type check) — exits after.
		probeEpoch(cfg.MongoURI)
		return
	}

	rdb, err := core.DialRedis(cfg.RedisAddr)
	if err != nil {
		// Node hard-exits on redis connect failure; for the SHADOW service
		// we stay up (health endpoints report 500 until redis is reachable)
		// and re-dial transparently per command.
		log.Printf("webgo: redis %s unreachable at boot: %v (re-dialing per command)", cfg.RedisAddr, err)
		rdb = &core.RedisClient{Addr: cfg.RedisAddr}
	}

	app := core.New(cfg, rdb)
	app.SetMongo(core.NewMongoLazy(cfg.MongoURI))
	app.RegisterFeature(status.Feature)
	app.RegisterFeature(healthcheck.New(app))
	app.RegisterFeature(devcsrf.Feature)

	// P1 surface: auth + static + system messages.
	app.RegisterFeature(authpages.Feature(app))
	app.RegisterFeature(staticpages.Feature(app))
	app.RegisterFeature(systemmessages.Feature(app))

	// web profile: unknown-route 404 view (general/404) — Node
	// webRouter.get('*', ErrorController.notFound).
	app.SetRender404(func(cxt *core.Cxt, res *core.Res) {
		tok := ""
		if cxt.Sess != nil {
			tok = cxt.Sess.CsrfToken()
		}
		origin := cfg.SiteURL
		if origin == "" {
			origin = "http://" + cxt.Req.Host
		}
		// 404 dynamic slots: the request path + the session user (the
		// skeleton was captured logged-in; Node renders both per-request).
		pe, uid := "", ""
		if cxt.Sess != nil {
			if raw, ok := cxt.Sess.GetRaw("passport"); ok {
				var pp struct {
					User struct {
						Email string `json:"email"`
						ID    string `json:"_id"`
					} `json:"user"`
				}
				if json.Unmarshal(raw, &pp) == nil {
					pe, uid = pp.User.Email, pp.User.ID
				}
			}
		}
		// The skeleton carries the slash ("7420/<path>"); trim our leading
		// slash so the render is "7420/zzz-..." and not "7420//zzz-...".
		pth := strings.TrimPrefix(cxt.Req.URL.Path, "/")
		views.NotFoundPage(res.W, views.PageData{CSRFToken: tok, Nonce: views.NewNonce(), Origin: origin, Path: pth, UserEmail: pe, UserID: uid})
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// graceful shutdown (GracefulShutdown parity, minimal: stop
	// accepting, drain in-flight)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Printf("webgo: shutting down")
		shctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(shctx)
	}()

	log.Printf("webgo: profile=%s listening on %s (shadow of services/web)", cfg.Profile, cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("webgo: %v", err)
	}
	// graceful shutdown handled above; reaching here means ListenAndServe
	// returned cleanly after Shutdown.
}
