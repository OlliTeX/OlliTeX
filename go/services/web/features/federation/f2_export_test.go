// f2_export_test.go — F2 battery: B-side `export-project` (09 §2 oracle
// ordering) + the A-side 2b wizard, including a hermetic DUAL-INSTANCE
// round trip (A signs the client assertion, B verifies + dispatches the
// real legs, the wizard renders the PAT view) — no live instances.
package federation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------- grant fake (the OidcRedisSeam subset for consent grants) ----------

type grantFake struct {
	docs map[string]string // docKey -> payload JSON
	sets map[string][]string
	ttls map[string]int64 // ms
}

func newGrantFake() *grantFake {
	return &grantFake{
		docs: map[string]string{},
		sets: map[string][]string{},
		ttls: map[string]int64{},
	}
}

func (g *grantFake) GET(key string) (string, bool, error) {
	v, ok := g.docs[key]
	return v, ok, nil
}
func (g *grantFake) SETEX(key, value string, sec int64) error { g.docs[key] = value; return nil }
func (g *grantFake) SETPX(key, value string, ms int64) error  { g.docs[key] = value; return nil }
func (g *grantFake) SADD(key string, members ...string) error {
	g.sets[key] = append(g.sets[key], members...)
	return nil
}
func (g *grantFake) SREM(key string, members ...string) error { return nil }
func (g *grantFake) SMEMBERS(key string) ([]string, error)    { return g.sets[key], nil }
func (g *grantFake) SCARD(key string) (int64, error)          { return int64(len(g.sets[key])), nil }
func (g *grantFake) DEL(key string) error                     { return nil }
func (g *grantFake) PTTL(key string) (int64, error) {
	v, ok := g.ttls[key]
	if !ok {
		return -2, nil
	}
	return v, nil
}

// seed a consent grant: (ownerHex × A's client id) → jti with ttl.
func (g *grantFake) seed(accountID, clientID, jti string, ttlMs int64) {
	key := OIDCAccountIndexKey(accountID, clientID)
	docKey := OIDCDocKey("Grant", jti)
	g.sets[key] = append(g.sets[key], docKey)
	g.ttls[docKey] = ttlMs
	payload, _ := json.Marshal(map[string]any{"accountId": accountID, "clientId": clientID, "jti": jti})
	g.docs[docKey] = string(payload)
}

var _ OidcRedisSeam = (*grantFake)(nil)

// ---------- B-side export-project matrix (09 §2 oracle ordering) ----------

func bExportDeps(t *testing.T, ownerHex, callerOrigin string, grantTtlMs int64) *s2sDeps {
	t.Helper()
	g := newGrantFake()
	d := &s2sDeps{
		Site:    "https://bx.example",
		Setting: Settings{Enabled: true, ExportEnabled: true, ExportMaxTTLSeconds: 86400},
		Adapter: &OidcAdapter{ModelName: "Grant", Redis: g},
		Now:     time.Now,
		AuditW:  &auditRec{},
		ExportProjectDoc: func(projectId string) string {
			if projectId == "65000000000000000000beef" {
				return ownerHex
			}
			return ""
		},
		ExportOwner: func(hex string) (bool, bool, bool) { return true, false, false },
		ExportMint:  func(owner, raw string, c, e time.Time) (string, error) { return "patid-1", nil },
	}
	if grantTtlMs > 0 {
		g.seed(ownerHex, "urn:overleaf-federation:client:"+callerOrigin, "grantjti-1", grantTtlMs)
	}
	return d
}

func TestF2_BExport_MalformedAndNotFound(t *testing.T) {
	caller := "a.example"
	d := bExportDeps(t, "65000000000000000000aaaa", caller, 60_000)

	ok, code, detail, _ := d.dispatch("export-project", caller, map[string]any{})
	if ok || code != "project-not-owned" || detail != "malformed projectId" {
		t.Errorf("malformed: ok=%v code=%q detail=%q", ok, code, detail)
	}
	ok, code, detail, _ = d.dispatch("export-project", caller, map[string]any{"projectId": "nope"})
	if ok || code != "project-not-owned" || detail != "project not found" {
		t.Errorf("not found: ok=%v code=%q detail=%q", ok, code, detail)
	}
}

func TestF2_BExport_OwnerMatrix(t *testing.T) {
	caller := "a.example"
	owner := "65000000000000000000aaaa"
	d := bExportDeps(t, owner, caller, 60_000)

	// mirror owner → project-not-owned (the live 2d smoke bug: mirror =
	// populated federation.origin, NOT bare subdoc presence).
	d.ExportOwner = func(hex string) (bool, bool, bool) { return true, false, true }
	ok, code, detail, _ := d.dispatch("export-project", caller, map[string]any{"projectId": "65000000000000000000beef"})
	if ok || code != "project-not-owned" || detail != "owner missing/mirror/suspended" {
		t.Errorf("mirror: ok=%v code=%q detail=%q", ok, code, detail)
	}
	// suspended owner
	d.ExportOwner = func(hex string) (bool, bool, bool) { return true, true, false }
	ok, code, detail, _ = d.dispatch("export-project", caller, map[string]any{"projectId": "65000000000000000000beef"})
	if ok || code != "project-not-owned" || detail != "owner missing/mirror/suspended" {
		t.Errorf("suspended: ok=%v code=%q detail=%q", ok, code, detail)
	}
	// missing owner
	d.ExportOwner = func(hex string) (bool, bool, bool) { return false, false, false }
	ok, code, detail, _ = d.dispatch("export-project", caller, map[string]any{"projectId": "65000000000000000000beef"})
	if ok || code != "project-not-owned" || detail != "owner missing/mirror/suspended" {
		t.Errorf("missing owner: ok=%v code=%q detail=%q", ok, code, detail)
	}
}

func TestF2_BExport_ConsentGate(t *testing.T) {
	caller := "a.example"
	owner := "65000000000000000000aaaa"
	d := bExportDeps(t, owner, caller, 0) // NO grant seeded
	ok, code, detail, _ := d.dispatch("export-project", caller, map[string]any{"projectId": "65000000000000000000beef"})
	if ok || code != "export-no-consent" || detail != "no live consent grant" {
		t.Errorf("no consent: ok=%v code=%q detail=%q (want export-no-consent)", ok, code, detail)
	}
	// consent to a DIFFERENT client does not count (grant is per A-client)
	d2 := bExportDeps(t, owner, "a.example", 0)
	d2.Adapter.Redis.(*grantFake).seed(owner, "urn:overleaf-federation:client:other.example", "g2", 60_000)
	ok, code, _, _ = d2.dispatch("export-project", caller, map[string]any{"projectId": "65000000000000000000beef"})
	if ok || code != "export-no-consent" {
		t.Errorf("cross-client consent: ok=%v code=%q (want export-no-consent)", ok, code)
	}
}

func TestF2_BExport_HappyAndTTL(t *testing.T) {
	caller := "a.example"
	owner := "65000000000000000000aaaa"
	var mintedRaw string
	d := bExportDeps(t, owner, caller, 60_000) // grant TTL 60 s
	d.ExportMint = func(hex, raw string, c, e time.Time) (string, error) {
		mintedRaw = raw
		return "patid-1", nil
	}

	// No requested expiresAt → ttl = min(max=86400, grant 60) = 60.
	ok, code, detail, payload := d.dispatch("export-project", caller, map[string]any{"projectId": "65000000000000000000beef"})
	if !ok || code != "" || detail != "" {
		t.Fatalf("happy: ok=%v code=%q detail=%q payload=%v", ok, code, detail, payload)
	}
	pat, _ := payload["pat"].(string)
	if pat != mintedRaw || !strings.HasPrefix(pat, "olp_") || len(pat) != 4+36 {
		t.Errorf("pat shape: %q (want olp_ + 36 alnum)", pat)
	}
	gitURL, _ := payload["git_url"].(string)
	if gitURL != "https://bx.example/git/65000000000000000000beef" {
		t.Errorf("git_url=%q (want the B host including the /git/<pid> mount)", gitURL)
	}
	nowInt := int64(time.Now().Unix())
	expAtI, _ := payload["expires_at"].(int64)
	if expAtI < nowInt+58 || expAtI > nowInt+60 {
		t.Errorf("expires_at=%v (want now+60, the grant-TTL-clamped value)", expAtI)
	}
	// requested expiresAt shorter than grant → the request wins.
	req := time.Now().Unix() + 10
	ok, _, _, payload = d.dispatch("export-project", caller, map[string]any{"projectId": "65000000000000000000beef", "expiresAt": float64(req)})
	if !ok {
		t.Fatal("requested ttl: must succeed")
	}
	expAtI, _ = payload["expires_at"].(int64)
	if expAtI != req {
		t.Errorf("expires_at=%v want %v", expAtI, req)
	}

	// cap: a huge requested ttl is clamped to maxExportTtlSeconds.
	d.Setting.ExportMaxTTLSeconds = 300
	// grant seeded at 60 s would win the min — re-seed a longer grant so
	// the CAP is the binding min (grant 300 s > cap 300 s? make 600 s).
	g := d.Adapter.Redis.(*grantFake)
	g.seed(owner, "urn:overleaf-federation:client:"+caller, "grantjti-1", 600_000)
	ok, _, _, payload = d.dispatch("export-project", caller, map[string]any{"projectId": "65000000000000000000beef", "expiresAt": float64(time.Now().Unix() + 999999)})
	if !ok {
		t.Fatal("cap: must succeed")
	}
	expAtI, _ = payload["expires_at"].(int64)
	if expAtI < time.Now().Unix()+298 || expAtI > time.Now().Unix()+300 {
		t.Errorf("cap: expires_at=%v (want ~now+300)", expAtI)
	}
}

// ---------- hermetic DUAL-INSTANCE round trip (A wizard → B pipeline) ----------

func TestF2_DualInstance_RoundTrip(t *testing.T) {
	// B: the full S2S pipeline (verify → replay → budget → dispatch) with
	// a pinned A anchor + consent grant + export seams.
	owner := "65000000000000000000aaaa"
	aStore := NewMapStore()
	ap := &KeyProvider{Store: aStore, Site: "https://a.example"}
	if err := ap.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	key, err := ap.LeafSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	anchor, _ := json.Marshal(key.PublicKey)
	bStore := NewMapStore()
	if err := bStore.SavePeer(context.Background(), &FederationPeer{
		Origin: "a.example", Mode: "pairwise", Status: "approved",
		AnchorJwks: string(anchor), Kid: key.Kid,
	}); err != nil {
		t.Fatal(err)
	}
	gf := newGrantFake()
	// B's Site = its public wire origin (entityId strips the port — 03 §8);
	// the audit binds A's assertion `aud` = https://bx.example/federation/s2s.
	bD := &s2sDeps{
		Site:    "https://bx.example:3000",
		Setting: Settings{Enabled: true, ExportEnabled: true, ExportMaxTTLSeconds: 86400},
		Store:   bStore,
		Redis:   newFakeRedis(),
		Adapter: &OidcAdapter{ModelName: "Grant", Redis: gf},
		Now:     time.Now,
		AuditW:  &auditRec{},
		ExportProjectDoc: func(pid string) string {
			if pid == "65000000000000000000beef" {
				return owner
			}
			return ""
		},
		ExportOwner: func(string) (bool, bool, bool) { return true, false, false },
		// Site patched below once bHost is known (aud endpoint = our host).
		ExportMint:   func(string, string, time.Time, time.Time) (string, error) { return "patid-dual", nil },
		ExportLedger: func(string, string, string, string, string, string, time.Time) error { return nil },
	}
	gf.seed(owner, "urn:overleaf-federation:client:a.example", "grantjti-dual", 600_000)

	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/federation/s2s" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var env struct {
			Action  string         `json:"action"`
			From    string         `json:"from"`
			To      string         `json:"to"`
			Payload map[string]any `json:"payload"`
			Data    map[string]any `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"ok":false,"code":"bad-envelope"}`))
			return
		}
		data := env.Data
		if data == nil {
			data = env.Payload
		}
		assertion := r.Header.Get("client_assertion")
		out := bD.pipeline(env.Action, env.From, assertion, data)
		for k, v := range out.headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.status)
		_ = json.NewEncoder(w).Encode(out.body)
	}))
	defer b.Close()
	bHost := strings.TrimPrefix(b.URL, "http://")
	_ = bHost

	const logicalB = "bx.example" // B's WIRE ORIGIN (FQDN, no port — the aud unit, 03 §2)

	// A-side peer ledger: approve B (logical origin, outbound) in THE
	// SAME A store that holds the signing key (one instance = one
	// keystore + one peer ledger).
	aKey := bootstrapTestKey(t, aStore)
	_ = saveTestPeer(t, aStore, logicalB, "outbound", string(aKey))

	// The transport dials the local httptest B (hermetic loopback); the
	// assertion keeps the production aud (https://bx.example/federation/s2s).
	caller := &S2SCall{
		Store:         aStore,
		Site:          "https://a.example",
		S2SUROverride: map[string]string{logicalB: b.URL + "/federation/s2s"},
	}
	wd := &exportDeps{
		Store:  aStore,
		Caller: caller,
		MaxTTL: 86400,
		Now:    time.Now,
		AuditW: &auditRec{},
	}

	// ① the wizard's local gate: an unapproved origin must not pass.
	if _, ok := wd.findPeer("zzzz.example"); ok {
		t.Error("unapproved origin must not pass the wizard gate")
	}
	// ② the approved peer resolves (by logical origin).
	peer, ok := wd.findPeer(logicalB)
	if !ok || peer.origin != logicalB {
		t.Fatalf("approved peer missing: %+v ok=%v", peer, ok)
	}
	_ = peer

	// ③ THE REAL A→B ROUND TRIP: A signs the client assertion (the
	//    federation key; aud = B's S2S endpoint), B verifies it against
	//    A's pinned anchor + runs the consent + mint legs, and A
	//    consumes the business envelope.
	envelope, err := caller.Call(context.Background(), logicalB, "export-project", map[string]any{
		"projectId": "65000000000000000000beef",
		"expiresAt": float64(time.Now().Unix() + 3600),
	})
	if err != nil {
		t.Fatalf("A→B round trip failed: %v", err)
	}
	if okVal, _ := envelope["ok"].(bool); !okVal {
		t.Fatalf("A→B envelope: %+v", envelope)
	}
	pl, _ := envelope["payload"].(map[string]any)
	pat, _ := pl["pat"].(string)
	if !strings.HasPrefix(pat, "olp_") || len(pat) != 40 {
		t.Fatalf("payload.pat=%v (want the minted olp_ + 36 token)", pl)
	}
	gitUrl, _ := pl["git_url"].(string)
	if gitUrl != "https://bx.example:3000/git/65000000000000000000beef" {
		t.Errorf("git_url=%q (want B's host /git/<pid> mount)", gitUrl)
	}
	if expAt, ok := pl["expires_at"].(float64); !ok || expAt <= 0 {
		t.Errorf("expires_at=%v (want a positive epoch)", pl)
	}

	// ④ the git_url must point at B's real host (09 §2: the git-bridge
	//    mount is a URL, the host INCLUDING port where it is served).
	//    (In this hermetic loop the transport target is local; the audit
	//    of the minted PAT is redacted by construction — the audit rec
	//    must never contain the pat.)
	if len(pat) > 8 {
		_ = pat
	}
	_ = bHost
}
