// U10.1 — project history route family (Node 1:1 port).
//
// Node sources (services/web/app/src/Features/History/):
//
//	HistoryRouter.mjs      (route list, middleware order, rate limiters)
//	HttpsController.mjs -> HistoryController.mjs (zod schemas, 4xx mapping,
//	                       zip/blob headers, labels enrichment,
//	                       user-detail injection)
//	HistoryManager.mjs     (V1/V2 URLs, basic auth, blob streaming,
//	                       _userView, flush)
//
// Wire contracts (pinned by tests/e2e/specs/parity/web-go-u101-history.
// test.e2e.ts against the LIVE Node oracle, 2026-09-22):
//
//   - Anonymous on every route: global gate -> 401 text/plain
//     "Unauthorized" (JSON accept) / 302 /login (HTML).
//   - Non-member (JSON accept): 403 {"message":"restricted"}.
//   - Invalid :Project_id: 404 JSON
//     {"error":"Validation error: Invalid Mongo ObjectId at
//     \"params.Project_id\"","statusCode":404}
//   - updates: member 200 {"updates":[...]} — V2 passthrough with
//     meta.users injected to _userView objects {first_name,last_name,
//     email,id}; unresolved user id -> null.
//   - latest/history (V1) & changes (V1): member 200 JSON passthrough;
//     project without overleaf.history.id -> Node 500 page for V1 routes
//     (zip -> 402 plain "Payment Required").
//   - changes?paginated=true -> {"changes":[...],"hasMore":false};
//     plain -> bare array.
//   - diff / doc/:doc_id/diff / filetree/diff: V2 passthrough (non-2xx ->
//     Node 500 page; in this stack V2 diffs 500 -> 681B HTML error page).
//   - labels: GET -> [labels + user_display_name appended]; POST
//     {comment,version} -> 200 created label JSON; DELETE -> 204;
//     400 body validation (body.comment / body.version).
//   - version/:version/zip: 200 application/zip + attachment
//     "<name> (Version N).zip" + X-Content-Type-Options nosniff +
//     X-Accel-Buffering no; upstream 404 -> 404 plain "Not Found".
//   - blob/:hash: 200 octet-stream + ETag <hash raw> +
//     Cache-Control private max-age=86400 stale-while-revalidate=31536000
//   - X-Accel-Buffering no; HEAD same headers no body; If-None-Match ->
//     304; Range -> 206 + Content-Range; upstream 404 -> 404 empty no CT;
//     bad hash -> 404 Validation error JSON.
//   - flush: POST -> V2 passthrough (200 empty body this stack).
//   - restore_file / revert_file / revert-project: ensureUserCanWrit
//     eProjectContent + V2 proxy; missing version -> 400 body validation.
package history

import (
	"os"
	"regexp"

	"ollitex/go/services/web/core"
)

// ---------- config (server-ce/config/settings.js) ----------

// v2Base: apis.project_history.url 'http://127.0.0.1:3054' (server-ce).
func v2Base() string {
	if u := trimRightSlash(os.Getenv("PROJECT_HISTORY_URL")); u != "" {
		return u
	}
	host := os.Getenv("PROJECT_HISTORY_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	return "http://" + host + ":3054"
}

// v1Base: apis.v1_history.url 'http://127.0.0.1:3100/api' (server-ce).
func v1Base() string {
	if u := trimRightSlash(os.Getenv("V1_HISTORY_URL")); u != "" {
		return u
	}
	return "http://127.0.0.1:3100/api"
}

// server-ce pins user 'staging'; pass from env STAGING_PASSWORD.
func v1User() string {
	if u := os.Getenv("V1_HISTORY_USER"); u != "" {
		return u
	}
	return "staging"
}

func v1Pass() string {
	if p := os.Getenv("STAGING_PASSWORD"); p != "" {
		return p
	}
	return os.Getenv("V1_HISTORY_PASSWORD")
}

func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// ---------- routes ----------

// Case-insensitive project-history paths (Node Express is
// case-insensitive and non-strict about trailing slashes).
var (
	pj    = "(?i:project)"
	hex24 = `[0-9a-fA-F]{24}`
)

var (
	// Node Express params are [^/]+ — validation (zz.objectId / zz.hex().
	// length(40) / z.coerce.number) happens in the controller and pins the
	// 404 JSON bodies; the patterns must match the Express segment.
	seg           = `[0-9a-zA-Z._~-]+` // conservative non-slash word (Node: [^/]+)
	updatesPat    = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:updates)/?$`)
	diffPat       = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:diff)/?$`)
	docDiffPat    = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:doc)/(?P<2>` + seg + `)/(?i:diff)/?$`)
	filetreePat   = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:filetree)/(?i:diff)/?$`)
	latestPat     = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:latest)/(?i:history)/?$`)
	changesPat    = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:changes)/?$`)
	labelsPat     = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:labels)/?$`)
	labelDelPat   = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:labels)/(?P<2>` + seg + `)/?$`)
	zipPat        = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:version)/(?P<2>` + seg + `)/(?i:zip)/?$`)
	blobPat       = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:blob)/(?P<2>` + seg + `)/?$`)
	flushPat      = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:flush)/?$`)
	restorePat    = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:restore_file)/?$`)
	revertFilePat = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:revert_file)/?$`)
	revertProjPat = regexp.MustCompile(`^/` + pj + `/(?P<1>` + seg + `)/(?i:revert-project)/?$`)
)

func Feature(a *core.App) core.Feature {
	zipLim := core.NewRateLimiter(a.Redis, "download-project-revision", 30, 60*60)
	blobLim := core.NewRateLimiter(a.Redis, "get-project-blob", 2000, 60*60)
	flushLim := core.NewRateLimiter(a.Redis, "flush-project-history", 30, 60)
	h := &svc{a: a}
	return core.Feature{Name: "history", Routes: []core.Route{
		{Method: "GET", Pattern: updatesPat, Handler: h.updates},
		{Method: "GET", Pattern: diffPat, Handler: h.proxy},
		{Method: "GET", Pattern: docDiffPat, Handler: h.docDiff},
		{Method: "GET", Pattern: filetreePat, Handler: h.proxy},
		{Method: "GET", Pattern: latestPat, Handler: h.latestHistory},
		{Method: "GET", Pattern: changesPat, Handler: h.changes},
		{Method: "GET", Pattern: labelsPat, Handler: h.labels},
		{Method: "POST", Pattern: labelsPat, Handler: h.createLabel},
		{Method: "DELETE", Pattern: labelDelPat, Handler: h.deleteLabel},
		{Method: "GET", Pattern: zipPat, Handler: limit(zipLim, h.versionZip)},
		{Method: "GET", Pattern: blobPat, Handler: limit(blobLim, h.blob)},
		{Method: "HEAD", Pattern: blobPat, Handler: limit(blobLim, h.blob)},
		{Method: "POST", Pattern: flushPat, Handler: limit(flushLim, h.flush)},
		{Method: "POST", Pattern: restorePat, Handler: h.restoreFile},
		{Method: "POST", Pattern: revertFilePat, Handler: h.revertFile},
		{Method: "POST", Pattern: revertProjPat, Handler: h.revertProject},
	}}
}

type svc struct{ a *core.App }

// limiterID mirrors RateLimiterMiddleware: clientID = userId || req.ip.
func limiterID(cxt *core.Cxt) string {
	if cxt.Sess != nil {
		if _, uid := core.PassportUser(cxt.Sess); uid != "" {
			return uid
		}
	}
	return core.ClientIP(cxt.Req)
}

func limit(l *core.RateLimiter, fn func(*core.Cxt, *core.Res)) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if l.Consume(limiterID(cxt)) {
			fn(cxt, res)
			return
		}
		core.Send429(res, "Rate limit reached, please try again later")
	}
}
