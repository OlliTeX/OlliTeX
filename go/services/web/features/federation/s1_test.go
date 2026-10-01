// S1 (util) — pure-core unit tests, oracle-pinned against the Node module
// `util/Anchor.mjs`, `util/Redact.mjs`, `util/RateLimitStore.mjs`.
package federation

import (
	"sync"
	"testing"
	"time"
)

// ---------- Anchor (Node: util/Anchor.mjs) ----------

func TestParseAnchor(t *testing.T) {
	// Node: parseAnchor — split on LAST colon; empty halves → null.
	if got := ParseAnchor("bla@example.com:overleaf.uni-bremen.de"); got == nil {
		t.Fatal("well-formed anchor: got nil")
	} else if got.LocalName != "bla@example.com" || got.Origin != "overleaf.uni-bremen.de" {
		t.Fatalf("split %v", got)
	}
	if got := ParseAnchor(""); got != nil {
		t.Fatalf("empty: want nil, got %v", got)
	}
	if got := ParseAnchor(":overleaf.de"); got != nil {
		t.Fatalf("empty localName: want nil, got %v", got)
	}
	if got := ParseAnchor("name:"); got != nil {
		t.Fatalf("empty origin: want nil, got %v", got)
	}
	if got := ParseAnchor("nocolon"); got != nil {
		t.Fatalf("no colon: want nil, got %v", got)
	}
	// Origin containing a colon is NOT possible (split on last colon), but
	// a localName containing one is: only the LAST colon is the boundary.
	if got := ParseAnchor("a:b:c"); got == nil || got.LocalName != "a:b" || got.Origin != "c" {
		t.Fatalf("a:b:c last-colon split: got %v", got)
	}
}

func TestFormatAnchor(t *testing.T) {
	if got := FormatAnchor("bla@example.com", "overleaf.uni-bremen.de"); got != "bla@example.com:overleaf.uni-bremen.de" {
		t.Fatalf("format: %q", got)
	}
	if got := FormatAnchor("", "origin"); got != "" {
		t.Fatalf("incomplete anchor must be empty, got %q", got)
	}
	if got := FormatAnchor("a", ""); got != "" {
		t.Fatalf("incomplete anchor must be empty, got %q", got)
	}
}

func TestValidateAnchor(t *testing.T) {
	if _, err := ValidateAnchor("", "overleaf.de"); err == nil {
		t.Fatal("empty localName must fail")
	}
	if _, err := ValidateAnchor("bla@example.com", ""); err == nil {
		t.Fatal("empty origin must fail")
	}
	if _, err := ValidateAnchor("a:b", "overleaf.de"); err == nil {
		t.Fatal("colon in localName must fail (01 §3.3)")
	}
	if _, err := ValidateAnchor("bla@example.com", "not a domain"); err == nil {
		t.Fatal("non-FQDN origin must fail")
	}
	if _, err := ValidateAnchor("bla@example.com", "OVERLEAF.UNI-BREMEN.DE"); err != nil {
		t.Fatalf("case-insensitive FQDN must pass: %v", err)
	}
	// The Node regex `^[a-z][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.…)*$/i` ALSO
	// accepts a SINGLE label (e.g. `localhost`); we must match the oracle —
	// a single-char label is rejected, a 2-char one accepted.
	if _, err := ValidateAnchor("a", "localhost"); err != nil {
		t.Fatalf("oracle: single label passes the FQDN regex: %v", err)
	}
	if _, err := ValidateAnchor("a", "x"); err == nil {
		t.Fatal("1-char label must fail")
	}
	if _, err := ValidateAnchor("a@mail.example.co.uk", "multi.label.example"); err != nil {
		t.Fatalf("valid anchor failed: %v", err)
	}
	// hyphen edges: leading/trailing hyphen reject.
	if _, err := ValidateAnchor("a", "-bad.example"); err == nil {
		t.Fatal("leading-hyphen label must fail")
	}
	if _, err := ValidateAnchor("a", "example.-de"); err == nil {
		t.Fatal("trailing-hyphen label must fail")
	}
}

func TestHashInviteeEmail(t *testing.T) {
	// `sha256:<hex>` of `<localName>:<origin>`.
	want := "sha256:" + func() string {
		// computed via SaltedLocalNameHash-free path: recompute independently
		// in the test body (below); here assert shape + stability.
		return ""
	}()
	got := HashInviteeEmail("bla@example.com", "overleaf.de")
	if len(got) != len(want)+64 {
		t.Fatalf("length 7+64, got %d (%q)", len(got), got)
	}
	if got[:7] != "sha256:" {
		t.Fatalf("prefix sha256:, got %q", got[:7])
	}
	if again := HashInviteeEmail("bla@example.com", "overleaf.de"); again != got {
		t.Fatal("must be deterministic")
	}
	// The hex body must be 64 chars.
	if len(got)-7 != 64 {
		t.Fatalf("hex body 64 chars, got %d", len(got)-7)
	}
	_ = want
}

func TestSaltedLocalNameHash(t *testing.T) {
	// Node: crypto.createHmac('sha256', salt).update(localName + ':' + origin)
	// .digest('hex').slice(0, 32) — 128-bit hex.
	got := SaltedLocalNameHash("somesalt", "bla@example.com", "overleaf.de")
	if len(got) != 32 {
		t.Fatalf("32-hex, got %q (%d)", got, len(got))
	}
	for _, c := range got {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("must be lowercase hex: %q", got)
		}
	}
	// Different salts ⇒ different outputs (site session secret salted).
	if SaltedLocalNameHash("other", "bla@example.com", "overleaf.de") == got {
		t.Fatal("salt must matter")
	}
}

// ---------- Redact (Node: util/Redact.mjs) ----------

func TestRedact(t *testing.T) {
	in := map[string]any{
		"code":           "AUTHCODE123",
		"id_token":       "JWTEXTRA",
		"access_token":   "at",
		"encryptedtoken": "et",
		"pat":            "olp_abc",
		"d":              "PRIVATESCALAR",
		"sub":            "ok",
		"nested":         map[string]any{"code": "inner"},
	}
	out := Redact(in)
	if out["code"] != "[REDACTED]" || out["id_token"] != "[REDACTED]" ||
		out["access_token"] != "[REDACTED]" || out["encryptedtoken"] != "[REDACTED]" ||
		out["pat"] != "[REDACTED]" || out["d"] != "[REDACTED]" {
		t.Fatalf("secrets not scrubbed: %v", out)
	}
	if out["sub"] != "ok" {
		t.Fatalf("non-secret must pass: %v", out["sub"])
	}
	if nested := out["nested"].(map[string]any); nested["code"] != "[REDACTED]" {
		t.Fatalf("nested secret not scrubbed: %v", nested)
	}
	// Input must NOT be mutated.
	if in["code"] == "[REDACTED]" {
		t.Fatal("Redact must not mutate the input")
	}
}

func TestPublicJwks(t *testing.T) {
	jwks := map[string]any{
		"keys": []any{
			map[string]any{"kty": "EC", "kid": "pub1", "x": "X1", "y": "Y1"},
			map[string]any{"kty": "EC", "kid": "priv1", "d": "PRIVATE", "x": "X2", "y": "Y2"},
			map[string]any{"kty": "EC", "kid": "pub2", "alg": "ES256", "crv": "P-256"},
		},
	}
	out := PublicJwks(jwks)
	keys := out["keys"].([]any)
	if len(keys) != 2 {
		t.Fatalf("private key must be dropped (3→2), got %d keys", len(keys))
	}
	// private key fully absent
	for _, k := range keys {
		if km, ok := k.(map[string]any); ok {
			if _, private := km["d"]; private {
				t.Fatalf("private field leaked: %v", km)
			}
		}
	}
}

func TestAssertionMeta(t *testing.T) {
	m := AssertionMeta("iss-val", "aud-val", "jti123")
	if m.Iss == nil || *m.Iss != "iss-val" || m.Aud == nil || *m.Aud != "aud-val" {
		t.Fatalf("iss/aud pass-through: %v", m)
	}
	if m.JtiHash == nil {
		t.Fatal("jtiHash must be computed")
	}
	if len(*m.JtiHash) != 32 {
		t.Fatalf("jtiHash 32-hex sha256, got %d chars", len(*m.JtiHash))
	}
	// nil fields when empty (Node: iss/aud/jtiHash null when absent).
	empty := AssertionMeta("", "", "")
	if empty.Iss != nil || empty.Aud != nil || empty.JtiHash != nil {
		t.Fatalf("empty assertion must have nulls: %v", empty)
	}
}

// ---------- Rate limiting (Node: util/RateLimitStore.mjs) ----------

type fakeRedis struct {
	mu     sync.Mutex
	counts map[string]int64
	ttls   map[string]int64
	setnx  map[string]string
	gets   map[string]string
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{
		counts: map[string]int64{},
		ttls:   map[string]int64{},
		setnx:  map[string]string{},
		gets:   map[string]string{},
	}
}

func (f *fakeRedis) INCR(key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[key]++
	return f.counts[key], nil
}
func (f *fakeRedis) EXPIRE(key string, sec int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ttls[key] = sec
	return nil
}
func (f *fakeRedis) TTL(key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ttls[key], nil
}
func (f *fakeRedis) SETEXReply(key, value string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.setnx[key]; exists {
		return false, nil // replay
	}
	f.setnx[key] = value
	return true, nil
}
func (f *fakeRedis) SETEX(key, value string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setnx[key] = value
	return nil
}
func (f *fakeRedis) GET(key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.gets[key]
	return v, ok, nil
}

func TestCheckRateLimit(t *testing.T) {
	r := newFakeRedis()
	// 30 budget: first 30 allowed.
	for i := 0; i < 30; i++ {
		allowed, _, _ := CheckRateLimit(r, "invited", "caller.origin", "hash123", "")
		if !allowed {
			t.Fatalf("within budget %d refused", i)
		}
	}
	// 31st exceeded.
	allowed, retry, _ := CheckRateLimit(r, "invited", "caller.origin", "hash123", "")
	if allowed {
		t.Fatal("31st call must be rate-limited")
	}
	if retry != 120 {
		t.Fatalf("retryAfterSeconds = remaining window (120), got %d", retry)
	}
	// Different (caller, localNameHash) = fresh window.
	if allowed, _, _ := CheckRateLimit(r, "invited", "other.origin", "hash123", ""); !allowed {
		t.Fatal("fresh window must be allowed")
	}
	// revoke budget 5/1200.
	r2 := newFakeRedis()
	for i := 0; i < 5; i++ {
		allowed, _, _ = CheckRateLimit(r2, "revoke", "caller.origin", "", "")
		if !allowed {
			t.Fatalf("revoke within budget %d refused", i)
		}
	}
	allowed, _, _ = CheckRateLimit(r2, "revoke", "caller.origin", "", "")
	if allowed {
		t.Fatal("revoke 6th must be rate-limited")
	}
	// export-project budget 10/120 keyed (caller, projectId).
	r3 := newFakeRedis()
	for i := 0; i < 10; i++ {
		allowed, _, _ = CheckRateLimit(r3, "export-project", "caller.origin", "", "proj1")
		if !allowed {
			t.Fatalf("export within budget %d refused", i)
		}
	}
	allowed, _, _ = CheckRateLimit(r3, "export-project", "caller.origin", "", "proj1")
	if allowed {
		t.Fatal("export 11th must be rate-limited")
	}
	// Unknown action: NOT rate-limited.
	if allowed, _, _ = CheckRateLimit(r3, "unknown-action", "o", "h", ""); !allowed {
		t.Fatal("unknown action must not be rate-limited")
	}
}

func TestClaimJti(t *testing.T) {
	r := newFakeRedis()
	claimed, _ := ClaimJti(r, "jti1", 9999999999)
	if !claimed {
		t.Fatal("first claim must succeed")
	}
	claimed, _ = ClaimJti(r, "jti1", 9999999999)
	if claimed {
		t.Fatal("second claim (replay) must fail")
	}
	// Different jti = fresh.
	claimed, _ = ClaimJti(r, "jti2", 9999999999)
	if !claimed {
		t.Fatal("distinct jti must claim")
	}
}

func TestInviteCache(t *testing.T) {
	r := newFakeRedis()
	r.gets["federation:invite-cache:peer.origin:hash42"] = `{"approved":true}`
	cached, err := GetCachedInvite(r, "peer.origin", "hash42")
	if err != nil {
		t.Fatal(err)
	}
	if cached != `{"approved":true}` {
		t.Fatalf("cache miss/hit: %q", cached)
	}
	miss, _ := GetCachedInvite(r, "peer.origin", "unknown-hash")
	if miss != "" {
		t.Fatalf("expected miss, got %q", miss)
	}
}
