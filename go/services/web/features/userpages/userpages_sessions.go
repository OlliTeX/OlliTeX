package userpages

import (
	"encoding/json"
	"fmt"
	"log"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// ---------- shared small helpers ----------

// ErrSessions500 — the Node getAllUserSessions JSON.parse fault contract
// (corrupt session doc → 500 view for ALL sessions endpoints). The
// handlers call a.Render500 directly (same shape as serveradmin).
// (Kept as a sentinel for tests.)
var ErrSessions500 = fmt.Errorf("userpages: sessions store corrupt (node parity 500)")

// ---------- sessions helpers (UserSessionsManager parity) ----------

type sessionEntry struct {
	IPAddress      string `json:"ip_address"`
	SessionCreated string `json:"session_created"`
}

// allUserSessions — Node getAllUserSessions: smembers, exclude current,
// GET each, missing skipped, JSON.parse (corrupt → ok=false → 500 view),
// passport.user | legacy .user → {ip_address, session_created}.

// allUserSessions — Node getAllUserSessions: smembers, exclude current,
// GET each, missing skipped, JSON.parse (corrupt → ok=false → 500 view),
// passport.user | legacy .user → {ip_address, session_created}.
func allUserSessions(rdb *core.RedisClient, uid, curSid string) ([]sessionEntry, bool) {
	keys, err := rdb.SMEMBERS(core.UserSessionsKey(uid))
	if err != nil {
		return nil, false
	}
	excl := core.SessionDocKey(curSid)
	var out []sessionEntry
	for _, k := range keys {
		if k == excl {
			continue
		}
		raw, found, err := rdb.GET(k)
		if err != nil || !found {
			continue // node: `if (!session) continue`
		}
		if !json.Valid([]byte(raw)) {
			return nil, false // node JSON.parse throws → 500 (pinned)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return nil, false
		}
		pu := nestedUser(doc, "passport", "user")
		if pu == nil {
			continue
		}
		e := sessionEntry{}
		e.IPAddress, _ = pu["ip_address"].(string)
		e.SessionCreated, _ = pu["session_created"].(string)
		out = append(out, e)
	}
	if out == nil {
		out = []sessionEntry{}
	}
	return out, true
}

// purgeSessions — Node removeSessionsFromRedis(user, retain): DEL each
// referenced doc, SREM the set, PEXPIRE refresh.

// purgeSessions — Node removeSessionsFromRedis(user, retain): DEL each
// referenced doc, SREM the set, PEXPIRE refresh.
func purgeSessions(rdb *core.RedisClient, uid, retain string) int {
	keys, err := rdb.SMEMBERS(core.UserSessionsKey(uid))
	if err != nil || len(keys) == 0 {
		return 0
	}
	keep := core.SessionDocKey(retain)
	var del []string
	for _, k := range keys {
		if k != keep {
			del = append(del, k)
		}
	}
	if len(del) == 0 {
		return 0
	}
	for _, k := range del {
		_ = rdb.DEL(k)
	}
	_ = rdb.SREM(core.UserSessionsKey(uid), del...)
	_ = rdb.PEXPIRE(core.UserSessionsKey(uid), core.CookieSessionLengthMs())
	return len(del)
}

// clearAllUserSessions — Node removeSessionsFromRedis(user, null) edge
// (settingsPage: user just deleted → kill ALL tracked sessions).

// clearAllUserSessions — Node removeSessionsFromRedis(user, null) edge
// (settingsPage: user just deleted → kill ALL tracked sessions).
func clearAllUserSessions(a *core.App, uid string) {
	if uid == "" {
		return
	}
	keys, err := a.Redis.SMEMBERS(core.UserSessionsKey(uid))
	if err != nil || len(keys) == 0 {
		return
	}
	for _, k := range keys {
		_ = a.Redis.DEL(k)
	}
	_ = a.Redis.SREM(core.UserSessionsKey(uid), keys...)
}

func currentUserEntry(s *core.Session) sessionEntry {
	e := sessionEntry{}
	if s == nil {
		return e
	}
	raw, ok := s.GetRaw("passport")
	if !ok {
		raw, ok = s.GetRaw("user")
		if !ok {
			return e
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			e.IPAddress, _ = m["ip_address"].(string)
			e.SessionCreated, _ = m["session_created"].(string)
			return e
		}
	}
	var wrap struct {
		User map[string]any `json:"user"`
	}
	if json.Unmarshal(raw, &wrap) == nil && wrap.User != nil {
		e.IPAddress, _ = wrap.User["ip_address"].(string)
		e.SessionCreated, _ = wrap.User["session_created"].(string)
	}
	return e
}

// ---------- GET /user/sessions ----------

// ---------- GET /user/sessions ----------

func getSessionsPage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := cxt.Sess.UserIDHex()
		doc, ok := userDoc(a, ctx, uid)
		if !ok {
			clearAllUserSessions(a, uid)
			res.Redirect(cxt.Req, 302, "/")
			return
		}
		email, _ := doc["email"].(string)
		others, ok2 := allUserSessions(a.Redis, uid, cxt.Sess.SessID)
		if !ok2 {
			render500(a, cxt, res)
			return
		}
		cur := currentUserEntry(cxt.Sess)
		d := pageBase(cxt, email, mustObjectID(uid).Hex())
		d.SessionsCurrentRow = sessionRow(cur.IPAddress, cur.SessionCreated)
		d.SessionsOtherRows = otherRows(others)
		views.SessionsPage(res.W, d)
	}
}

func getSessionList(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := cxt.Sess.UserIDHex()
		if _, ok := userDoc(a, ctx, uid); !ok {
			clearAllUserSessions(a, uid)
			res.Redirect(cxt.Req, 302, "/")
			return
		}
		others, ok := allUserSessions(a.Redis, uid, cxt.Sess.SessID)
		if !ok {
			render500(a, cxt, res)
			return
		}
		cur := currentUserEntry(cxt.Sess)
		body, _ := json.Marshal(map[string]any{
			"currentSession": map[string]string{
				"ip_address":      cur.IPAddress,
				"session_created": cur.SessionCreated,
			},
			"sessions": others,
		})
		res.JSON(200, body)
	}
}

// ---------- POST /user/sessions/clear ----------

// ---------- POST /user/sessions/clear ----------

func postClear(a *core.App, mail *core.Mail) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		uid := cxt.Sess.UserIDHex()
		doc, ok := userDoc(a, ctx, uid)
		if !ok {
			res.SendStatus(401)
			return
		}
		email, _ := doc["email"].(string)

		// 1) getAllUserSessions (Node order) — corrupt → 500 BEFORE audit.
		others, ok2 := allUserSessions(a.Redis, uid, cxt.Sess.SessID)
		if !ok2 {
			render500(a, cxt, res)
			return
		}

		// 2) audit entry (pinned doc shape, insertion order).
		sessDocs := make([]bson.D, 0, len(others))
		for _, e := range others {
			sessDocs = append(sessDocs, bson.D{
				{Key: "ip_address", Value: e.IPAddress},
				{Key: "session_created", Value: e.SessionCreated},
			})
		}
		entry := bson.D{
			{Key: "userId", Value: mustObjectID(uid)},
			{Key: "info", Value: bson.D{{Key: "sessions", Value: sessDocs}}},
			{Key: "initiatorId", Value: mustObjectID(uid)},
			{Key: "ipAddress", Value: core.ClientIP(cxt.Req)},
			{Key: "operation", Value: "clear-sessions"},
			{Key: "timestamp", Value: time.Now().UTC()},
			{Key: "__v", Value: 0},
		}
		db, dbErr := a.Mongo.DB(ctx)
		if db != nil {
			if _, insErr := db.Collection("userAuditLogEntries").InsertOne(ctx, entry); insErr != nil {
				log.Printf("webgo: audit insert failed: %v", insErr)
			}
		} else if dbErr != nil {
			log.Printf("webgo: audit db unavailable: %v", dbErr)
		}

		// 3) purge other sessions (keep current).
		purgeSessions(a.Redis, uid, cxt.Sess.SessID)

		// 4) security alert mail (Node: send, log on error, never 500).
		now := time.Now().UTC()
		subject := "Overleaf security note: active sessions cleared"
		desc := fmt.Sprintf("active sessions were cleared on your account %s", email)
		text := "Hi there,\n\nActive sessions cleared\n\n" +
			now.Format("Monday 2 January 2006") + " at " + now.Format("15:04") + "\n\n" +
			desc + "\n\n" +
			"Quick guide: " + siteURL() + "/learn/how-to/Keeping_your_account_secure\n\n" +
			"Thanks,\nOlliTeX Team\n"
		html := "<html><body><h1>Active sessions cleared</h1>" +
			"<p>" + now.Format("Monday 2 January 2006") + " at " + now.Format("15:04") + "</p>" +
			"<p>" + desc + "</p>" +
			"<p><a href=\"" + siteURL() + "/learn/how-to/Keeping_your_account_secure\">quick guide</a></p>" +
			"</body></html>"
		if err := mail.Send(email, subject, text, html); err != nil {
			// Node logs the SMTP failure and still 201s (pinned: mail never
			// breaks the response).
			log.Printf("webgo: sessions-clear mail to %s: %v", email, err)
		}

		res.SendStatus(201)
	}
}

// ---------- moment ----------

func sessionRow(ip, createdISO string) string {
	mid, err := time.Parse(time.RFC3339, createdISO)
	return "<tr><td>" + ip + "</td><td>" + momentUTC(mid, err == nil) + " UTC</td></tr>"
}

func otherRows(in []sessionEntry) string {
	var b strings.Builder
	for i := range in {
		b.WriteString(sessionRow(in[i].IPAddress, in[i].SessionCreated))
	}
	return b.String()
}

// momentUTC — moment(t).utc().format('Do MMM YYYY, h:mm a') (pinned).

// momentUTC — moment(t).utc().format('Do MMM YYYY, h:mm a') (pinned).
func momentUTC(t time.Time, valid bool) string {
	if !valid {
		return "Invalid date"
	}
	t = t.UTC()
	day := t.Day()
	suf := "th"
	switch {
	case day%10 == 1 && day%100 != 11:
		suf = "st"
	case day%10 == 2 && day%100 != 12:
		suf = "nd"
	case day%10 == 3 && day%100 != 13:
		suf = "rd"
	}
	mm := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}[int(t.Month())-1]
	hour := t.Hour()
	ampm := "am"
	h12 := hour % 12
	if h12 == 0 {
		h12 = 12
	}
	if hour >= 12 {
		ampm = "pm"
	}
	return fmt.Sprintf("%d%s %s %d, %d:%02d %s",
		day, suf, mm, t.Year(), h12, t.Minute(), ampm)
}
