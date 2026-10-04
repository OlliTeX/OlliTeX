// f1_invite_preview_test.go — F1 battery: B-side `invited` soft preview +
// A-side S2S outbound (callPeer) + A-side invite-preview route with the
// invitation cache (all oracle-pinned against overleaf-fed).
package federation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- B-side: the `invited` soft preview (invited.mjs oracle) ----------

func TestF1_BInvitedPreview_Matrix(t *testing.T) {
	cases := []struct {
		name         string
		preview      func(localName string) (bool, bool, string)
		payload      map[string]any
		wantOK       bool
		wantCode     string
		wantApproved bool
		wantName     any
	}{
		{
			name:    "missing invitee — soft deny, ok envelope",
			preview: func(string) (bool, bool, string) { t.Fatal("must not be called"); return false, false, "" },
			payload: map[string]any{"localName": "x@b.example"},
			wantOK:  true, wantCode: ``, wantApproved: false, wantName: nil,
		},
		{
			name:    "origin mismatch — soft deny (B is the home oracle)",
			preview: func(string) (bool, bool, string) { t.Fatal("must not be called"); return false, false, "" },
			payload: map[string]any{"invitee": map[string]any{"localName": "x@b.example", "origin": "OTHER.example"}},
			wantOK:  true, wantCode: ``, wantApproved: false, wantName: nil,
		},
		{
			name: "local account found + active — approved with display name",
			preview: func(localName string) (bool, bool, string) {
				if localName != "carol@b.example" {
					t.Fatalf("localName=%q", localName)
				}
				return true, false, "Carol SSO"
			},
			payload: map[string]any{"invitee": map[string]any{"localName": "carol@b.example", "origin": "b.example"}},
			wantOK:  true, wantCode: ``, wantApproved: true, wantName: "Carol SSO",
		},
		{
			name:    "suspended account — soft deny",
			preview: func(string) (bool, bool, string) { return true, true, "Carol SSO" },
			payload: map[string]any{"invitee": map[string]any{"localName": "carol@b.example", "origin": "b.example"}},
			wantOK:  true, wantCode: ``, wantApproved: false, wantName: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &s2sDeps{Site: "https://b.example:3000", PreviewLocalUser: tc.preview}
			ok, code, detail, out := d.invitedPreview(tc.payload)
			if ok != tc.wantOK || code != tc.wantCode || detail != "" {
				t.Fatalf("envelope: ok=%v code=%q detail=%q out=%v (want ok=%v code=%q)", ok, code, detail, out, tc.wantOK, tc.wantCode)
			}
			if out == nil {
				t.Fatal("missing payload")
			}
			if approved, _ := out["approved"].(bool); approved != tc.wantApproved {
				t.Errorf("payload.approved=%v want %v (payload=%v)", approved, tc.wantApproved, out)
			}
			if got, want := out["displayName"], tc.wantName; got != want {
				t.Errorf("payload.displayName=%#v want %#v", got, want)
			}
		})
	}
}

// ---------- A-side: the anchor gate (gateAnchor oracle) ----------

func TestF1_PeerGate_Messages(t *testing.T) {
	store := NewMapStore()
	aKey := bootstrapTestKey(t, store)
	_ = saveTestPeer(t, store, "bx.example", "outbound", string(aKey))

	// 400 — malformed anchor
	if _, _, status, msg := PeerGate(store, "no-colon-here"); status != 400 || !strings.Contains(msg, "invalid anchor") {
		t.Errorf("malformed: status=%d msg=%q", status, msg)
	}
	// 404 — peer not approved
	if _, _, status, msg := PeerGate(store, "bob:unknown.example"); status != 404 || msg != "peer not approved for this origin" {
		t.Errorf("unknown peer: status=%d msg=%q", status, msg)
	}
	// approved + outbound — gates
	peer, anchor, status, msg := PeerGate(store, "bob@bx.example:bx.example")
	if status != 0 || msg != "" || peer == nil || anchor.LocalName != "bob@bx.example" || anchor.Origin != "bx.example" {
		t.Errorf("outbound gate: status=%d msg=%q anchor=%+v", status, msg, anchor)
	}
	// 403 — inbound-only peer
	if err := store.SavePeer(context.Background(), &FederationPeer{Origin: "in.example", Status: "approved", Direction: "inbound"}); err != nil {
		t.Fatal(err)
	}
	if _, _, status, msg := PeerGate(store, "bob@in.example:in.example"); status != 403 || !strings.Contains(msg, "not approved for outbound") {
		t.Errorf("inbound-only: status=%d msg=%q", status, msg)
	}
}

// ---------- A-side: callPeer wire (buildS2sRequest + callPeer oracle) ----------

func TestF1_CallPeer_WireAndStatuses(t *testing.T) {
	aStore := NewMapStore()
	bootstrapTestKey(t, aStore)

	var gotHeader string
	var gotAssertionType string
	var gotBody map[string]any
	var gotAuth string

	// B: the business envelope.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/federation/s2s" {
			t.Errorf("wire: %s %s (want POST /federation/s2s)", r.Method, r.URL.Path)
		}
		gotHeader = r.Header.Get("Content-Type")
		gotAssertionType = r.Header.Get("client_assertion_type")
		gotAuth = r.Header.Get("client_assertion")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"payload":{"approved":true,"displayName":"Carol SSO"}}`))
	}))
	defer srv.Close()
	origin := strings.TrimPrefix(srv.URL, "http://")

	caller := &S2SCall{Store: aStore, Site: "https://a.example", Scheme: "http"}
	env, err := caller.Call(context.Background(), origin, "invited", map[string]any{"invitee": map[string]any{"origin": origin, "localName": "carol"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	_ = env
	if gotHeader != "application/json" {
		t.Errorf("Content-Type=%q", gotHeader)
	}
	if gotAssertionType != "urn:ietf:params:oauth:client-assertion-type:jwt" {
		t.Errorf("client_assertion_type=%q", gotAssertionType)
	}
	if gotAuth == "" || len(strings.Split(gotAuth, ".")) != 3 {
		t.Errorf("client_assertion is not a Compact JWS: %q", gotAuth)
	}
	if gotBody["action"] != "invited" || gotBody["to"] != origin {
		t.Errorf("body=%v (want action=invited to=%s)", gotBody, origin)
	}
	if gotBody["from"] != "a.example" {
		t.Errorf("body.from=%v want a.example", gotBody["from"])
	}
	if _, ok := gotBody["ts"].(float64); !ok {
		t.Errorf("body.ts missing/non-numeric: %v", gotBody)
	}
	audit, ok := gotBody["payload"].(map[string]any)
	if !ok || audit["invitee"] == nil {
		t.Errorf("payload.invitee missing: %v", gotBody)
	}
}

func TestF1_CallPeer_Refusals(t *testing.T) {
	aStore := NewMapStore()
	bootstrapTestKey(t, aStore)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://elsewhere.example/x", http.StatusFound)
	}))
	defer redirect.Close()
	rOrg := strings.TrimPrefix(redirect.URL, "http://")

	rateLim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer rateLim.Close()
	lOrg := strings.TrimPrefix(rateLim.URL, "http://")

	serverErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"code":"peer-expired","detail":"peer trust expired"}`))
	}))
	defer serverErr.Close()
	eOrg := strings.TrimPrefix(serverErr.URL, "http://")

	call := func(org string) *PeerRefusal {
		_, err := (&S2SCall{Store: aStore, Site: "https://a.example", Scheme: "http"}).Call(context.Background(), org, "invited", map[string]any{})
		p, ok := AsPeerRefusal(err)
		if !ok {
			t.Fatalf("want *PeerRefusal, got %T (%v)", err, err)
		}
		return p
	}

	if p := call(rOrg); p.Code != "redirect-refused" {
		t.Errorf("redirect: code=%q want redirect-refused", p.Code)
	}
	if p := call(lOrg); p.Code != "rate-limited" {
		t.Errorf("429: code=%q want rate-limited", p.Code)
	}
	if p := call(eOrg); p.Code != "peer-expired" || p.Detail != "peer trust expired" {
		t.Errorf("500: code=%q detail=%q (want the B-side code pass-through)", p.Code, p.Detail)
	}
}

// ---------- A-side: the preview flow (route logic minus core glue) ----------

type fakeSeam struct{ m map[string]string }

func (f *fakeSeam) INCR(string) (int64, error)               { return 0, nil }
func (f *fakeSeam) EXPIRE(string, int64) error               { return nil }
func (f *fakeSeam) TTL(string) (int64, error)                { return -1, nil }
func (f *fakeSeam) GET(k string) (string, bool, error)       { v, ok := f.m[k]; return v, ok, nil }
func (f *fakeSeam) SETEX(k, v string, t time.Duration) error { f.m[k] = v; return nil }
func (f *fakeSeam) SETEXReply(k, v string, t time.Duration) (bool, error) {
	if _, ok := f.m[k]; ok {
		return false, nil
	}
	f.m[k] = v
	return true, nil
}

func TestF1_PreviewFlow_HappyAndCache(t *testing.T) {
	aStore := NewMapStore()
	aKey := bootstrapTestKey(t, aStore)
	bOrg := "bx.example"
	if err := saveTestPeer(t, aStore, bOrg, "outbound", string(aKey)); err != nil {
		t.Fatal(err)
	}

	var calls int32
	fr := &fakeSeam{m: map[string]string{}}
	flow := &previewDeps{
		Store: aStore,
		Red:   fr,
		Call: func(ctx context.Context, peerOrigin, action string, payload map[string]any) (map[string]any, error) {
			if peerOrigin != bOrg || action != "invited" {
				return nil, &PeerRefusal{Code: "wire-test", Detail: "unexpected call " + action + " → " + peerOrigin}
			}
			atomic.AddInt32(&calls, 1)
			inv, _ := payload["invitee"].(map[string]any)
			name, _ := inv["localName"].(string)
			return map[string]any{"ok": true, "payload": map[string]any{
				"approved": name == "carol@b.example",
				"displayName": func() any {
					if name == "carol@b.example" {
						return "Carol SSO"
					}
					return nil
				}(),
			}}, nil
		},
	}

	// 1) happy path — B's payload verbatim + cached.
	status, body := previewFlow(flow, "carol@b.example:"+bOrg)
	if status != 200 || body["approved"] != true || body["displayName"] != "Carol SSO" {
		t.Fatalf("happy: status=%d body=%v", status, body)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("B hit count=%d want 1", n)
	}

	// 2) cache hit — B NOT hit again (the 60 s invitation cache).
	status, body = previewFlow(flow, "carol@b.example:"+bOrg)
	if status != 200 || body["approved"] != true {
		t.Fatalf("cached: status=%d body=%v", status, body)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("B hit count=%d want 1 (cache must serve the 2nd call)", n)
	}

	// 3) soft deny from B (no local account) — 200 + approved:false, NOT cached.
	status, body = previewFlow(flow, "gone@b.example:"+bOrg)
	if status != 200 || body["approved"] != false {
		t.Errorf("soft deny: status=%d body=%v", status, body)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("B hit count=%d want 2 (soft denials are not cached)", n)
	}

	// 4) gate failures keep their oracle statuses/messages.
	if st, body := previewFlow(flow, "bob:unknown.example"); st != 404 || body["message"] != "peer not approved for this origin" {
		t.Errorf("404 gate: st=%d body=%v", st, body)
	}
	if st, body := previewFlow(flow, "nocolon"); st != 400 {
		t.Errorf("400 gate: st=%d body=%v", st, body)
	}
	if st, _ := previewFlow(flow, ""); st != 400 {
		t.Errorf("empty anchor: st=%d want 400", st)
	}

	// 5) a B-side refusal degrades (NOT a refusal to the user) —
	//    200 + approved:false + degraded:true (05 §4.1).
	flow.Call = func(context.Context, string, string, map[string]any) (map[string]any, error) {
		return nil, &PeerRefusal{Code: "rate-limited", Detail: "peer rate limit exceeded"}
	}
	status, body = previewFlow(flow, "dave@bx.example:"+bOrg)
	if status != 200 || body["approved"] != false || body["degraded"] != true {
		t.Errorf("degrade: status=%d body=%v (want 200 approved:false degraded:true)", status, body)
	}
}

// Test helpers for the peer ledger + bootstrap (s1 infrastructure).
func bootstrapTestKey(t *testing.T, store *MapStore) string {
	t.Helper()
	ap := &KeyProvider{Store: store, Site: "https://a.example"}
	if err := ap.Bootstrap(); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	k, err := ap.LeafSigningKey()
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	js, err := json.Marshal(k.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(js)
}

func saveTestPeer(t *testing.T, store *MapStore, origin, direction, anchor string) error {
	t.Helper()
	return store.SavePeer(context.Background(), &FederationPeer{
		Origin:     origin,
		Mode:       "pairwise",
		Status:     "approved",
		Direction:  direction,
		AnchorJwks: anchor,
	})
}
