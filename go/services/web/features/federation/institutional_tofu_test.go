// Institutional TOFU — 02 §4 chain resolve on the peer PIN.
//
// The peer row in institutional mode carries the explicit-registration
// result (PeerRegistration: child anchor JWK/kid + trust-chain expiry
// 02 §4). VerifyS2sClientAssertion resolves the trusted anchor from THAT
// registration (never the direct pin), gates on TrustChainExpiresAt, and
// rejects with honest machine codes (chain-expired / anchor-missing).
// Pairwise behaviour (pinning IS establishment) is unchanged and pinned
// here as a regression.
package federation

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// instPair — A (signer, a.example) + B (verifier, b.example) with B's peer
// row in the given mode; the trusted key on both sides is A's leaf public
// key (pairwise: direct pin; institutional: registered child anchor).
func instPair(t *testing.T, mode string, reg *PeerRegistration) (assertion, from string, bStore *MapStore) {
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
	pubJSON, err := json.Marshal(key.PublicKey)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}

	bStore = NewMapStore()
	peer := &FederationPeer{
		Origin: "a.example",
		Status: "approved",
		Mode:   mode,
		Kid:    key.Kid,
	}
	switch mode {
	case "pairwise":
		peer.AnchorJwks = string(pubJSON)
	case "institutional":
		if reg == nil {
			// no registration — the anchor-missing leg
		} else {
			reg.ChildAnchorJwks = string(pubJSON)
			reg.ChildAnchorKid = key.Kid
			peer.Registration = reg
		}
	}
	if err := bStore.SavePeer(context.Background(), peer); err != nil {
		t.Fatalf("save peer: %v", err)
	}
	req, err := ap.BuildS2sRequest("b.example", "invited", map[string]any{"projectId": "650000000000000000dead"})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return req.Headers["client_assertion"], "a.example", bStore
}

func instDeps(bStore *MapStore) *s2sDeps {
	return &s2sDeps{
		Setting: loadSettings(),
		Site:    "https://b.example:3000",
		Salt:    "test-salt",
		Store:   bStore,
		Redis:   newFakeRedis(),
		Now:     time.Now,
		AuditW:  &auditRec{},
	}
}

// T1 — institutional: the registered child anchor is the trusted key;
// the assertion verifies and dispatches (soft preview), and the caller
// is stamped AnchorSource=institutional.
func TestInstitutional_TOFU_ChainResolve(t *testing.T) {
	now := time.Now().Unix()
	assertion, from, bStore := instPair(t, "institutional", &PeerRegistration{
		ExpiresAt:           now + 86400,
		TrustChainExpiresAt: now + 30*24*3600, // 02 §4: chain valid for 30 days
	})
	out := instDeps(bStore).pipeline("invited", from, assertion, map[string]any{
		"projectId": "650000000000000000dead",
	})
	if out.status != http.StatusOK {
		t.Fatalf("institutional chain live: want 200, got %d body=%v", out.status, out.body)
	}
	if out.body["ok"] != true {
		t.Errorf("want soft ok:true preview, got %v", out.body)
	}
}

// T1b — the VerifiedCaller carries the anchor source so downstream
// audit/grant code can distinguish HOW trust was established.
func TestInstitutional_AnchorSourceStamped(t *testing.T) {
	now := time.Now().Unix()
	assertion, from, bStore := instPair(t, "institutional", &PeerRegistration{
		ExpiresAt:           now + 86400,
		TrustChainExpiresAt: now + 3600,
	})
	kp := &KeyProvider{Store: bStore, Site: "https://b.example:3000"}
	caller, code, detail, err := kp.VerifyS2sClientAssertion(assertion, from)
	if err != nil {
		t.Fatalf("verify: %v (%s %s)", err, code, detail)
	}
	if caller.AnchorSource != "institutional" {
		t.Fatalf("AnchorSource = %q, want institutional", caller.AnchorSource)
	}
}

// T2 — chain expired: registration is valid, the chain is not → the
// LOCKED honest code `chain-expired` (not bad-signature, not 200).
func TestInstitutional_ChainExpired(t *testing.T) {
	now := time.Now().Unix()
	assertion, from, bStore := instPair(t, "institutional", &PeerRegistration{
		ExpiresAt:           now + 86400,
		TrustChainExpiresAt: now - 120, // the chain lapsed 2 minutes ago
	})
	out := instDeps(bStore).pipeline("invited", from, assertion, map[string]any{
		"projectId": "650000000000000000dead",
	})
	if out.status != http.StatusUnauthorized {
		t.Fatalf("expired chain: want 401, got %d body=%v", out.status, out.body)
	}
	if out.body["code"] != "chain-expired" {
		t.Fatalf("want code chain-expired, got %v (body=%v)", out.body["code"], out.body)
	}
}

// T3 — institutional peer without a usable registration: honest
// `anchor-missing` (no fallback to the direct pin — trust is only what
// the registration established, 02 §4).
func TestInstitutional_NoRegistration(t *testing.T) {
	assertion, from, bStore := instPair(t, "institutional", nil)
	out := instDeps(bStore).pipeline("invited", from, assertion, map[string]any{
		"projectId": "650000000000000000dead",
	})
	if out.status != http.StatusUnauthorized {
		t.Fatalf("no registration: want 401, got %d body=%v", out.status, out.body)
	}
	if out.body["code"] != "anchor-missing" {
		t.Fatalf("want code anchor-missing, got %v (body=%v)", out.body["code"], out.body)
	}
}

// T4 — institutional registration for a DIFFERENT key: the assertion
// signed by A's leaf does not match the registered child anchor →
// unknown-kid (the pre-check rejects before signature).
func TestInstitutional_WrongChildAnchor(t *testing.T) {
	now := time.Now().Unix()
	otherPub, otherPriv, err := GenerateES256()
	if err != nil {
		t.Fatal(err)
	}
	_ = otherPriv
	otherJSON, err := json.Marshal(otherPub)
	if err != nil {
		t.Fatal(err)
	}
	assertion, from, bStore := instPair(t, "institutional", &PeerRegistration{
		ExpiresAt:           now + 86400,
		TrustChainExpiresAt: now + 3600,
		ChildAnchorJwks:     string(otherJSON), // a key that is NOT A's
		ChildAnchorKid:      otherPub.Kid,
	})
	// instPair overwrote the child anchor with A's key — re-set the
	// "other" anchor to test the mismatch leg honestly.
	peer, err := bStore.PeerByOrigin(context.Background(), from)
	if err != nil || peer == nil {
		t.Fatal("peer not stored")
	}
	peer.Registration.ChildAnchorJwks = string(otherJSON)
	peer.Registration.ChildAnchorKid = otherPub.Kid
	if err := bStore.SavePeer(context.Background(), peer); err != nil {
		t.Fatal(err)
	}
	out := instDeps(bStore).pipeline("invited", from, assertion, map[string]any{
		"projectId": "650000000000000000dead",
	})
	if out.status != http.StatusUnauthorized {
		t.Fatalf("wrong child anchor: want 401, got %d body=%v", out.status, out.body)
	}
	if out.body["code"] != "unknown-kid" {
		t.Fatalf("want code unknown-kid, got %v (body=%v)", out.body["code"], out.body)
	}
}

// T5 — pairwise regression: pinning IS establishment — the direct pin
// verifies, and the caller is stamped AnchorSource=pairwise.
func TestPairwise_PinningIsEstablishment(t *testing.T) {
	assertion, from, bStore := instPair(t, "pairwise", nil)
	kp := &KeyProvider{Store: bStore, Site: "https://b.example:3000"}
	caller, code, detail, err := kp.VerifyS2sClientAssertion(assertion, from)
	if err != nil {
		t.Fatalf("pairwise verify: %v (%s %s)", err, code, detail)
	}
	if caller.AnchorSource != "pairwise" {
		t.Fatalf("AnchorSource = %q, want pairwise", caller.AnchorSource)
	}
	// the pipeline path too (dispatch + honest preview).
	out := instDeps(bStore).pipeline("invited", from, assertion, map[string]any{
		"projectId": "650000000000000000dead",
	})
	if out.status != http.StatusOK {
		t.Fatalf("pairwise pipeline: want 200, got %d body=%v", out.status, out.body)
	}
}
