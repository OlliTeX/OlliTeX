// Package systemmessages ports GET /system/messages (P1 wave).
//
// Node contract (pinned live, 2026-09-13):
//
//	anonymous            → 200 []            (whitelisted route)
//	logged-in, site open → 200 [ {content, _id, [placements]}, ... ]
//	                       (mongo `system_messages`, natural order; the Node
//	                       manager caches but the value is the collection)
//	site closed (SITE_OPEN=false) → 200 [ {content:"SITE IS CLOSED TO
//	  PUBLIC. OPEN ONLY FOR SITE ADMINS. DO NOT EDIT PROJECTS.",
//	  _id:"protected"} ]
package systemmessages

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"ollitex/go/services/web/core"
)

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "systemmessages",
		Routes: []core.Route{
			// Node: webRouter.get('/system/messages') after the gate, but
			// whitelisted (addEndpointToLoginWhitelist, router.mjs:336) and
			// its handler treats anonymous explicitly → NoLogin.
			{Method: "GET", Path: "/system/messages", NoLogin: true, Handler: handler(a)},
		},
	}
}

// siteIsOpen mirrors settings.defaults.js:761 (`SITE_OPEN !== 'false'`).
func siteIsOpen() bool {
	return os.Getenv("SITE_OPEN") != "false"
}

func handler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
		res.W.Header().Set("X-Powered-By", "Express")
		res.W.WriteHeader(200)

		loggedIn := cxt.Sess != nil && cxt.Sess.IsLoggedIn()
		if !loggedIn || !siteIsOpen() {
			if !loggedIn {
				_, _ = io.WriteString(res.W, "[]")
				return
			}
			// site closed: the single admin notice replaces everything
			_, _ = io.WriteString(res.W, `[{"content":"SITE IS CLOSED TO PUBLIC. OPEN ONLY FOR SITE ADMINS. DO NOT EDIT PROJECTS.","_id":"protected"}]`)
			return
		}
		if a.Mongo == nil {
			// Node would 500; the e2e/live deployments wire mongo.
			res.W.WriteHeader(200)
			_, _ = io.WriteString(res.W, "[]")
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.W.WriteHeader(200)
			_, _ = io.WriteString(res.W, "[]")
			return
		}
		cur, err := db.Collection("system_messages").Find(ctx, bson.D{})
		if err != nil {
			res.W.WriteHeader(200)
			_, _ = io.WriteString(res.W, "[]")
			return
		}
		defer cur.Close(ctx)
		type msg struct {
			Content    string   `json:"content"`
			ID         string   `json:"_id"`
			Placements []string `json:"placements,omitempty"`
		}
		out := make([]msg, 0)
		for cur.Next(ctx) {
			m := msg{}
			_ = cur.Decode(&m)
			out = append(out, m)
		}
		b, err := json.Marshal(out)
		if err != nil {
			_, _ = io.WriteString(res.W, "[]")
			return
		}
		_, _ = res.W.Write(b)
	}
}
