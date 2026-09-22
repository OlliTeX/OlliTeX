// Package analytics — port of services/web/app/src/Features/Analytics
// (the web surface; the v1-bridge analytics manager is OUT of this stack —
// `analytics` feature = Boolean(Settings.apis.v1.url), and apis.v1.url is
// UNSET here, so every controller short-circuits to res.sendStatus(202).
//
// Routers (AnalyticsRouter.mjs, applied to webRouter — session+csrf chain):
//
//	POST   /event/:event([a-z0-9-_]+)
//	     → rate-limit analytics-record-event (200/60)   [no params]
//	     → hasFeature('analytics') false → 202 "Accepted"
//	PUT    /editingSession/:projectId
//	     → rate-limit analytics-update-editing-session (20/60) [params projectId]
//	     → hasFeature('analytics') false → 202 "Accepted"
//
// Wire pins (live-oracle /tmp/u102-wire-node.cjs + u102-edges-node.cjs):
//   - 202 text/plain; charset=utf-8, Content-Length 8, body "Accepted",
//     ETag W/"8-YaBXLEiT7zQxEyDYTILfiL6oPhE" (= sha1 of "Accepted"),
//     session-cookie refresh headers as usual.
//   - anonymous stateful call → the GLOBAL csrf 403 (core, before routing);
//     anonymous GET on these paths → the global login wall (302/401).
//   - two-segment paths (/event/a/b) and wrong methods → the app 404
//     ("Cannot POST ..." page / 404 view) — no route registered.
//   - the route param is a SINGLE segment of any content (observed:
//     uppercase, dots, encoded chars all reach the 202 — the regex
//     ([a-z0-9-_]+) does not restrict the wire in practice; we accept
//     any one segment, pinned by the gate's edge cases).
//
// The ENABLED branch (apis.v1.url set — NOT in this stack) would POST the
// event to the v1 bridge; that path is unreachable here and deliberately
// not faked: if the flag is ever set the 202 short-circuit disappears and
// the branch would need the v1 client. The flag is read from config so
// the unit stays honest.
package analytics

import (
	"os"

	"ollitex/go/services/web/core"
	"regexp"
)

var (
	// eventPat — Node route '/event/:event([a-z0-9-_]+)'. Express route
	// matching is case-insensitive (pinned live: POST /event/ABC -> 202,
	// /event/abc! -> 404 Cannot, /event/a+b -> 404), so the class is the
	// full a-zA-Z0-9-_.
	eventPat = regexp.MustCompile(`^/event/([A-Za-z0-9_-]+)$`)
	esessPat = regexp.MustCompile(`^/editingSession/([^/]+)$`)
)

type svc struct {
	rec   *core.RateLimiter
	esess *core.RateLimiter
	// Features.hasFeature('analytics') = Boolean(Settings.apis.v1.url);
	// apis.v1 is unset in this stack → the enabled branch is unreachable
	// (honest: if it were ever set the v1 client would be needed here).
	analytics bool
}

// Feature registers the analytics stub surface.
func Feature(a *core.App) core.Feature {
	_ = a
	s := &svc{
		rec:       core.NewRateLimiter(a.Redis, "analytics-record-event", 200, 60),
		esess:     core.NewRateLimiter(a.Redis, "analytics-update-editing-session", 20, 60),
		analytics: os.Getenv("APIS_V1_URL") != "",
	}
	return core.Feature{
		Name: "analytics",
		Routes: []core.Route{
			{Method: "POST", Pattern: eventPat, Handler: s.recordEvent},
			{Method: "PUT", Pattern: esessPat, Handler: s.updateEditingSession},
		},
	}
}

// handler202 — Node: features off → res.sendStatus(202) BEFORE any
// validation (the controller checks hasFeature first), so body shape is
// irrelevant for the wire.
func (s *svc) recordEvent(cxt *core.Cxt, res *core.Res) {
	if s.rec.Consume(limiterID(cxt)) {
		respond202(res)
		return
	}
	core.Send429(res, "Rate limit reached, please try again later")
}

func (s *svc) updateEditingSession(cxt *core.Cxt, res *core.Res) {
	// Node key: params ['projectId'] + clientId joined by ':' (the router
	// middleware builds `key = params.concat(clientId).join(':')`). Go
	// dispatch names unnamed capture groups by 1-based index.
	pid := cxt.Params["1"]
	if s.esess.Consume(pid + ":" + limiterID(cxt)) {
		respond202(res)
		return
	}
	core.Send429(res, "Rate limit reached, please try again later")
}

// respond202 — res.sendStatus(202): body "Accepted" (8 bytes), text/plain,
// ETag over the status text (pinned live).
func respond202(res *core.Res) {
	res.SendStatus(202)
}

// limiterID mirrors Node RateLimiterMiddleware: clientId = userId || req.ip.
func limiterID(cxt *core.Cxt) string {
	if cxt.Sess != nil {
		if _, uid := core.PassportUser(cxt.Sess); uid != "" {
			return uid
		}
	}
	return core.ClientIP(cxt.Req)
}
