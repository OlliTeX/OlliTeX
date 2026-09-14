// Package userpages ports the Node user settings + sessions family (P3.3
// flip unit):
//
//	GET    /user/settings          (login; React view)
//	POST   /user/settings          (login+csrf; zod-strict update)
//	GET    /user/sessions          (login; website-redesign view)
//	GET    /user/sessions/list     (login; JSON twin)
//	POST   /user/sessions/clear    (login+csrf; 201 + audit + mail + purge)
//
// Node ground truth: services/web/app/src/Features/User/{UserPagesController,
// UserController, UserSessionsManager, UserUpdater, UserGetter}.mjs +
// views/user/{settings,sessions}.pug. Contracts pinned live 2026-09-14
// (p33 pin report); the flip gate spec is the authority.
//
// Deliberately deferred in this leaf (WEB_GO_PLAN.md P3.3):
//   - the email-CHANGE success path (add/setDefault/remove emails + mails);
//     the 409 conflict / invalid / own-no-op branches ARE implemented;
//   - the general/400 HTML render for a 409 with Accept: text/html.

package userpages

import (
	"encoding/json"
	"ollitex/go/services/web/core"
	"os"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------- routes ----------

func Feature(a *core.App) core.Feature {
	mail := core.NewMail()
	return core.Feature{
		Name: "userpages",
		Routes: []core.Route{
			{Method: "GET", Path: "/user/settings", Handler: getSettings(a)},
			{Method: "POST", Path: "/user/settings", Handler: postSettings(a)},
			{Method: "GET", Path: "/user/sessions", Handler: getSessionsPage(a)},
			{Method: "GET", Path: "/user/sessions/list", Handler: getSessionList(a)},
			{Method: "POST", Path: "/user/sessions/clear", Handler: postClear(a, mail)},
		},
	}
}

// ---------- shared small helpers ----------

// ErrSessions500 — the Node getAllUserSessions JSON.parse fault contract
// (corrupt session doc → 500 view for ALL sessions endpoints). The
// handlers call a.Render500 directly (same shape as serveradmin).
// (Kept as a sentinel for tests.)

func render500(a *core.App, cxt *core.Cxt, res *core.Res) {
	if a.Render500 != nil {
		a.Render500(cxt, res)
		return
	}
	res.SendStatus(500)
}

func mustObjectID(hex string) primitive.ObjectID {
	oid, _ := primitive.ObjectIDFromHex(hex)
	return oid
}

// popString — Node popSessionValue: read a truthy string, delete it.

// popString — Node popSessionValue: read a truthy string, delete it.
func popString(s *core.Session, key string) (string, bool) {
	raw, ok := s.GetRaw(key)
	if !ok {
		return "", false
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	if v == "" {
		return "", false
	}
	s.Del(key)
	return v, true
}

func siteURL() string {
	if v := strings.TrimSpace(os.Getenv("OVERLEAF_SITE_URL")); v != "" {
		return v
	}
	return "http://127.0.0.1:7420"
}

// ---------- POST /user/settings (zod-strict mirror) ----------
