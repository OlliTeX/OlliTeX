// zz_live_fedb_test.go — B→3: the LIVE dual-instance S11 round-trip
// (A→B federated invite preview over a REAL HTTP hop, both ends full Go
// web instances, per the overleaf-fed oracle).
//
// Why a live harness: the hermetic battery (f1_invite_preview_test.go /
// f2_export_test.go) pins the LOGIC against stubbed transports. This
// test pins the WIRE: A's outbound S2S `invited` call (s2scall.go —
// signed assertion, the LOCKED {action,from,to,ts,payload} envelope) →
// B's /federation/s2s router (s2s.go — the oracle ordering: settings
// gate, envelope decode, action known, assertion verify against the
// PINNED anchor, jti replay, rate limit) → B's `invited` soft preview
// (s2s_actions.go invitedPreview — the invited.mjs oracle: ALWAYS the ok
// envelope, payload.approved distinguishes; not-found is a valid
// preview result, NOT a refusal) → back to A, which renders the B
// payload (invite_preview.go previewFlow) or degrades when B is
// unreachable (a preview failure is NOT a refusal).
//
// ENV GATE (all required unless noted — skipped when unset, per the
// zz_live_* house pattern; the owner stands up the instances + pins):
//
//	FED_LIVE_A_BASE         instance A base (http://127.0.0.1:PORT_A)
//	FED_LIVE_A_EMAIL        A admin (a logged-in A user starts the preview)
//	FED_LIVE_A_PW           A admin password
//	FED_LIVE_B_ORIGIN       the origin A sees B as (the pinned peer origin;
//	                          also the host of B's /federation/s2s — e.g.
//	                          127.0.0.1:PORT_B)
//	FED_LIVE_B_LOCALNAME    B-side user that EXISTS (approved leg) [carol]
//	FED_LIVE_B_MISSING      B-side user that MUST NOT exist [zeta-<random>]
//
// OWNER PRECONDITION (the TOFU owner-window): both instances booted with
// federation ENABLED (site setting) and the peer pins present — A's peer
// store: B origin approved, direction outbound|both; B's peer store: A
// origin approved with A's anchorJwks pinned (POST /admin/federation/peers
// TOFU pin, then /approve). When a pin is missing the legs below fail
// LOUDLY with the exact machine code (that is a correct refusal to
// report, not a pass):
//
//   - B rejects A's assertion → A degrades → {approved:false,
//     degraded:true} → leg 4 FAILS with the wire detail (degraded is the
//     "the B round trip did NOT happen" marker).
//   - A's peer gate (B not approved outbound on A) → 404
//     "peer not approved for this origin" → leg 4 FAILS.
package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------- live helpers (the raw-HTTP login contract, curl-proven) ----------

type fedLiveInst struct {
	base   string
	email  string
	pw     string
	client *http.Client
}

// fedLiveLogin — GET /login (save cookies + the csrfToken meta), then
// POST /login JSON {email,password} + X-XSRF-TOKEN (the Go web's
// csrfTokenFrom: body._csrf||query._csrf||csrf-token||xsrf-token||
// x-csrf-token||x-xsrf-token) on the same jar → the overleaf.sid session.
// Contract: 302 → /project on success (not a body).
func fedLiveLogin(t *testing.T, inst *fedLiveInst) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.base+"/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := inst.client.Do(req)
	if err != nil {
		t.Fatalf("GET /login on %s: %v (instance down?)", inst.base, err)
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /login -> %d: %s", res.StatusCode, string(body[:min(len(body), 200)]))
	}
	meta := string(body)
	csrf := ""
	if i := strings.Index(meta, `csrfToken" content="`); i > 0 {
		csrf = meta[i+len(`csrfToken" content="`):]
		csrf = csrf[:strings.IndexAny(csrf, `" `)]
	}
	if csrf == "" {
		t.Fatalf("login page has no csrfToken meta (base=%s)", inst.base)
	}
	payload, _ := json.Marshal(map[string]string{"email": inst.email, "password": inst.pw})
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.base+"/login", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-XSRF-TOKEN", csrf)
	res2, err := inst.client.Do(req2)
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != 302 {
		t.Fatalf("POST /login -> %d (want 302 → /project; auth failure = wrong fixture creds or wrong instance)", res2.StatusCode)
		if loc := res2.Header.Get("Location"); loc != "" {
			t.Logf("location: %s", loc)
		}
	}
}

// fedLiveGet — GET path on the instance (session jar), returns (status,
// raw JSON body).
func fedLiveGet(t *testing.T, inst *fedLiveInst, path string) (int, []byte, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := inst.client.Do(req)
	if err != nil {
		t.Fatalf("GET %s%s: %v", inst.base, path, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	loc := res.Header.Get("Location")
	return res.StatusCode, body, loc
}

// ---------- the harness ----------

func TestFedB_LiveS11RoundTrip(t *testing.T) {
	if os.Getenv("FED_LIVE_A_BASE") == "" || os.Getenv("FED_LIVE_B_ORIGIN") == "" {
		t.Skip("set FED_LIVE_A_BASE + FED_LIVE_A_EMAIL/_PW + FED_LIVE_B_ORIGIN (+FED_LIVE_B_LOCALNAME) to run the live dual-instance S11 round-trip — the hermetic f1/f2 battery already pins the logic; this pins the WIRE (real A→B S2S `invited` over signed assertion + B's pin verify + soft envelope back)")
	}
	email := os.Getenv("FED_LIVE_A_EMAIL")
	pw := os.Getenv("FED_LIVE_A_PW")
	if email == "" || pw == "" {
		t.Skip("FED_LIVE_A_EMAIL + FED_LIVE_A_PW required (a logged-in A user starts the preview)")
	}
	bOrigin := strings.ToLower(strings.TrimSpace(os.Getenv("FED_LIVE_B_ORIGIN")))
	bLocal := os.Getenv("FED_LIVE_B_LOCALNAME")
	if bLocal == "" {
		bLocal = "carol"
	}
	bMissing := os.Getenv("FED_LIVE_B_MISSING")
	if bMissing == "" {
		bMissing = fmt.Sprintf("zed-live-missing-%d", time.Now().UnixNano()%1_000_000_000)
	}
	// same-host dual containers: distinct names + ports (the docker
	// container name on each side is the operator's business — origins
	// are the PINNED values from the operator's peer setup, passed via
	// env).
	a := &fedLiveInst{base: strings.TrimRight(os.Getenv("FED_LIVE_A_BASE"), "/"), email: email, pw: pw}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a.client = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	t.Run("A-login", func(t *testing.T) {
		fedLiveLogin(t, a)
	})

	t.Run("PRE-federation-enabled-on-A", func(t *testing.T) {
		// probe the settings gate FIRST: an unconfigured instance (the
		// common first-run state) answers the feature-off envelope, and
		// every later leg would otherwise misreport it as a wire/B-side
		// failure — name the real precondition instead.
		st, body, _ := fedLiveGet(t, a, "/api/federation/invite/preview")
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if code, _ := m["code"].(string); code == "federation-off" {
			t.Fatalf("federation is DISABLED on A (envelope: %s) — owner precondition: enable the federation setting on instance A (site setting `federation`), then the peer pins (A: B origin approved outbound|both; B: A origin approved with A's anchorJwks pinned — POST /admin/federation/peers TOFU + /approve)", body)
		}
		if st != 400 {
			t.Fatalf("probe status=%d body=%s (want 400 anchor-required on a configured instance)", st, body)
		}
	})

	t.Run("NEG-missing-anchor", func(t *testing.T) {
		st, body, _ := fedLiveGet(t, a, "/api/federation/invite/preview")
		if st != 400 {
			t.Fatalf("status=%d body=%s (want 400 anchor-required)", st, body)
		}
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if msg, _ := m["message"].(string); msg != "anchor required" {
			t.Fatalf("message=%v (oracle: 'anchor required') body=%s", m["message"], body)
		}
	})

	t.Run("NEG-peer-gate-unknown-origin", func(t *testing.T) {
		// an origin A has NOT pinned — the A-side gate refuses BEFORE
		// any wire (no B round trip, no degrade).
		st, body, _ := fedLiveGet(t, a, "/api/federation/invite/preview?anchor="+url.QueryEscape(bLocal+":not-a-pinned-origin.example"))
		if st != 404 {
			t.Fatalf("status=%d body=%s (want 404 peer-gate)", st, body)
		}
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if msg, _ := m["message"].(string); !strings.Contains(msg, "peer not approved") {
			t.Fatalf("message=%q (oracle peer-gate 'peer not approved for this origin') body=%s", msg, body)
		}
	})

	t.Run("WIRE-deny-missing-B-user", func(t *testing.T) {
		st, body, _ := fedLiveGet(t, a, "/api/federation/invite/preview?anchor="+url.QueryEscape(bMissing+":"+bOrigin))
		if st != 200 {
			t.Fatalf("status=%d body=%s (oracle: a preview failure is still 200/degraded or the soft envelope)", st, body)
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("envelope not JSON: %s", body)
		}
		if approved, _ := m["approved"].(bool); approved {
			t.Fatalf("approved=user that must not exist — wire=%s (B-side preview bug?)", body)
		}
		if deg, _ := m["degraded"].(bool); deg {
			// the B round trip did NOT happen (A degraded) — the wire is
			// unproven: report the precondition failure loudly (a pin is
			// missing on one side, B is unreachable from A, or B refused
			// the assertion: peer-unknown/unknown-kid/bad-signature).
			t.Fatalf("DEGRADED — the A→B wire did NOT complete (pin missing on A or B, or B rejected the assertion): %s", body)
		}
		t.Logf("soft-deny envelope (oracle payload shape): %s", body)
	})

	t.Run("WIRE-approve-existing-B-user", func(t *testing.T) {
		st, body, _ := fedLiveGet(t, a, "/api/federation/invite/preview?anchor="+url.QueryEscape(bLocal+":"+bOrigin))
		if st != 200 {
			t.Fatalf("status=%d body=%s (want 200 soft envelope)", st, body)
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("envelope not JSON: %s", body)
		}
		if deg, _ := m["degraded"].(bool); deg {
			t.Fatalf("DEGRADED — the A→B wire did NOT complete: %s", body)
		}
		approved, _ := m["approved"].(bool)
		if !approved {
			t.Fatalf("approved=false for a B user that exists: %s (B-side preview returned soft-deny — check the B account state)", body)
		}
		if dn, ok := m["displayName"].(string); ok && dn == "" {
			t.Fatalf("displayName empty string (oracle: null or a name): %s", body)
		}
		t.Logf("approved envelope (the LIVE wire proof): %s", body)
	})

	t.Run("WIRE-stable-repeat-(cache)", func(t *testing.T) {
		// the 60 s A-side invitation cache (invite_preview.go) makes an
		// immediate repeat return the same answer — assert stability
		// (identity of the approved flag is the contract; the cache
		// itself is the A-side behavior).
		st, body, _ := fedLiveGet(t, a, "/api/federation/invite/preview?anchor="+url.QueryEscape(bLocal+":"+bOrigin))
		if st != 200 {
			t.Fatalf("status=%d body=%s", st, body)
		}
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if approved, _ := m["approved"].(bool); !approved {
			t.Fatalf("repeat leg not stable: %s", body)
		}
	})
}

// TestFedB_LiveBPing — optional B-admin evidence leg (skips itself when
// the B admin creds are absent): list B's peer store — the pin of A's
// origin is the precondition the wire depends on, so its absence is
// reported HERE (with the exact status) instead of as an opaque degrade
// on the A side.
func TestFedB_LiveBPing(t *testing.T) {
	base := os.Getenv("FED_LIVE_B_BASE")
	if base == "" {
		bOrigin := os.Getenv("FED_LIVE_B_ORIGIN")
		if bOrigin == "" {
			t.Skip("no live B instance configured (FED_LIVE_B_BASE/FED_LIVE_B_ORIGIN)")
			return
		}
		base = "http://" + bOrigin
	}
	email, pw := os.Getenv("FED_LIVE_B_EMAIL"), os.Getenv("FED_LIVE_B_PW")
	if email == "" || pw == "" {
		t.Skip("FED_LIVE_B_EMAIL + FED_LIVE_B_PW not set — B-side admin evidence leg off (the A-driven leg proves the wire regardless)")
	}
	b := &fedLiveInst{base: strings.TrimRight(base, "/"), email: email, pw: pw}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	b.client = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	fedLiveLogin(t, b)

	aOrigin := os.Getenv("FED_LIVE_A_ORIGIN")
	if aOrigin == "" {
		u, err := url.Parse(strings.TrimRight(os.Getenv("FED_LIVE_A_BASE"), "/"))
		if err != nil || u.Host == "" {
			t.Skip("cannot derive A's origin (set FED_LIVE_A_ORIGIN explicitly)")
		}
		aOrigin = strings.ToLower(u.Host)
	}
	st, body, _ := fedLiveGet(t, b, "/admin/federation/peers")
	if st == 302 {
		t.Fatalf("B admin login did not stick (redirected to %s — creds?)", "")
	}
	if st != 200 {
		t.Fatalf("GET /admin/federation/peers on B -> %d: %s", st, body[:min(len(body), 300)])
	}
	var list []map[string]any
	if err := json.Unmarshal(body, &list); err != nil {
		// some shapes: {peers:[...]} — accept both.
		var wrap struct {
			Peers []map[string]any `json:"peers"`
		}
		if err2 := json.Unmarshal(body, &wrap); err2 == nil {
			list = wrap.Peers
		} else {
			t.Fatalf("peer list not parseable: %s", body)
		}
	}
	var pinned *map[string]any
	for i := range list {
		if p, _ := list[i]["origin"].(string); p == aOrigin {
			pinned = &list[i]
			break
		}
	}
	if pinned == nil {
		fmt.Printf("NOTE: B's peer store has NO row for A's origin %s — the A→B wire CANNOT complete until the owner pins it (POST /admin/federation/peers TOFU + /approve).\n", aOrigin)
	} else {
		status, _ := (*pinned)["status"].(string)
		if status != "approved" {
			fmt.Printf("NOTE: B's peer row for A origin %s is %q (want approved).\n", aOrigin, status)
		} else {
			t.Logf("B pinned A origin %s as approved — the wire precondition is visible on B's side", aOrigin)
		}
	}
}
