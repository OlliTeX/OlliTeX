// S4a tests: the OIDC provider 7-method adapter (oracle:
// RedisOidcProviderAdapter.mjs). Fake redis with real TTL simulation.
package federation

import (
	"fmt"
	"testing"
	"time"
)

// fakeOidcRedis — in-memory Redis subset (string + SET) with PTTL
// simulation so expired-by-TTL members are skipped (05 §8.2).
type fakeOidcRedis struct {
	keys    map[string]string
	expires map[string]int64 // unix-sec expiry (absent = no expiry)
	sets    map[string]map[string]struct{}
	now     func() int64
}

func newFakeOidcRedis() *fakeOidcRedis {
	return &fakeOidcRedis{
		keys:    map[string]string{},
		expires: map[string]int64{},
		sets:    map[string]map[string]struct{}{},
		now:     func() int64 { return time.Now().Unix() },
	}
}

func (r *fakeOidcRedis) alive(key string) bool {
	if exp, ok := r.expires[key]; ok && exp != 0 {
		if r.now() >= exp {
			delete(r.keys, key)
			delete(r.expires, key)
			delete(r.sets[key], "")
			return false
		}
	}
	_, ok := r.keys[key]
	return ok
}

func (r *fakeOidcRedis) GET(key string) (string, bool, error) {
	if v, ok := r.keys[key]; ok && r.alive(key) {
		return v, true, nil
	}
	return "", false, nil
}

func (r *fakeOidcRedis) SETEX(key, value string, sec int64) error {
	r.keys[key] = value
	if sec > 0 {
		r.expires[key] = r.now() + sec
	}
	return nil
}

func (r *fakeOidcRedis) SETPX(key, value string, ms int64) error {
	r.keys[key] = value
	if ms > 0 {
		r.expires[key] = r.now() + int64(ms/1000)
	}
	return nil
}

func (r *fakeOidcRedis) SETNoTTL(key, value string) error {
	r.keys[key] = value
	delete(r.expires, key)
	return nil
}

func (r *fakeOidcRedis) DEL(key string) error {
	delete(r.keys, key)
	delete(r.expires, key)
	return nil
}

func (r *fakeOidcRedis) SADD(key string, members ...string) error {
	if r.sets[key] == nil {
		r.sets[key] = map[string]struct{}{}
	}
	for _, m := range members {
		r.sets[key][m] = struct{}{}
	}
	return nil
}

func (r *fakeOidcRedis) SREM(key string, members ...string) error {
	for _, m := range members {
		delete(r.sets[key], m)
	}
	return nil
}

func (r *fakeOidcRedis) SMEMBERS(key string) ([]string, error) {
	var out []string
	for m := range r.sets[key] {
		out = append(out, m)
	}
	return out, nil
}

func (r *fakeOidcRedis) SCARD(key string) (int64, error) {
	return int64(len(r.sets[key])), nil
}

func (r *fakeOidcRedis) PTTL(key string) (int64, error) {
	if v, ok := r.keys[key]; ok {
		_ = v
		if exp, ok := r.expires[key]; ok && exp != 0 {
			if r.now() >= exp {
				return -2, nil
			}
			return (r.expireMs(key)), nil
		}
		return -1, nil
	}
	return -2, nil
}

func (r *fakeOidcRedis) expireMs(key string) int64 {
	exp, _ := r.expires[key]
	remaining := (exp - r.now()) * 1000
	if remaining < 1 {
		return 1
	}
	return remaining
}

func (r *fakeOidcRedis) PING() error { return nil }

// --- the adapter (Find/FindByUID/FindByUserCode/Upsert/RevokeByGrantId/
// Destroy/Consume), 05 §8.2 ---

func TestOidcAdapterUpsertFindRoundTrip(t *testing.T) {
	r := newFakeOidcRedis()
	a := &OidcAdapter{ModelName: "AuthorizationCode", Redis: r}
	ttl := int64(120)
	payload := map[string]any{
		"jti":      "code-1",
		"clientId": "urn:overleaf-federation:client:b.example.org",
		"grantId":  "grant-1",
		"uid":      "sess-1",
	}
	if err := a.Upsert("code-1", payload, &ttl); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := a.Find("code-1")
	if err != nil || got["clientId"] != "urn:overleaf-federation:client:b.example.org" {
		t.Fatalf("find: %v %v", err, got)
	}
	// sub-index (session.js contract).
	if sid, ok, err := r.GET("federation:oidc:sub:sess-1"); err != nil || !ok || sid != "code-1" {
		t.Fatalf("sub-index: %q %v %v", sid, ok, err)
	}
	// grantable→grant SET index (revokeByGrantId cascade source).
	members, _ := r.SMEMBERS("federation:oidc:grant:grant-1")
	if len(members) != 1 || members[0] != "federation:oidc:AuthorizationCode:code-1" {
		t.Fatalf("grant SET: %v", members)
	}
	// GRANTABLE→client sweep SET (killOutstandingCodes).
	clientSet, _ := r.SMEMBERS("federation:oidc:client:urn:overleaf-federation:client:b.example.org")
	if len(clientSet) != 1 || clientSet[0] != "federation:oidc:AuthorizationCode:code-1" {
		t.Fatalf("client SET: %v", clientSet)
	}
	// session doc (no grantId) does NOT enter the client sweep (06 §178
	// "not over-cross" via sub key only).
	sess := &OidcAdapter{ModelName: "Session", Redis: r}
	uidPayload := map[string]any{"jti": "sess-1", "uid": "sess-1"}
	if err := sess.Upsert("sess-1", uidPayload, &ttl); err != nil {
		t.Fatal(err)
	}
	if m, _ := r.SMEMBERS("federation:oidc:client:nonexistent"); len(m) != 0 {
		t.Fatalf("session doc must not enter client sweep: %v", m)
	}
}

func TestOidcAdapterConsumeSetsFlagNotDeleted(t *testing.T) {
	r := newFakeOidcRedis()
	a := &OidcAdapter{ModelName: "AuthorizationCode", Redis: r}
	ttl := int64(120)
	if err := a.Upsert("code-1", map[string]any{"jti": "code-1", "clientId": "urn:x", "grantId": "g1"}, &ttl); err != nil {
		t.Fatal(err)
	}
	if err := a.Consume("code-1"); err != nil {
		t.Fatal(err)
	}
	got, err := a.Find("code-1")
	if err != nil {
		t.Fatal(err)
	}
	if got["consumed"] == nil {
		t.Fatal("consume must set payload.consumed (epoch sec), NOT delete (revocation cascade, 02 §7.5)")
	}
}

func TestOidcAdapterRevokeByGrantIdCascade(t *testing.T) {
	r := newFakeOidcRedis()
	a := &OidcAdapter{ModelName: "AuthorizationCode", Redis: r}
	ttl := int64(120)
	for _, id := range []string{"c1", "c2"} {
		if err := a.Upsert(id, map[string]any{"jti": id, "clientId": "urn:x", "grantId": "g1", "uid": id}, &ttl); err != nil {
			t.Fatal(err)
		}
	}
	// a Grant doc with the same grantId (the consent record) — must NOT
	// be swept (06 §174: revocation affects tokens, not the consent row).
	gr := &OidcAdapter{ModelName: "Grant", Redis: r}
	if err := gr.Upsert("g1", map[string]any{"jti": "g1", "accountId": "acc-1", "clientId": "urn:x"}, &ttl); err != nil {
		t.Fatal(err)
	}
	if err := a.RevokeByGrantId("g1"); err != nil {
		t.Fatal(err)
	}
	// token docs gone, sub-indices gone.
	if _, ok, _ := r.GET("federation:oidc:AuthorizationCode:c1"); ok {
		t.Fatal("c1 must be swept by revokeByGrantId")
	}
	if _, ok, _ := r.GET("federation:oidc:sub:c1"); ok {
		t.Fatal("sub:c1 must be swept (cascade)")
	}
	// the consent Grant survives (revocation ≠ consent revocation, 06 §174).
	g, _ := gr.Find("g1")
	if g == nil {
		t.Fatal("consent Grant must survive token revocation (06 §174)")
	}
}

func TestOidcAdapterDestroyTrimsSecondaryIndexes(t *testing.T) {
	r := newFakeOidcRedis()
	gr := &OidcAdapter{ModelName: "Grant", Redis: r}
	for i := 0; i < 2; i++ {
		if err := gr.Upsert(fmt.Sprintf("g%d", i+1), map[string]any{"jti": fmt.Sprintf("g%d", i+1), "accountId": "acc", "clientId": "urn:x"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// account SET holds both grant doc keys.
	if n, _ := r.SCARD("federation:oidc:account:acc:urn:x"); n != 2 {
		t.Fatalf("account SET card: %d", n)
	}
	// destroy first (06 §175 sweep): doc + one SET member gone, SET kept.
	if err := gr.Destroy("g1"); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.SCARD("federation:oidc:account:acc:urn:x"); n != 1 {
		t.Fatalf("account SET after destroy: %d (want 1)", n)
	}
	// destroy second: SET now empty → deleted (05 §8.2 SCARD==0 → DEL).
	if err := gr.Destroy("g2"); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.SCARD("federation:oidc:account:acc:urn:x"); n != 0 {
		t.Fatalf("empty account SET must be DEL-ed: %d", n)
	}
}

func TestOidcAdapterFindByAccountAndClientSkipsExpired(t *testing.T) {
	r := newFakeOidcRedis()
	gr := &OidcAdapter{ModelName: "Grant", Redis: r}
	// a live grant (long TTL).
	if err := gr.Upsert("live", map[string]any{"jti": "live", "accountId": "acc", "clientId": "urn:x"}, ptr6(3600)); err != nil {
		t.Fatal(err)
	}
	// a short-TTL grant — will be TTL-expired below (09 §2.1 "stale
	// members are skipped", findByAccountAndClient pttl >= 0 guard).
	if err := r.SADD("federation:oidc:account:acc:urn:x", "federation:oidc:Grant:stale"); err != nil {
		t.Fatal(err)
	}
	if err := r.SETEX("federation:oidc:Grant:stale", `{"jti":"stale","accountId":"acc","clientId":"urn:x"}`, 1); err != nil {
		t.Fatal(err)
	}
	// advance the fake clock past the 1s TTL.
	r.now = func() int64 { now := time.Now().Unix(); return now + 2 }
	got, err := FindByAccountAndClient(r, "acc", "urn:x")
	if err != nil {
		t.Fatal(err)
	}
	// the stale member must be skipped (pttl < 0), the live grant wins
	// deterministically.
	if got != "live" {
		t.Fatalf("first LIVE grant must win (09 §2.1); expired member skipped: got %q", got)
	}
}

func ptr6(v int64) *int64 { return &v }

// TestRevokeClientCodesKillOutstanding — 04 §5 `killOutstandingCodes`
// (S5 S2S admin revoke calls this): token docs for the client are swept,
// the consent Grant for that client is NOT (06 §174), second client's
// token survives (06 §178 not-over-cross), idempotent on second run.
func TestRevokeClientCodesKillOutstanding(t *testing.T) {
	r := newFakeOidcRedis()
	ttl := int64(120)
	ac := &OidcAdapter{ModelName: "AuthorizationCode", Redis: r}
	if err := ac.Upsert("c1", map[string]any{"jti": "c1", "clientId": "urn:x", "grantId": "g1"}, &ttl); err != nil {
		t.Fatal(err)
	}
	at := &OidcAdapter{ModelName: "AccessToken", Redis: r}
	if err := at.Upsert("t1", map[string]any{"jti": "t1", "clientId": "urn:x", "grantId": "g1"}, &ttl); err != nil {
		t.Fatal(err)
	}
	if err := at.Upsert("t-other", map[string]any{"jti": "t-other", "clientId": "urn:other", "grantId": "g2"}, &ttl); err != nil {
		t.Fatal(err)
	}
	gr := &OidcAdapter{ModelName: "Grant", Redis: r}
	if err := gr.Upsert("g1", map[string]any{"jti": "g1", "accountId": "acc", "clientId": "urn:x"}, &ttl); err != nil {
		t.Fatal(err)
	}
	if members, _ := r.SMEMBERS("federation:oidc:client:urn:x"); len(members) != 2 {
		t.Fatalf("client index: %d (want 2 token docs)", len(members))
	}
	revoked, err := ac.RevokeClientCodes("urn:x")
	if err != nil {
		t.Fatal(err)
	}
	if revoked != 2 {
		t.Fatalf("revoked: %d (want 2)", revoked)
	}
	if _, ok, _ := r.GET("federation:oidc:AuthorizationCode:c1"); ok {
		t.Fatal("code sweep: c1 must be destroyed")
	}
	if _, ok, _ := r.GET("federation:oidc:AccessToken:t1"); ok {
		t.Fatal("token sweep: t1 must be destroyed")
	}
	if g, _ := gr.Find("g1"); g == nil {
		t.Fatal("consent Grant must survive (06 §174)")
	}
	if _, ok, _ := r.GET("federation:oidc:AccessToken:t-other"); !ok {
		t.Fatal("other client's token must survive (not over-cross, 06 §178)")
	}
	if n, err := ac.RevokeClientCodes("urn:x"); err != nil || n != 0 {
		t.Fatalf("second sweep: %d %v (want 0)", n, err)
	}
}

// TestOidcAdapterNoTTLUpsertStaysForever — nil expires → no-TTL write
// (fake keeps it forever; production always passes a model ttl).
func TestOidcAdapterNoTTLUpsertStaysForever(t *testing.T) {
	r := newFakeOidcRedis()
	a := &OidcAdapter{ModelName: "Grant", Redis: r}
	if err := a.Upsert("g1", map[string]any{"jti": "g1", "accountId": "a", "clientId": "urn:x"}, nil); err != nil {
		t.Fatal(err)
	}
	g, _ := a.Find("g1")
	if g == nil {
		t.Fatal("no-TTL upsert must persist (fake)")
	}
}
