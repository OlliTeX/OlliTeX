// P6.10 — dropbox module surface for the OlliTeX Go web (WEB_GO_PLAN).
//
// Node module oracle (services/web/modules/dropbox, 12 routes — full pin
// table in WEB_GO_PLAN.md P6.10): user oauth2/callback/status/connect/
// disconnect; project state (GET, LOGIN-ONLY: no authz, no zod — bad/ghost/
// other all → 200 {"connected":false} vs state DELETE/link/pull/push/files
// behind ensureUserCanWriteProjectContent); new/dropbox import.
//
// Storage (live-verified, same traps as P6.9 webdav):
//
//	dropboxusercredentials — all-lowercase mongoose plural; doc {userId:
//	<HEX STRING>, accessToken: <token>, path: string, refreshToken?: string}
//	dropboxsyncprojectstates — {projectId: <string>, connected, path,
//	ownerId: <string>, …}
//
// Cipher (DropboxCredentials.mjs): AES-256-GCM, token =
// base64( iv12 ‖ ct ‖ tag16 ). Node's decrypt reads the embedded bytes via
// buffer.toString('base64') → base64-decode — an IDENTITY — so plain
// GCM is the faithful port and tokens round-trip on both stacks under the
// same env keys. Key = sha256('overleaf-dropbox-credentials-v2|' +
// WEBDAV_TOKEN_CIPHER_PASSWORD) (or the SECRET_TOKEN fallback); without
// either env the connect handler 500s with the exact pinned message.
// Legacy decrypt candidates: the 32-char prefix + NODE_ENV keys (String
// padEnd(32,'x').slice(0,32) semantics).
//
// Scope note: the Dropbox API client is implemented in client.go for live
// deployments; the sandbox (no account, no app keys, no network) pins the
// offline-deterministic surface only.

package dropbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var dbxHex24 = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// dbxBadOID404 — shared P4 zod param schema (params.project_id).
const dbxBadOID404 = `{"error":"Validation error: Invalid Mongo ObjectId at \"params.project_id\"","statusCode":404}`

// Feature registers the 12 dropbox routes.
func Feature(a *core.App) core.Feature {
	p := func(s string) *regexp.Regexp {
		return regexp.MustCompile(`^/project/([^/]+)` + s + `$`)
	}
	return core.Feature{Name: "dropbox", Routes: []core.Route{
		{Method: "GET", Path: "/user/dropbox/status", Handler: hDbxStatus(a)},
		{Method: "POST", Path: "/user/dropbox/connect", Handler: hDbxConnect(a)},
		{Method: "POST", Path: "/user/dropbox/disconnect", Handler: hDbxDisconnect(a)},
		{Method: "GET", Path: "/user/dropbox/oauth2", Handler: hDbxOAuth2(a)},
		{Method: "GET", Path: "/user/dropbox/oauth/callback", Handler: hDbxCallback(a)},
		{Method: "GET", Pattern: p("/dropbox/state$"), Handler: hDbxState(a)},
		{Method: "DELETE", Pattern: p("/dropbox/state$"), Handler: hDbxUnlink(a)},
		{Method: "POST", Pattern: p("/dropbox/link$"), Handler: hDbxLink(a)},
		{Method: "POST", Pattern: p("/dropbox/pull$"), Handler: hDbxPull(a)},
		{Method: "POST", Pattern: p("/dropbox/push$"), Handler: hDbxPush(a)},
		{Method: "GET", Pattern: p("/dropbox/files$"), Handler: hDbxFiles(a)},
		{Method: "POST", Path: "/project/new/dropbox", Handler: hDbxNew(a)},
	}}
}

// ---- shared plumbing --------------------------------------------------------

func dbxUID(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	return cxt.Sess.UserIDHex()
}

func dbxPageData(cxt *core.Cxt, reqPath string) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Path: reqPath}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
	}
	return d
}

// dbxAuthzProject — the /project/:project_id/dropbox/* gate
// (ensureUserCanWriteProjectContent), the shared P4 chain.
func dbxAuthzProject(a *core.App, cxt *core.Cxt, res *core.Res) (string, bool) {
	seg := cxt.Params["1"]
	if !dbxHex24.MatchString(seg) {
		res.JSON(404, []byte(dbxBadOID404))
		return "", false
	}
	oid, oerr := primitive.ObjectIDFromHex(strings.ToLower(seg))
	if oerr != nil {
		res.JSON(404, []byte(dbxBadOID404))
		return "", false
	}
	uid := dbxUID(cxt)
	doc, lerr := dbxLoadProject(a, cxt.Req.Context(), oid)
	if lerr != nil {
		res.JSON(500, []byte(`{"message":"internal error"}`))
		return "", false
	}
	if doc == nil {
		views.NotFoundPage(res.W, dbxPageData(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		return "", false
	}
	if uid == "" || !dbxCanWrite(uid, *doc) {
		if core.AcceptsJSON(cxt.Req) {
			res.JSON(403, []byte(`{"message":"restricted"}`))
		} else {
			views.Restricted403(res.W, dbxPageData(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
		}
		return "", false
	}
	return seg, true
}

func dbxLoadProject(a *core.App, ctx context.Context, oid primitive.ObjectID) (*bson.D, error) {
	if a.Mongo == nil {
		return nil, nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d bson.D
	if err := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func dbxCanWrite(uidHex string, doc bson.D) bool {
	own, _ := dbxDocStr(doc, "owner_ref")
	if own == "" {
		if o, ok := dbxDocVal(doc, "owner"); ok {
			if dm, ok := o.(bson.D); ok {
				for _, k := range []string{"userId", "user_id", "_id"} {
					for _, e := range dm {
						if e.Key == k {
							if s, ok := oidHex(e.Value); ok && s != "" {
								own = s
							}
						}
					}
				}
			}
		}
	}
	if own != "" && strings.EqualFold(own, uidHex) {
		return true
	}
	for _, key := range []string{"collab_refs", "collaborator_refs", "tokenAccessReadAndWrite_refs"} {
		if arr, ok := dbxDocVal(doc, key); ok {
			if items, ok := arr.([]interface{}); ok {
				for _, it := range items {
					if s, ok := oidHex(it); ok && s != "" && strings.EqualFold(s, uidHex) {
						return true
					}
				}
			}
		}
	}
	return false
}

func dbxDocVal(doc bson.D, key string) (interface{}, bool) {
	for _, e := range doc {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func dbxDocStr(doc bson.D, key string) (string, bool) {
	v, ok := dbxDocVal(doc, key)
	if !ok {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	if o, ok := v.(primitive.ObjectID); ok {
		return o.Hex(), true
	}
	return "", false
}

func oidHex(v interface{}) (string, bool) {
	switch t := v.(type) {
	case primitive.ObjectID:
		return t.Hex(), true
	case string:
		if dbxHex24.MatchString(t) {
			return t, true
		}
	}
	return "", false
}

// ---- body helpers -----------------------------------------------------------

type dbxBody map[string]json.RawMessage

func dbxReadBody(cxt *core.Cxt) dbxBody {
	raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	var m dbxBody
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	if m == nil {
		m = dbxBody{}
	}
	return m
}

func dbxStr(m dbxBody, key string) (string, bool) {
	r, ok := m[key]
	if !ok {
		return "", false
	}
	var s *string
	if err := json.Unmarshal(r, &s); err != nil || s == nil {
		return "", false
	}
	return *s, true
}
