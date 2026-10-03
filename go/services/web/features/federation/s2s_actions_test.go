// s2s_actions_test.go — S5 dispatch pipeline: verify → replay → rate →
// action legs → audit rows. In-package fakes only (MapStore, fakeRedis
// from s1_test.go) — no network, no mongo, no redis.

package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"ollitex/go/services/web/core"
)

// auditRec — records WriteAudit calls (operation + projectId + info).
type auditRec struct {
	mu   sync.Mutex
	rows []auditRow
}

type auditRow struct {
	op        string
	projectId *string
	info      map[string]any
}

func (r *auditRec) WriteAudit(ctx context.Context, operation string, projectId *string, info map[string]any, initiatorId, ipAddress string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, auditRow{op: operation, projectId: projectId, info: info})
	return nil
}

func (r *auditRec) find(t *testing.T, op string) auditRow {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.op == op {
			return row
		}
	}
	t.Fatalf("audit row %q not found (rows: %+v)", op, r.rows)
	return auditRow{}
}

// s2sPair — A (signer, a.example) + B (verifier, b.example): B's approved
// peer pins A's leaf-public key as its anchor JWK, so
// VerifyS2sClientAssertion passes (kid + ES256 + iss + aud + exp).
func s2sPair(t *testing.T, action string, payload map[string]any) (assertion, from string, bStore *MapStore, data map[string]any) {
	t.Helper()
	aStore := NewMapStore()
	ap := &KeyProvider{Store: aStore, Site: "https://a.example:3000"}
	if err := ap.Bootstrap(); err != nil {
		t.Fatalf("bootstrap A: %v", err)
	}
	key, err := ap.LeafSigningKey()
	if err != nil {
		t.Fatalf("leaf key A: %v", err)
	}
	anchor, err := json.Marshal(key.PublicKey)
	if err != nil {
		t.Fatalf("marshal anchor: %v", err)
	}
	bStore = NewMapStore()
	if err := bStore.SavePeer(context.Background(), &FederationPeer{
		Origin:     "a.example",
		Mode:       "pairwise",
		Status:     "approved",
		AnchorJwks: string(anchor),
		Kid:        key.Kid,
	}); err != nil {
		t.Fatalf("save peer: %v", err)
	}
	req, err := ap.BuildS2sRequest("b.example", action, payload)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	data, _ = req.Body["payload"].(map[string]any)
	return req.Headers["client_assertion"], "a.example", bStore, data
}

// bDeps — B-side pipeline deps with the shared redis fake + audit rec.
func bDeps(store *MapStore, fr *fakeRedis, rec *auditRec) *s2sDeps {
	return &s2sDeps{
		Setting: loadSettings(),
		Site:    "https://b.example:3000",
		Salt:    "test-salt",
		Store:   store,
		Redis:   fr,
		Now:     time.Now,
		AuditW:  rec,
	}
}

func TestS2SPipeline_VerifyDispatchReplay(t *testing.T) {
	data := map[string]any{"projectId": "65000000000000000000dead"}
	assertion, from, bStore, _ := s2sPair(t, "invited", data)
	rec := &auditRec{}
	d := bDeps(bStore, newFakeRedis(), rec)

	// First call: verified + replay-claimed + dispatch (honest pending).
	out := d.pipeline("invited", from, assertion, data)
	if out.status != 200 {
		t.Fatalf("first call: want 200, got %d body=%v", out.status, out.body)
	}
	if out.body["ok"] != false || out.body["code"] != "s11-pending" {
		t.Errorf("first call: want honest s11-pending, got %v", out.body)
	}
	// Same assertion (same jti) again → LOCKED 401 replay-jti.
	out2 := d.pipeline("invited", from, assertion, data)
	if out2.status != 401 || out2.body["code"] != "replay-jti" {
		t.Fatalf("replay: want 401 replay-jti, got %d %v", out2.status, out2.body)
	}
}

func TestS2SPipeline_RateLimited_RevokeBudget(t *testing.T) {
	// revoke budget is 5 / 1200 s per caller origin (03 §5) → the 6th
	// verified call must 429 with the LOCKED code + Allow-Retry-After.
	fr := newFakeRedis()
	rec := &auditRec{}
	for i := 0; i < 6; i++ {
		data := map[string]any{"projectId": "65000000000000000000dead"}
		assertion, from, bStore, _ := s2sPair(t, "revoke", data)
		d := bDeps(bStore, fr, rec) // shared redis fake → shared budget
		out := d.pipeline("revoke", from, assertion, data)
		if i < 5 {
			if out.status != 200 {
				t.Fatalf("call %d: want 200, got %d body=%v", i, out.status, out.body)
			}
			continue
		}
		if out.status != 429 {
			t.Fatalf("call 6: want 429, got %d body=%v", out.status, out.body)
		}
		if out.body["code"] != "rate-limited" {
			t.Errorf("call 6: want rate-limited, got %v", out.body)
		}
		if ra, ok := out.headers["Allow-Retry-After"]; !ok || ra == "" {
			t.Errorf("call 6: want Allow-Retry-After header, got %v", out.headers)
		}
	}
}

func TestS2SPipeline_PeerNotApproved(t *testing.T) {
	// A signs fine, but B's peer row is NOT approved → LOCKED refinement:
	// peer-not-approved (not the generic peer-unknown).
	aStore := NewMapStore()
	ap := &KeyProvider{Store: aStore, Site: "https://a.example:3000"}
	if err := ap.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	req, err := ap.BuildS2sRequest("b.example", "invited", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	bStore := NewMapStore()
	if err := bStore.SavePeer(context.Background(), &FederationPeer{
		Origin: "a.example", Mode: "pairwise", Status: "pending",
	}); err != nil {
		t.Fatal(err)
	}
	d := &s2sDeps{Store: bStore, Site: "https://b.example:3000", Salt: "t", Now: time.Now}
	out := d.pipeline("invited", "a.example", req.Headers["client_assertion"], map[string]any{})
	if out.status != 401 || out.body["code"] != "peer-not-approved" {
		t.Fatalf("want 401 peer-not-approved, got %d %v", out.status, out.body)
	}
}

func TestS2SPipeline_MissingAssertion(t *testing.T) {
	d := &s2sDeps{Store: NewMapStore(), Site: "https://b.example:3000", Now: time.Now}
	out := d.pipeline("invited", "a.example", "", map[string]any{})
	if out.status != 401 || out.body["code"] != "bad-signature" {
		t.Fatalf("want 401 bad-signature, got %d %v", out.status, out.body)
	}
}

func TestS2SPipeline_RevokeSweep(t *testing.T) {
	// revoke leg: active export grant (with PAT) → revoked + best-effort
	// PAT sweep + honest counts + ExportSwept project-scoped audit row.
	_, from, bStore, _ := s2sPair(t, "revoke", nil)
	pid := "65000000000000000000dead"
	data := map[string]any{"projectId": pid}
	if err := bStore.SaveExportGrant(context.Background(), &FederationExportGrant{
		ProjectID:  pid,
		HomeOrigin: "b.example",
		PatID:      "pat-1",
		Status:     "active",
	}); err != nil {
		t.Fatal(err)
	}
	rec := &auditRec{}
	d := bDeps(bStore, newFakeRedis(), rec)
	patSwept := []string{}
	d.DeleteFederationPAT = func(patID string) bool {
		patSwept = append(patSwept, patID)
		return true
	}
	ok, code, _, payload := d.dispatch("revoke", from, data)
	if !ok || code != "revoked" {
		t.Fatalf("revoke: want ok/revoked, got %v %q", ok, code)
	}
	grants, gerr := bStore.ExportGrantsForProject(context.Background(), pid)
	if gerr != nil || len(grants) != 1 || grants[0].Status != "revoked" {
		t.Fatalf("grant not revoked: err=%v %+v", gerr, grants)
	}
	if len(patSwept) != 1 || patSwept[0] != "pat-1" {
		t.Fatalf("PAT sweep: want [pat-1], got %v", patSwept)
	}
	if fmt.Sprint(payload["grantsRevoked"]) != "1" {
		t.Errorf("honest count grantsRevoked: want 1, got %v", payload)
	}
	// pipeline-level audit leg (04 §8):
	d.audit("revoke", ok, code, from, "", pid, map[string]any{})
	row := rec.find(t, "federation_export_swept")
	if row.projectId == nil || *row.projectId != pid {
		t.Errorf("ExportSwept row must carry projectId, got %+v", row)
	}
	if row.info["origin"] != from {
		t.Errorf("ExportSwept info.origin: got %v", row.info)
	}
}

func TestS2SPipeline_AuthorizeInvite(t *testing.T) {
	cases := []struct {
		name      string
		resolve   func(origin, localName string) (string, string, string)
		wantOK    bool
		wantCode  string
		wantAudit string
	}{
		{
			name: "mirror-resolves",
			resolve: func(origin, localName string) (string, string, string) {
				return "6500000000000000000000aa", "federation", ""
			},
			wantOK: true, wantCode: "invitee-resolved",
			wantAudit: AuditTypes.InviteApproved,
		},
		{
			name: "suspended-denied",
			resolve: func(origin, localName string) (string, string, string) {
				return "6500000000000000000000aa", "email", "invitee-disabled"
			},
			wantOK: false, wantCode: "invitee-disabled",
			wantAudit: AuditTypes.InviteDenied,
		},
		{
			name: "unknown-denied",
			resolve: func(origin, localName string) (string, string, string) {
				return "", "", "invitee-unknown"
			},
			wantOK: false, wantCode: "invitee-unknown",
			wantAudit: AuditTypes.InviteDenied,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			localName := "zoe@b.example"
			data := map[string]any{"localName": localName}
			_, from, bStore, _ := s2sPair(t, "authorize-invite", data)
			rec := &auditRec{}
			fr := newFakeRedis()
			d := bDeps(bStore, fr, rec)
			d.ResolveUser = tc.resolve
			ok, code, _, payload := d.dispatch("authorize-invite", from, data)
			if ok != tc.wantOK || code != tc.wantCode {
				t.Fatalf("want ok=%v code=%s, got %v %q", tc.wantOK, tc.wantCode, ok, code)
			}
			if tc.wantOK && payload["user"] != "6500000000000000000000aa" {
				t.Errorf("want resolved user hex, got %v", payload)
			}
			// cached-invite marker (04 §6) — present only on success.
			if tc.wantOK {
				hash := SaltedLocalNameHash(d.Salt, localName, from)
				cached, cerr := GetCachedInvite(fr, from, hash)
				if cerr != nil || !strings.Contains(cached, "6500000000000000000000aa") {
					t.Errorf("cached invite marker: err=%v cached=%q", cerr, cached)
				}
			}
			// pipeline-level audit leg (04 §8):
			d.audit("authorize-invite", ok, code, from, localName, "", map[string]any{})
			row := rec.find(t, tc.wantAudit)
			if !tc.wantOK && row.info["reason"] != tc.wantCode {
				t.Errorf("denial reason must be audited, got %v", row.info)
			}
		})
	}
}

func TestS2SPipeline_ExportLegs(t *testing.T) {
	pid := "65000000000000000000beef"
	// gate OFF (default) → honest export-disabled + ExportDenied row.
	_, from, bStore, _ := s2sPair(t, "export-project", nil)
	rec := &auditRec{}
	d := bDeps(bStore, newFakeRedis(), rec)
	ok, code, _, _ := d.dispatch("export-project", from, map[string]any{"projectId": pid})
	if ok || code != "export-disabled" {
		t.Fatalf("gate off: want export-disabled, got %v %q", ok, code)
	}
	d.audit("export-project", ok, code, from, "", pid, map[string]any{})
	rec.find(t, "federation_export_denied")

	// gate ON → honest s11-pending + ExportRequested row.
	t.Setenv("FEDERATION_EXPORT_ENABLED", "true")
	rec2 := &auditRec{}
	d2 := bDeps(bStore, newFakeRedis(), rec2)
	ok2, code2, _, _ := d2.dispatch("export-project", from, map[string]any{"projectId": pid})
	if ok2 || code2 != "s11-pending" {
		t.Fatalf("gate on: want s11-pending, got %v %q", ok2, code2)
	}
	d2.audit("export-project", ok2, code2, from, "", pid, map[string]any{})
	rec2.find(t, "federation_export_requested")
}

// TestS2SHandler_DispatchWired — the mounted handler now 401s on a known
// action without an assertion (retires the old `action-pending` stub
// pin), and accepts the `payload` envelope alias (overleaf-fed §9).
func TestS2SHandler_DispatchWired(t *testing.T) {
	t.Setenv("FEDERATION_ENABLED", "true")
	a := core.New(&core.Config{Profile: "web"}, nil)
	w := doFed(t, a, "POST", "/federation/s2s",
		`{"from":"b","to":"a","action":"invited","data":{"projectId":"p1"}}`, nil)
	if w.Code != 401 {
		t.Fatalf("want 401 (assertion required), got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"bad-signature"`) {
		t.Errorf("want bad-signature code, got %s", w.Body.String())
	}
	w2 := doFed(t, a, "POST", "/federation/s2s",
		`{"from":"b","to":"a","action":"revoke","payload":{"projectId":"p1"}}`, nil)
	if w2.Code != 401 {
		t.Fatalf("payload alias: want 401, got %d body=%s", w2.Code, w2.Body.String())
	}
}
