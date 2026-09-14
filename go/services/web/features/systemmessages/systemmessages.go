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
	"fmt"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

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
		var body []byte
		if logged := cxt.Sess != nil && cxt.Sess.IsLoggedIn(); !logged {
			body = []byte("[]")
		} else if !siteIsOpen() {
			// site closed: the single admin notice replaces everything
			body = []byte(`[{"content":"SITE IS CLOSED TO PUBLIC. OPEN ONLY FOR SITE ADMINS. DO NOT EDIT PROJECTS.","_id":"protected"}]`)
		} else if a.Mongo == nil {
			// Node would 500; the e2e/live deployments wire mongo.
			body = []byte("[]")
		} else {
			ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
			defer cancel()
			db, err := a.Mongo.DB(ctx)
			if err != nil {
				body = []byte("[]")
			} else {
				cur, err := db.Collection("systemmessages").Find(ctx, bson.D{})
				if err != nil {
					body = []byte("[]")
				} else {
					defer cur.Close(ctx)
					var docs []map[string]any
					for cur.Next(ctx) {
						var m map[string]any
						if cur.Decode(&m) == nil {
							docs = append(docs, m)
						}
					}
					if b, err := marshalMessages(docs); err == nil {
						body = b
					} else {
						body = []byte("[]")
					}
				}
			}
		}
		res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
		res.W.Header().Set("X-Powered-By", "Express")
		res.W.Header().Set("ETag", core.EtagWeakBody(string(body)))
		res.W.Header().Set("Content-Length", fmt.Sprint(len(body)))
		res.W.WriteHeader(200)
		_, _ = res.W.Write(body)
	}
}

// marshalMessages serializes the system_messages docs in the exact Node
// (mongoose toJSON) key order — pinned live P3.1:
// [{"_id":"<hex>","content":<str>,"placements":[...],"__v":0}] — with
// placements / __v retained ONLY when present in the stored doc.
func marshalMessages(docs []map[string]any) ([]byte, error) {
	type kv struct {
		k string
		v any
	}
	var parts []string
	for _, d := range docs {
		out := []kv{{"_id", d["_id"]}}
		if c, ok := d["content"]; ok {
			out = append(out, kv{"content", c})
		}
		if p, ok := d["placements"]; ok {
			out = append(out, kv{"placements", p})
		}
		if v, ok := d["__v"]; ok {
			out = append(out, kv{"__v", v})
		}
		ob := &strings.Builder{}
		ob.WriteString("{")
		for i, e := range out {
			if i > 0 {
				ob.WriteString(",")
			}
			ob.WriteString(`"` + e.k + `":`)
			bv, err := json.Marshal(e.v)
			if err != nil {
				bv = []byte("null")
			}
			if e.k == "_id" {
				// ObjectID → hex string (mongoose toJSON), not a JSON object.
				if oid, ok := e.v.(primitive.ObjectID); ok {
					ob.WriteString(`"` + oid.Hex() + `"`)
					continue
				}
				if s, ok := e.v.(string); ok {
					ob.WriteString(`"` + s + `"`)
					continue
				}
			}
			ob.Write(bv)
		}
		ob.WriteString("}")
		parts = append(parts, ob.String())
	}
	if len(parts) == 0 {
		return []byte("[]"), nil
	}
	return []byte("[" + strings.Join(parts, ",") + "]"), nil
}
