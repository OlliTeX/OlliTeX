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
	"ollitex/go/services/web/features/adminusers"
	"ollitex/go/services/web/features/authpages"
	"ollitex/go/services/web/features/compile"
	"ollitex/go/services/web/features/devcsrf"
	"ollitex/go/services/web/features/dropbox"
	"ollitex/go/services/web/features/editorpages"
	"ollitex/go/services/web/features/healthcheck"
	"ollitex/go/services/web/features/hub"
	"ollitex/go/services/web/features/instancestats"
	"ollitex/go/services/web/features/launchpad"
	"ollitex/go/services/web/features/gitbridge"
	"ollitex/go/services/web/features/library"
	"ollitex/go/services/web/features/llmsettings"
	"ollitex/go/services/web/features/mendeley"
	"ollitex/go/services/web/features/languagetool"
	"ollitex/go/services/web/features/notifications"
	"ollitex/go/services/web/features/orcidpicker"
	"ollitex/go/services/web/features/pageshells"
	"ollitex/go/services/web/features/passwordreset"
	"ollitex/go/services/web/features/projectlist"
	"ollitex/go/services/web/features/registrationpage"
	"ollitex/go/services/web/features/serveradmin"
	"ollitex/go/services/web/features/sitesettings"
	"ollitex/go/services/web/features/staticpages"
	"ollitex/go/services/web/features/status"
	"ollitex/go/services/web/features/systemmessages"
	"ollitex/go/services/web/features/templates"
	"ollitex/go/services/web/features/tokenaccess"
	"ollitex/go/services/web/features/texfmt"
	"ollitex/go/services/web/features/trackchanges"
	"ollitex/go/services/web/features/userpages"
	"ollitex/go/services/web/features/webdav"
	"ollitex/go/services/web/features/zotero"
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
	app.RegisterFeature(passwordreset.Feature(app))
	app.RegisterFeature(tokenaccess.Feature(app))

	// P3.1 surface: ServerAdmin leaf — system-message CRUD + editor gate.
	app.RegisterFeature(serveradmin.Feature(app))
	app.RegisterFeature(instancestats.Feature(app))
	app.RegisterFeature(userpages.Feature(app))
	app.RegisterFeature(registrationpage.Feature(app))

	// P6.20 surface: launchpad (first-admin bootstrap) — the last P6 flip.
	// Oracle + bake pins in go/services/web/features/launchpad +
	// go/services/web/views/pages_data_p620.go.
	app.RegisterFeature(launchpad.Feature(app))

	// P3.6 surface: Manage/Site SiteSettings leaf.
	app.RegisterFeature(sitesettings.Feature(app))

	// P4.1 surface: project list (GET /user/projects).
	app.RegisterFeature(projectlist.Feature(app))
	app.RegisterFeature(projectlist.AdminFeature(app))
	app.RegisterFeature(adminusers.Feature(app))

	// P6.4a surface: OlliTeX llm module settings surface (BYO provider rows,
	// selected model, compliance rubrics, usage, grammar prefs, admin LLM
	// settings file + check/scan/usage; chat/completion/review is P6.4b).
	app.RegisterFeature(llmsettings.Feature(app))

	// P6.5 surface: bib-editor library (GET/POST/… /library/references*,
	// PATCH /library/references/:key, + the two /library pages).
	app.RegisterFeature(library.Feature(app))

	// P6.6 surface: zotero module (/user/zotero/* — status/unlink/groups/
	// oauth(+callback)/picker libraries|collections|items|bibtex).
	app.RegisterFeature(zotero.Feature(app))

	// P6.7 surface: orcid-picker module (/orcid-picker/search|works|fetch-bib
	// — live ORCID pub API; SSRF-guarded; deterministic 400/502/200 pins).
	app.RegisterFeature(orcidpicker.Feature(app))

	// P6.8 surface: mendeley module (/user/mendeley/status|oauth(+callback),
	// /mendeley/groups, POST /mendeley/unlink — unconfigured sandbox pins).
	app.RegisterFeature(mendeley.Feature(app))

	// P6.9 surface: webdav module (/user/webdav/status|connect|disconnect,
	// /project/:id/webdav/* (state|files|pull|push|conflict/resolve|link|
	// project-name), DELETE state, POST /project/new/webdav) — unlinked
	// sandbox pins (WEBDAV_ENABLED=true in the e2e env).
	app.RegisterFeature(webdav.Feature(app))

	// P6.10 surface: dropbox module (/user/dropbox/* status|connect|disconnect|
	// oauth2|oauth/callback, /project/:id/dropbox/* (state|link|pull|push|files),
	// DELETE state, POST /project/new/dropbox) — unlinked sandbox pins
	// (DROPBOX_ENABLED=true in the e2e env; authz via the shared P4 chain).
	app.RegisterFeature(dropbox.Feature(app))

	// P6.12 surface: track-changes module (11 routes under /project/:id/...
	// — track_changes state, accept-changes, ranges, changes/users, threads,
	// comment send/edit/delete, thread resolve/reopen/delete)
	app.RegisterFeature(trackchanges.Feature(app))

	// P6.13 surface: template gallery (redirects / 3 public JSON routes /
	// preview + bundle asset streams / six management routes 403 in this
	// profile — no user carries template admin rights).
	app.RegisterFeature(templates.Feature(app))

	// P6.14 surface: notifications preferences (global + per-project GET/POST,
	// /user/notification-preferences 301s, /user/send-test-email).
	app.RegisterFeature(notifications.Feature(app))

	// P6.15 surface: LanguageTool proxy (languages, check, admin connection
	// check).
	app.RegisterFeature(languagetool.Feature(app))

	// P6.17 surface: tex-autoformatter module (POST /api/format-tex —
	// tex-fmt spawn or the bibtex normalizer for .bib filenames).
	app.RegisterFeature(texfmt.Feature(app))

	// P6.18 surface: page-shells module — the legacy shell pages are
	// removed (hubs are the settings surfaces): GET /user/mysettings →
	// 301 /hub#/mysettings.account; GET /admin/panel → 301 /hub#/overview
	// (site admin — non-admin bounces to /restricted?from=…).
	app.RegisterFeature(pageshells.Feature(app))

	// P6.19 surface: git-bridge web module — PAT endpoints
	// (/git-bridge/personal-access-tokens*), /oauth/token/info, and the
	// bridge-called API (GET/POST /api/v0/docs/:p[/snapshots...]).
	app.RegisterFeature(gitbridge.Feature(app))

	// P5.1a surface: editor page (GET /editor/:id + /Project/:id).
	app.RegisterFeature(editorpages.Feature(app))

	// P5.2a surface: compile control plane (POST /Project/:id/compile +
	// /compile/stop) — clsi stays Node; Go is the orchestration/response layer.
	app.RegisterFeature(compile.Feature(app))

	// P6 surface (P6.1): ollitex-hub module (/hub page, /hub legacy
	// redirects, /api/hub-theme theme API, /api/hub/health, /api/hub/notes).
	app.RegisterFeature(hub.Feature(app))

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
				views.NotFoundPage(res.W, views.PageData{CSRFToken: tok, Nonce: views.NewNonce(), Origin: origin, Path: pth, UserEmail: pe, UserID: uid, CanManageTemplateMenu: templates.SessionMenuGrant(cxt.Sess), NavAdmin: tplNavAdmin(cxt.Sess)})
	})

	// web profile: rendered 403 page (general/restricted) — the global
	// auth-chain renders it for anonymous non-GET on restricted routes;
	// the templates feature renders it for logged-in non-privileged mgmt.

	// web profile: rendered 500 page (general/500) — the ServerAdmin
	// leaf routes error into it (pinned: nonce CSP + Permissions-Policy
	// + 681-byte deterministic body + weak ETag).
	app.SetRender500(func(cxt *core.Cxt, res *core.Res) {
		origin := cfg.SiteURL
		if origin == "" {
			origin = "http://" + cxt.Req.Host
		}
		adminEmail := os.Getenv("ADMIN_EMAIL")
		if adminEmail == "" {
			adminEmail = "placeholder@example.com"
		}
		views.Error500Page(res.W, views.PageData{Nonce: views.NewNonce(), Origin: origin, AdminEmail: adminEmail})
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

// tplNavAdmin: admin navbar fragment for per-request page renders (the
// generic 404/500 skeletons are captured from a non-admin render and
// carry the slot empty).
func tplNavAdmin(sess *core.Session) string {
	if templates.SessionIsAdmin(sess) {
		return views.AdminNavFragment
	}
	return ""
}
