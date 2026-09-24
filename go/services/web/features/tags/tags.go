// Package tags ports the user project-tag surface (P7 completion unit U1 —
// "complete Go web in e2e-impact order, then hard-cutover").
//
// Node sources (oracle):
//
//	services/web/app/src/router.mjs                         (route wiring + limiter refs)
//	services/web/app/src/Features/Tags/TagsController.mjs   (schemas + handlers)
//	services/web/app/src/Features/Tags/TagsHandler.mjs      (mongo ops, MAX_TAG_LENGTH)
//	services/web/app/src/models/Tag.mjs                     (TAG_COLOR_REGEX; user_id and
//	                                                           project_ids are plain STRINGS)
//	services/web/app/src/infrastructure/RateLimiter.mjs     (redis key rate-limit:<name>:<uid>)
//
// Routes (web profile, Node order — every one requireLogin):
//
//	GET    /tag                                  → 200 [tag...] (natural order, full docs)
//	POST   /tag                                  → 200 tag | 400 VA | 500 page (name>50)
//	POST   /tag/:tagId/rename                    → 204 | 400 VA | 404 bad-oid | 500 page (name>50)
//	POST   /tag/:tagId/edit                      → 204 | 400 VA | 404 bad-oid | 500 page (name>50)
//	DELETE /tag/:tagId                           → 204 | 404 bad-oid
//	POST   /tag/:tagId/project/:projectId        → 204 | 404 bad-oid (either param)
//	POST   /tag/:tagId/projects                  → 204 | 400 VA projectIds | 404 bad-oid tagId
//	POST   /tag/:tagId/projects/remove           → 204 | 400 VA projectIds | 404 bad-oid tagId
//	DELETE /tag/:tagId/project/:projectId        → 204 | 404 bad-oid (either param)
//	GET    /user/:userId/tag                     → U10.3r: basic-auth gate
//	                                               (401/302 + fresh sid), then
//	                                               404 HTML page for every userId
//
// Pinned contracts (live Node oracle 2026-09-22, e2e stack):
//
//   - Tag doc wire JSON: {_id, user_id, name, color?, project_ids:[<24-hex
//     str>...], __v} — stored BSON order; color ABSENT when created without one.
//   - POST /tag NEW-create response key order differs from the duplicate path:
//     new → {"user_id","name","color"?,"project_ids":[],"_id","__v"}
//     dup → stored order (_id first) — Node returns Tag.findOne on duplicate key.
//   - project_ids are stored as HEX STRINGS (mongoose casts ObjectIds to
//     string on the [String] path; the tag filter does includes("<hex>")).
//   - params objectId bad → 404 {"error":"Validation error: Invalid Mongo ObjectId
//     at \"params.<name>\"","statusCode":404}
//   - body objectId bad   → 400 "... at \"body.projectIds[i]\"","statusCode":400
//   - name>50 → 500 HTML error page (Node throws 'Exceeded max tag length').
//   - anon GET  /tag → 401 "Unauthorized" (json accept) / 302 /login
//     anon POST/DELETE → 403 "Forbidden" (core csrf runs first)
//   - rate limits (redis-shared with Node): create-tag 30/60s, rename-tag 10/60s
//     (rename AND edit), delete-tag 30/60s, add/remove single+bulk 10/60s —
//     key = session uid → 429 "Rate limit reached, please try again later"
//     (no content-type, chunked).
//   - rename/edit/delete/add/remove on a tag not owned by the user are silent
//     no-ops (updateOne/deleteOne without match is not an error in Node) → 204.
package tags

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/templates"
	"ollitex/go/services/web/views"
)

// ---------- route patterns (Node router order) ----------

var (
	renamePat     = regexp.MustCompile(`^/tag/([^/]+)/rename$`)
	editPat       = regexp.MustCompile(`^/tag/([^/]+)/edit$`)
	tagIdPat      = regexp.MustCompile(`^/tag/([^/]+)$`)
	memberPat     = regexp.MustCompile(`^/tag/([^/]+)/project/([^/]+)$`)
	addManyPat    = regexp.MustCompile(`^/tag/([^/]+)/projects$`)
	removeManyPat = regexp.MustCompile(`^/tag/([^/]+)/projects/remove$`)
	privTagPat    = regexp.MustCompile(`^/user/([^/]+)/tag$`)
	validOID      = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)
	colorRE       = regexp.MustCompile(`^(#[a-fA-F0-9]{6}|hsl\(\d{1,3}, 70%, 45%\))$`)
)

const (
	maxTagLength = 50 // TagsHandler MAX_TAG_LENGTH
	rateLimitMsg = "Rate limit reached, please try again later"

	// colorIssueMsg — zod's regex failure text (the backslashes are part of
	// the message; already JSON-escaped for the wire body).
	colorIssueMsg = `Invalid string: must match pattern /^(#[a-fA-F0-9]{6}|hsl\\(\\d{1,3}, 70%, 45%\\))$/ at \"body.color\"`
)

// malformedParam — Node's 404 JSON for a bad route ObjectId (pin: statusCode
// 404, NOT 400; message embeds the route param name).
func malformedParam(name string) []byte {
	return []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.` + name + `\"","statusCode":404}`)
}

func va400(msg string) []byte {
	return []byte(`{"error":"Validation error: ` + msg + `","statusCode":400}`)
}

// Feature registers the tag routes.
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "tags",
		Routes: []core.Route{
			{Method: "GET", Path: "/tag", Handler: getAllTags(a)},
			{Method: "POST", Path: "/tag", Handler: createTagHandler(a)},
			{Method: "POST", Pattern: renamePat, Handler: renameTagHandler(a)},
			{Method: "POST", Pattern: editPat, Handler: editTagHandler(a)},
			{Method: "DELETE", Pattern: tagIdPat, Handler: deleteTagHandler(a)},
			{Method: "POST", Pattern: memberPat, Handler: memberOpHandler(a, "add")},
			{Method: "POST", Pattern: addManyPat, Handler: addProjectsHandler(a)},
			{Method: "DELETE", Pattern: memberPat, Handler: memberOpHandler(a, "remove")},
			{Method: "POST", Pattern: removeManyPat, Handler: removeProjectsHandler(a)},
			// Node: this route lives on privateApiRouter (x-api-key surface)
			// and is SESSION-dependent: with a valid logged-in session (the U1
			// gate's context) the Node stack renders the standard 404 HTML page
			// for every userId (the fork's apiGetAllTags falls through to
			// NotFound); over a bare/no-session hit it is a basic-auth 401/302.
			// The Go web shadow pins the U1 (session) oracle — the 404 page —
			// which is the live, green, committed gate for this route; the
			// no-session 401/302 surface is a Node context that the Go drop-in
			// does not reproduce here (NoSession route). U1 parity is
			// authoritative for this route.
			{Method: "GET", Pattern: privTagPat, NoLogin: true, Handler: func(cxt *core.Cxt, res *core.Res) {
				views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			}},
			// U-API (privateApiRouter): the api-profile (basic-auth) variant of
			// GET /user/:userId/tag — Node TagsController.apiGetAllTags. Wire
			// (Node api :3000, live-pinned 2026-09-23): unauth/wrong → 401
			// (challenge); userId not a 24-hex oid → 404 JSON VA (params.userId);
			// valid oid (ghost OR real) → 200 [tags] / []. APIOnly → the web
			// profile SKIPS this (serving the U1 404-page oracle above); the
			// routeNoSession fix keeps the web 404 page session-correct (u1
			// green). Registered AFTER the web oracle, so on the web profile the
			// NoLogin 404-page handler is reached first.
			{Method: "GET", Pattern: privTagPat, NoSession: true, APIOnly: true, Handler: apiTagGetHandler(a)},
		},
	}
}

// apiTagGetHandler — Node privateApiRouter GET /user/:userId/tag
// (TagsController.apiGetAllTags → _getTags → Tag.find({user_id}) →
// res.json(allTags)). Reuses the web tag machinery (oidParam for the
// 404-VA params.userId wire, mongoRun + dJSONTag for the tag-array body).
func apiTagGetHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		if c.A.Cfg.Profile != "api" {
			// Defensive: core.App skips APIOnly routes on the web profile, so
			// this is unreachable on :4000; if it were, mirror the 404-VA wire.
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, malformedParam("userId"))
			return
		}
		if !a.APIBasicGate401(c, r, req) {
			return // unauth / wrong basic → 401 challenge wire
		}
		// The api-profile wire carries X-Powered-By: Express on the 404-VA and
		// 200 responses (Node sets it on the VA path + res.json). oidParam does
		// NOT set it (the web tag handlers don't need it), so set it here first
		// so the 404-VA response carries it (pinned: Node :3000 tag-invalid =
		// XPB present; the Go leg was missing it → uapi diff).
		r.W.Header().Set("X-Powered-By", "Express")
		uid, ok := oidParam(c, r, "1", "userId") // not 24-hex → 404 VA (params.userId)
		if !ok {
			return
		}
		// Node: Tag.find({ user_id: userId }) → res.json(allTags). An absent
		// user yields an empty cursor → [] → 200 (not a 404).
		var docs []primitive.D
		err := mongoRun(a, c, func(db *mongo.Database, ctx context.Context) error {
			cur, e := db.Collection("tags").Find(ctx, bson.D{{Key: "user_id", Value: uid}})
			if e != nil {
				return e
			}
			defer cur.Close(ctx)
			for cur.Next(ctx) {
				var d primitive.D
				if decErr := cur.Decode(&d); decErr != nil {
					continue
				}
				docs = append(docs, d)
			}
			return cur.Err()
		})
		if err != nil {
			serve500Page(c, r)
			return
		}
		out := make([]string, 0, len(docs))
		for _, d := range docs {
			out = append(out, string(dJSONTag(d)))
		}
		r.W.Header().Set("X-Powered-By", "Express")
		r.JSON(200, []byte("["+strings.Join(out, ",")+"]"))
	}
}

// ---------- session gate (defensive; the global login gate covers anon) ----------

func userGate(cxt *core.Cxt, res *core.Res) (string, bool) {
	if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
		if core.AcceptsJSON(cxt.Req) {
			res.SendStatus(401)
		} else {
			res.Redirect(cxt.Req, 302, "/login")
		}
		return "", false
	}
	return cxt.Sess.UserIDHex(), true
}

// ---------- oid route params (404 pin) ----------

func oidParam(cxt *core.Cxt, res *core.Res, group, param string) (primitive.ObjectID, bool) {
	p := cxt.Params[group]
	if !validOID.MatchString(p) {
		res.JSON(404, malformedParam(param))
		return primitive.ObjectID{}, false
	}
	o, err := primitive.ObjectIDFromHex(strings.ToLower(p))
	if err != nil {
		res.JSON(404, malformedParam(param))
		return primitive.ObjectID{}, false
	}
	return o, true
}

// hexParam — like oidParam but returns the (lowercase) hex string:
// project_ids entries are stored as strings.
func hexParam(cxt *core.Cxt, res *core.Res, group, param string) (string, bool) {
	p := cxt.Params[group]
	if !validOID.MatchString(p) {
		res.JSON(404, malformedParam(param))
		return "", false
	}
	o, err := primitive.ObjectIDFromHex(strings.ToLower(p))
	if err != nil {
		res.JSON(404, malformedParam(param))
		return "", false
	}
	return o.Hex(), true
}

// ---------- body reading (pinned: express.json + zod wire) ----------

// readBody — the pinned mapping (live-verified on the identical webRouter
// express.json stack, serveradmin gates):
//
//	""            → {} (empty object)
//	valid object  → map + top-level key order (zod reports unknown keys in
//	                                   document order)
//	valid "null"  → kind "null"   → zod: "received null at body" 400
//	valid array   → kind "array"  → zod: "received array at body" 400
//	scalar        → kind "scalar" → express.json strict rejects BEFORE the
//	                                 handler → 400 "{}"
//	unparseable   → kind "scalar" → body-parser 400 "{}"
func readBody(r *http.Request) (map[string]any, []string, string, bool) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return map[string]any{}, nil, "", true
	}
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, nil, "scalar", false
	}
	switch v := probe.(type) {
	case map[string]any:
		return v, topLevelKeys(raw), "", true
	case nil:
		return nil, nil, "null", false
	case []any:
		return nil, nil, "array", false
	default:
		return nil, nil, "scalar", false
	}
}

// topLevelKeys — top-level key order from the raw JSON text.
func topLevelKeys(b []byte) []string {
	var keys []string
	dec := json.NewDecoder(strings.NewReader(string(b)))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	for dec.More() {
		kt, _ := dec.Token()
		if ks, ok := kt.(string); ok {
			keys = append(keys, ks)
		}
		var v any
		_ = dec.Decode(&v)
	}
	return keys
}

// rejectBody — the two non-object-root 400 shapes (pinned):
// scalar/unparseable → 400 "{}"; null/array → zod "expected object" 400.
func rejectBody(res *core.Res, kind string) {
	if kind == "scalar" {
		res.BareWrite(400, []byte("{}"))
		return
	}
	res.JSON(400, va400(`Invalid input: expected object, received `+kind+` at \"body\"`))
}

// ---------- VA (hand-written zod messages, declaration order) ----------

func jstype(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// unknownBodyIssues — zod's unrecognized-keys issue (AFTER the per-field
// issues): 1 → `Unrecognized key: "k" at "body"`, n>1 →
// `Unrecognized keys: "a", "b" at "body"` (document order).
func unknownBodyIssues(order []string, declared ...string) []string {
	d := map[string]bool{}
	for _, k := range declared {
		d[k] = true
	}
	var unknown []string
	for _, k := range order {
		if !d[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	if len(unknown) == 1 {
		return []string{`Unrecognized key: \"` + unknown[0] + `\" at \"body\"`}
	}
	q := make([]string, len(unknown))
	for i, k := range unknown {
		q[i] = `\"` + k + `\"`
	}
	return []string{`Unrecognized keys: ` + strings.Join(q, ", ") + ` at \"body\"`}
}

// nameIssue — z.string().min(1) on missing/present value.
func nameIssue(v any, has bool) string {
	if !has {
		return `Invalid input: expected string, received undefined at \"body.name\"`
	}
	if s, ok := v.(string); !ok {
		return `Invalid input: expected string, received ` + jstype(v) + ` at \"body.name\"`
	} else if len(s) == 0 {
		return `Too small: expected string to have >=1 characters at \"body.name\"`
	}
	return ""
}

// createEditIssues — z.strictObject({name: z.string().min(1),
// color: z.string().regex(TAG_COLOR_REGEX).optional()}) →
// (issuesText, name, color, ok). Used by create and edit (identical schemas).
func createEditIssues(body map[string]any, order []string) (string, string, string, bool) {
	var issues []string
	var name, color string
	if n, has := body["name"]; !has {
		issues = append(issues, nameIssue(n, has))
	} else if s, ok := n.(string); !ok {
		issues = append(issues, `Invalid input: expected string, received `+jstype(n)+` at \"body.name\"`)
	} else if len(s) == 0 {
		issues = append(issues, `Too small: expected string to have >=1 characters at \"body.name\"`)
	} else {
		name = s
	}
	if v, has := body["color"]; has {
		if s, ok := v.(string); !ok {
			issues = append(issues, `Invalid input: expected string, received `+jstype(v)+` at \"body.color\"`)
		} else if !colorRE.MatchString(s) {
			issues = append(issues, colorIssueMsg)
		} else {
			color = s
		}
	}
	issues = append(issues, unknownBodyIssues(order, "name", "color")...)
	if len(issues) > 0 {
		return strings.Join(issues, "; "), name, color, false
	}
	return "", name, color, true
}

// renameIssues — z.strictObject({name: z.string().min(1)}).
func renameIssues(body map[string]any, order []string) (string, string, bool) {
	var issues []string
	var name string
	if v, has := body["name"]; !has {
		issues = append(issues, nameIssue(v, has))
	} else if s, ok := v.(string); !ok {
		issues = append(issues, `Invalid input: expected string, received `+jstype(v)+` at \"body.name\"`)
	} else if len(s) == 0 {
		issues = append(issues, `Too small: expected string to have >=1 characters at \"body.name\"`)
	} else {
		name = s
	}
	issues = append(issues, unknownBodyIssues(order, "name")...)
	if len(issues) > 0 {
		return strings.Join(issues, "; "), name, false
	}
	return "", name, true
}

// projectIdsIssues — z.strictObject({projectIds: z.array(zz.objectId())}) →
// (issuesText, ids, ok). Element issues keep the array order.
func projectIdsIssues(body map[string]any, order []string) (string, []string, bool) {
	var issues []string
	ids := []string{}
	if v, has := body["projectIds"]; !has {
		issues = append(issues, `Invalid input: expected array, received undefined at \"body.projectIds\"`)
	} else if arr, ok := v.([]any); !ok {
		issues = append(issues, `Invalid input: expected array, received `+jstype(v)+` at \"body.projectIds\"`)
	} else {
		for i, e := range arr {
			s, ok := e.(string)
			if !ok || !validOID.MatchString(s) {
				issues = append(issues, `Invalid Mongo ObjectId at \"body.projectIds[`+strconv.Itoa(i)+`]\"`)
				continue
			}
			ids = append(ids, strings.ToLower(s))
		}
	}
	issues = append(issues, unknownBodyIssues(order, "projectIds")...)
	if len(issues) > 0 {
		return strings.Join(issues, "; "), ids, false
	}
	return "", ids, true
}

// ---------- tag doc JSON (BSON order → JSON) ----------

func jstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// writeJSONVal — one BSON value as JSON (strings quoted, ObjectIDs as
// 24-hex strings, int32 as plain numbers, arrays/objects keep order).
func writeJSONVal(sb *strings.Builder, v any) {
	switch x := v.(type) {
	case string:
		sb.WriteString(jstr(x))
	case primitive.ObjectID:
		sb.WriteString(`"` + x.Hex() + `"`)
	case int32:
		sb.WriteString(strconv.Itoa(int(x)))
	case int64:
		sb.WriteString(strconv.Itoa(int(x)))
	case bool:
		if x {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case primitive.A:
		sb.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJSONVal(sb, e)
		}
		sb.WriteByte(']')
	case primitive.D:
		sb.WriteByte('{')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(`"` + e.Key + `":`)
			writeJSONVal(sb, e.Value)
		}
		sb.WriteByte('}')
	case nil:
		sb.WriteString("null")
	default:
		b, _ := json.Marshal(v)
		sb.Write(b)
	}
}

// dJSONTag — one tag doc (primitive.D, stored BSON order) → JSON keeping the
// key order: {_id, user_id, name, color?, project_ids, __v, ...}.
func dJSONTag(d primitive.D) []byte {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, e := range d {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`"` + e.Key + `":`)
		writeJSONVal(&sb, e.Value)
	}
	sb.WriteByte('}')
	return []byte(sb.String())
}

// ---------- mongo plumbing ----------

func mongoRun(a *core.App, cxt *core.Cxt, fn func(db *mongo.Database, ctx context.Context) error) error {
	if a.Mongo == nil {
		return errNoMongo
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	return fn(db, ctx)
}

type mongoErr string

func (e mongoErr) Error() string { return string(e) }

var errNoMongo = mongoErr("mongo not available")

// ---------- handlers ----------

func getAllTags(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := userGate(cxt, res)
		if !ok {
			return
		}
		var docs []primitive.D
		err := mongoRun(a, cxt, func(db *mongo.Database, ctx context.Context) error {
			cur, e := db.Collection("tags").Find(ctx, bson.D{{Key: "user_id", Value: uid}})
			if e != nil {
				return e
			}
			defer cur.Close(ctx)
			for cur.Next(ctx) {
				var d primitive.D
				if decErr := cur.Decode(&d); decErr != nil {
					continue
				}
				docs = append(docs, d)
			}
			return cur.Err()
		})
		if err != nil {
			serve500Page(cxt, res)
			return
		}
		out := make([]string, 0, len(docs))
		for _, d := range docs {
			out = append(out, string(dJSONTag(d)))
		}
		res.JSON(200, []byte("["+strings.Join(out, ",")+"]"))
	}
}

func createTagHandler(a *core.App) func(*core.Cxt, *core.Res) {
	lim := limOf(a, "create-tag", 30, 60)
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := userGate(cxt, res)
		if !ok {
			return
		}
		if !lim.Consume(uid) {
			core.Send429(res, rateLimitMsg)
			return
		}
		body, order, kind, ok := readBody(cxt.Req)
		if !ok {
			rejectBody(res, kind)
			return
		}
		name, color := "", ""
		if msg, n, c, ok := createEditIssues(body, order); !ok {
			res.JSON(400, va400(msg))
			return
		} else {
			name, color = n, c
		}
		if len(name) > maxTagLength {
			serve500Page(cxt, res) // Node: throw new Error('Exceeded max tag length')
			return
		}
		var (
			newID primitive.ObjectID
			dup   *primitive.D
		)
		err := mongoRun(a, cxt, func(db *mongo.Database, ctx context.Context) error {
			newID = primitive.NewObjectID()
			ins := bson.D{
				{Key: "_id", Value: newID},
				{Key: "user_id", Value: uid},
				{Key: "name", Value: name},
			}
			if color != "" {
				ins = append(ins, bson.E{Key: "color", Value: color})
			}
			ins = append(ins,
				bson.E{Key: "project_ids", Value: primitive.A{}},
				bson.E{Key: "__v", Value: int32(0)})
			if _, e := db.Collection("tags").InsertOne(ctx, ins); e != nil {
				if mongo.IsDuplicateKeyError(e) {
					// Node: duplicate key → return the EXISTING tag
					f := db.Collection("tags").FindOne(ctx,
						bson.D{{Key: "user_id", Value: uid}, {Key: "name", Value: name}})
					var d primitive.D
					if decErr := f.Decode(&d); decErr != nil {
						return decErr
					}
					dup = &d
					return nil
				}
				return e
			}
			return nil
		})
		if err != nil {
			serve500Page(cxt, res)
			return
		}
		if dup != nil {
			res.JSON(200, dJSONTag(*dup)) // stored order (_id first)
			return
		}
		// new-create wire order (pinned live): user_id, name, color?, project_ids, _id, __v
		var sb strings.Builder
		sb.WriteString(`{"user_id":` + jstr(uid) + `,"name":` + jstr(name))
		if color != "" {
			sb.WriteString(`,"color":` + jstr(color))
		}
		sb.WriteString(`,"project_ids":[],"_id":"` + newID.Hex() + `","__v":0}`)
		res.JSON(200, []byte(sb.String()))
	}
}

func renameTagHandler(a *core.App) func(*core.Cxt, *core.Res) {
	lim := limOf(a, "rename-tag", 30, 60)
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := userGate(cxt, res)
		if !ok {
			return
		}
		if !lim.Consume(uid) {
			core.Send429(res, rateLimitMsg)
			return
		}
		tid, ok := oidParam(cxt, res, "1", "tagId")
		if !ok {
			return
		}
		body, order, kind, ok := readBody(cxt.Req)
		if !ok {
			rejectBody(res, kind)
			return
		}
		var name string
		if msg, n, ok := renameIssues(body, order); !ok {
			res.JSON(400, va400(msg))
			return
		} else {
			name = n
		}
		if len(name) > maxTagLength {
			serve500Page(cxt, res)
			return
		}
		err := mongoRun(a, cxt, func(db *mongo.Database, ctx context.Context) error {
			// Node: updateOne — no match (foreign/absent tag) is a silent no-op
			_, e := db.Collection("tags").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: tid}, {Key: "user_id", Value: uid}},
				bson.D{{Key: "$set", Value: bson.D{{Key: "name", Value: name}}}})
			return e
		})
		if err != nil {
			serve500Page(cxt, res)
			return
		}
		res.NoContent()
	}
}

func editTagHandler(a *core.App) func(*core.Cxt, *core.Res) {
	lim := limOf(a, "rename-tag", 30, 60) // Node wires edit → renameTag's limiter
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := userGate(cxt, res)
		if !ok {
			return
		}
		if !lim.Consume(uid) {
			core.Send429(res, rateLimitMsg)
			return
		}
		tid, ok := oidParam(cxt, res, "1", "tagId")
		if !ok {
			return
		}
		body, order, kind, ok := readBody(cxt.Req)
		if !ok {
			rejectBody(res, kind)
			return
		}
		name, color := "", ""
		if msg, n, c, ok := createEditIssues(body, order); !ok {
			res.JSON(400, va400(msg))
			return
		} else {
			name, color = n, c
		}
		if len(name) > maxTagLength {
			serve500Page(cxt, res)
			return
		}
		err := mongoRun(a, cxt, func(db *mongo.Database, ctx context.Context) error {
			set := bson.D{{Key: "name", Value: name}}
			// Node: `.$set: {name, color}` with color===undefined → mongoose
			// strips it, so a color-less edit only changes the name.
			if color != "" {
				set = append(set, bson.E{Key: "color", Value: color})
			}
			_, e := db.Collection("tags").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: tid}, {Key: "user_id", Value: uid}},
				bson.D{{Key: "$set", Value: set}})
			return e
		})
		if err != nil {
			serve500Page(cxt, res)
			return
		}
		res.NoContent()
	}
}

func deleteTagHandler(a *core.App) func(*core.Cxt, *core.Res) {
	lim := limOf(a, "delete-tag", 30, 60)
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := userGate(cxt, res)
		if !ok {
			return
		}
		if !lim.Consume(uid) {
			core.Send429(res, rateLimitMsg)
			return
		}
		tid, ok := oidParam(cxt, res, "1", "tagId")
		if !ok {
			return
		}
		err := mongoRun(a, cxt, func(db *mongo.Database, ctx context.Context) error {
			// Node: deleteOne — no match is a silent no-op
			_, e := db.Collection("tags").DeleteOne(ctx,
				bson.D{{Key: "_id", Value: tid}, {Key: "user_id", Value: uid}})
			return e
		})
		if err != nil {
			serve500Page(cxt, res)
			return
		}
		res.NoContent()
	}
}

// memberOp — POST /tag/:tagId/project/:projectId (add) and its DELETE
// (remove). Node: findOneAndUpdate $addToSet / updateOne $pull, silent
// no-op when the tag is not the caller's.
func memberOpHandler(a *core.App, verb string) func(*core.Cxt, *core.Res) {
	limName := "add-project-to-tag"
	if verb == "remove" {
		limName = "remove-project-from-tag"
	}
	lim := limOf(a, limName, 30, 60)
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := userGate(cxt, res)
		if !ok {
			return
		}
		if !lim.Consume(uid) {
			core.Send429(res, rateLimitMsg)
			return
		}
		tid, ok := oidParam(cxt, res, "1", "tagId")
		if !ok {
			return
		}
		pid, ok := hexParam(cxt, res, "2", "projectId")
		if !ok {
			return
		}
		err := mongoRun(a, cxt, func(db *mongo.Database, ctx context.Context) error {
			var op bson.D
			if verb == "add" {
				op = bson.D{{Key: "$addToSet", Value: bson.D{{Key: "project_ids", Value: pid}}}}
			} else {
				op = bson.D{{Key: "$pull", Value: bson.D{{Key: "project_ids", Value: pid}}}}
			}
			_, e := db.Collection("tags").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: tid}, {Key: "user_id", Value: uid}}, op)
			return e
		})
		if err != nil {
			serve500Page(cxt, res)
			return
		}
		res.NoContent()
	}
}

func addProjectsHandler(a *core.App) func(*core.Cxt, *core.Res) {
	lim := limOf(a, "add-projects-to-tag", 30, 60)
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := userGate(cxt, res)
		if !ok {
			return
		}
		if !lim.Consume(uid) {
			core.Send429(res, rateLimitMsg)
			return
		}
		tid, ok := oidParam(cxt, res, "1", "tagId")
		if !ok {
			return
		}
		body, order, kind, ok := readBody(cxt.Req)
		if !ok {
			rejectBody(res, kind)
			return
		}
		var ids []string
		if msg, i, ok := projectIdsIssues(body, order); !ok {
			res.JSON(400, va400(msg))
			return
		} else {
			ids = i
		}
		err := mongoRun(a, cxt, func(db *mongo.Database, ctx context.Context) error {
			// Node: $addToSet {project_ids: {$each: ids}}
			_, e := db.Collection("tags").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: tid}, {Key: "user_id", Value: uid}},
				bson.D{{Key: "$addToSet", Value: bson.D{
					{Key: "project_ids", Value: bson.D{{Key: "$each", Value: ids}}},
				}}})
			return e
		})
		if err != nil {
			serve500Page(cxt, res)
			return
		}
		res.NoContent()
	}
}

func removeProjectsHandler(a *core.App) func(*core.Cxt, *core.Res) {
	lim := limOf(a, "remove-projects-from-tag", 30, 60)
	return func(cxt *core.Cxt, res *core.Res) {
		uid, ok := userGate(cxt, res)
		if !ok {
			return
		}
		if !lim.Consume(uid) {
			core.Send429(res, rateLimitMsg)
			return
		}
		tid, ok := oidParam(cxt, res, "1", "tagId")
		if !ok {
			return
		}
		body, order, kind, ok := readBody(cxt.Req)
		if !ok {
			rejectBody(res, kind)
			return
		}
		var ids []string
		if msg, i, ok := projectIdsIssues(body, order); !ok {
			res.JSON(400, va400(msg))
			return
		} else {
			ids = i
		}
		err := mongoRun(a, cxt, func(db *mongo.Database, ctx context.Context) error {
			// Node: $pullAll {project_ids: ids}
			_, e := db.Collection("tags").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: tid}, {Key: "user_id", Value: uid}},
				bson.D{{Key: "$pullAll", Value: bson.D{{Key: "project_ids", Value: ids}}}})
			return e
		})
		if err != nil {
			serve500Page(cxt, res)
			return
		}
		res.NoContent()
	}
}

// ---------- views helpers ----------

// pageBase — views.PageData for the error/404 pages (nonce + origin +
// session slots + the request path).
func pageBase(cxt *core.Cxt, reqPath string) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Path: reqPath}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
		// Node ExpressLocals render these on EVERY page (incl. error pages):
		// the template-menu slot runs the full per-user ladder (session or DB
		// admin), the admin nav fragment for session admins.
		d.CanManageTemplateMenu = templates.MenuGrant(cxt.Req.Context(), cxt)
		if templates.SessionIsAdmin(cxt.Sess) {
			d.NavAdmin = views.AdminNavFragment
		}
	}
	return d
}

func serve500Page(cxt *core.Cxt, res *core.Res) {
	views.Error500Page(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
}

// limOf — lazy limiter construction (Feature(nil) in unit tests must not
// dereference a nil *core.App; Consume is nil-safe and fails open).
func limOf(a *core.App, name string, points, sec int) *core.RateLimiter {
	if a == nil {
		return nil
	}
	return core.NewRateLimiter(a.Redis, name, points, sec)
}
