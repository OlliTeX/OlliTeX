package templates

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"ollitex/go/services/web/core"
)

// TestSessionMenuGrantShapes pins ExposedSettings.canManageTemplatesMenu for
// the two session shapes Node accepts (SessionManager.getSessionUser:
// session.user first, session.passport.user fallback) — the OlliTeX CE
// session carries user.isAdmin under `user`, which must grant the menu for
// admins on every page (404 page parity, P7 U1).
func TestSessionMenuGrantShapes(t *testing.T) {
	mk := func(fields ...[2]string) *core.Session {
		doc := map[string]json.RawMessage{}
		for _, f := range fields {
			doc[f[0]] = json.RawMessage(f[1])
		}
		return &core.Session{SessID: "x", Doc: doc}
	}
	if SessionMenuGrant(nil) {
		t.Fatal("nil session must not grant")
	}
	if !SessionMenuGrant(mk([2]string{"user", `{"_id":"1234567890abcdef12345678","isAdmin":true}`})) {
		t.Fatal("session.user.isAdmin=true must grant (Node: session.user first)")
	}
	if SessionMenuGrant(mk([2]string{"user", `{"_id":"1234567890abcdef12345678","isAdmin":false}`})) {
		t.Fatal("non-admin session.user must not grant")
	}
	if !SessionMenuGrant(mk([2]string{"passport", `{"user":{"_id":"1234567890abcdef12345678","isAdmin":true}}`})) {
		t.Fatal("session.passport.user.isAdmin=true must grant (Node fallback shape)")
	}
	if SessionMenuGrant(mk([2]string{"passport", `{"user":{"_id":"1234567890abcdef12345678","isAdmin":false}}`})) {
		t.Fatal("passport non-admin must not grant")
	}
}

// TestMenuGrantNilSafe pins the exported wrapper: nil cxt is fail-closed, and
// the session-only ladder step works without an app (A==nil) — the DB ladder
// steps are live-pinned by the e2e gate (U1 404 page: admin granted via the
// DB user.isAdmin path, matching Node hasTemplateAdminAccess).
func TestMenuGrantNilSafe(t *testing.T) {
	if MenuGrant(context.Background(), nil) {
		t.Fatal("nil cxt must be fail-closed")
	}
	sess := &core.Session{SessID: "x", Doc: map[string]json.RawMessage{
		"user": json.RawMessage(`{"_id":"1234567890abcdef12345678","isAdmin":true}`),
	}}
	req, _ := http.NewRequest("GET", "/tag", nil)
	cxt := &core.Cxt{Req: req, Sess: sess, A: nil}
	if !MenuGrant(context.Background(), cxt) {
		t.Fatal("session.user.isAdmin=true must grant even without a DB handle")
	}
}
