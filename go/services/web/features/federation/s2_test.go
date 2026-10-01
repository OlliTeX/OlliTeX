// S2 (models) — MapStore unit tests, oracle-pinned against the Node
// Mongoose schemas (module-local, 04 §1–§4).
package federation

import (
	"context"
	"testing"
	"time"
)

func TestKeyStateValidation(t *testing.T) {
	base := FederationKey{Purpose: "federation", Kid: "k1", State: "published"}
	if err := (&base).validateStates(); err != nil {
		t.Fatalf("published must be valid: %v", err)
	}
	for _, st := range []string{"published", "active", "retiring", "revoked"} {
		k := FederationKey{Purpose: "federation", Kid: "k", State: st}
		if err := k.validateStates(); err != nil {
			t.Fatalf("state %q must be valid: %v", st, err)
		}
	}
	bad := FederationKey{Purpose: "federation", Kid: "k", State: "bogus"}
	if err := bad.validateStates(); err == nil {
		t.Fatal("bogus state must fail validation")
	}
}

func TestMapStoreKeys(t *testing.T) {
	ctx := context.Background()
	s := NewMapStore()
	// bootstrap: one published federation key.
	now := time.Now()
	if err := s.SaveKey(ctx, &FederationKey{Purpose: "federation", Kid: "k1", State: "published", PublishedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	got, err := s.KeyByPurposeAndKid(ctx, "federation", "k1")
	if err != nil || got.Kid != "k1" {
		t.Fatalf("get key: %v %v", got, err)
	}
	// ActiveKey miss (published, not active).
	if _, err := s.ActiveKey(ctx, "federation"); !IsNotFound(err) {
		t.Fatalf("ActiveKey miss must ErrNotFound, got %v", err)
	}
	// promote to active.
	if err := s.SaveKey(ctx, &FederationKey{Purpose: "federation", Kid: "k1", State: "active"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ActiveKey(ctx, "federation"); err != nil {
		t.Fatalf("ActiveKey hit: %v", err)
	}
	// rotate: k1→retiring, k2→active (two writes, 02 §5 grace window).
	if err := s.SaveKey(ctx, &FederationKey{Purpose: "federation", Kid: "k2", State: "published"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RotateKey(ctx, "k1", "k2", "federation", now); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	k1, _ := s.KeyByPurposeAndKid(ctx, "federation", "k1")
	k2, _ := s.KeyByPurposeAndKid(ctx, "federation", "k2")
	if k1.State != "retiring" || k2.State != "active" {
		t.Fatalf("rotation states: k1=%v k2=%v (want retiring/active)", k1.State, k2.State)
	}
	if got, _ := s.ActiveKey(ctx, "federation"); got.Kid != "k2" {
		t.Fatalf("active after rotate must be k2, got %v", got.Kid)
	}
	if retiring, _ := s.RetiringKeys(ctx, "federation"); len(retiring) != 1 || retiring[0].Kid != "k1" {
		t.Fatalf("retiring keys: %v", retiring)
	}
	// purpose isolation (federation vs oidc, 02 §5 table).
	if err := s.SaveKey(ctx, &FederationKey{Purpose: "oidc", Kid: "o1", State: "active"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ActiveKey(ctx, "oidc"); got.Kid != "o1" {
		t.Fatalf("oidc active: %v", got.Kid)
	}
	if got, _ := s.ActiveKey(ctx, "federation"); got.Kid != "k2" {
		t.Fatalf("federation active must stay k2, got %v", got.Kid)
	}
}

func TestMapStorePeerDirection(t *testing.T) {
	ctx := context.Background()
	s := NewMapStore()
	now := time.Now()
	// inbound, outbound, both-peer rows.
	if err := s.SavePeer(ctx, &FederationPeer{Origin: "in.ing", Status: "approved", Direction: "inbound", FederatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePeer(ctx, &FederationPeer{Origin: "out.ing", Status: "approved", Direction: "outbound", FederatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePeer(ctx, &FederationPeer{Origin: "both.ing", Status: "approved", Direction: "both", FederatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePeer(ctx, &FederationPeer{Origin: "denied.ing", Status: "pending", Direction: "both", FederatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// inbound query sees inbound + both (approved only).
	got, _ := s.ApprovedPeers(ctx, PeerDirectionInbound)
	if len(got) != 2 {
		t.Fatalf("inbound query: want 2 (inbound+both), got %d", len(got))
	}
	// outbound query sees outbound + both.
	got, _ = s.ApprovedPeers(ctx, PeerDirectionOutbound)
	if len(got) != 2 {
		t.Fatalf("outbound query: want 2 (outbound+both), got %d", len(got))
	}
	// "both" query = any approved direction.
	got, _ = s.ApprovedPeers(ctx, PeerDirectionBoth)
	if len(got) != 3 {
		t.Fatalf("both query: want 3 approved rows, got %d", len(got))
	}
	// the pending row must NOT appear under any direction.
	for _, d := range []PeerDirection{PeerDirectionInbound, PeerDirectionOutbound, PeerDirectionBoth} {
		got, _ = s.ApprovedPeers(ctx, d)
		for _, p := range got {
			if p.Origin == "denied.ing" {
				t.Fatalf("pending peer leaked under %v", d)
			}
		}
	}
}

func TestMapStoreRevokeKillCodes(t *testing.T) {
	ctx := context.Background()
	s := NewMapStore()
	now := time.Now()
	if err := s.SavePeer(ctx, &FederationPeer{Origin: "a.ing", Status: "approved", Direction: "both", FederatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokePeer(ctx, "a.ing", "admin", "2026-01-01", true); err != nil {
		t.Fatal(err)
	}
	p, _ := s.PeerByOrigin(ctx, "a.ing")
	if p.Status != "revoked" {
		t.Fatalf("revoked status, got %v", p.Status)
	}
	// unknown origin → ErrNotFound (revoke of an unregistered peer, 03 §4.3
	// envelope pre-check path).
	if err := s.RevokePeer(ctx, "ghost.ing", "", "", false); !IsNotFound(err) {
		t.Fatalf("revoke unknown: want ErrNotFound, got %v", err)
	}
}

func TestMapStoreExportGrantLifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewMapStore()
	g := &FederationExportGrant{ProjectID: "proj1", HomeOrigin: "home.ing", ExpiresAt: time.Now().Add(time.Hour), Status: "active"}
	if err := s.SaveExportGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	grants, _ := s.ExportGrantsForProject(ctx, "proj1")
	if len(grants) != 1 {
		t.Fatalf("one grant, got %d", len(grants))
	}
	active, _ := s.ActiveExportGrants(ctx)
	if len(active) != 1 || active[0].ProjectID != "proj1" {
		t.Fatalf("active sweep: %v", active)
	}
	if active[0].ID.IsZero() {
		t.Fatal("SaveExportGrant must mint ObjectID (09 §3.1 PAT mint)")
	}
}

func TestJWKPublicHalfDropsPrivate(t *testing.T) {
	priv := JWK{Kty: "EC", Kid: "k1", Crv: "P-256", X: "x", Y: "y", D: "SCALAR"}
	if !priv.IsPrivateJWK() {
		t.Fatal("D must mark private")
	}
	pub := priv.PublicHalf()
	if pub.D != "" {
		t.Fatalf("public half must drop d: %#v", pub)
	}
	if pub.Kid != "k1" || pub.X != "x" {
		t.Fatalf("public half must keep public fields: %#v", pub)
	}
	// empty D = public.
	if (JWK{Kty: "EC", X: "x"}).IsPrivateJWK() {
		t.Fatal("no-D key must be public")
	}
}

func TestTrustAnchorStore(t *testing.T) {
	ctx := context.Background()
	s := NewMapStore()
	if err := s.SaveTrustAnchor(ctx, &FederationTrustAnchor{EntityID: "ta.example.edu", DisplayName: "eduGain TA"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.TrustAnchorByEntityId(ctx, "ta.example.edu")
	if got.DisplayName != "eduGain TA" {
		t.Fatalf("TA display: %v", got)
	}
	if err := s.DeleteTrustAnchor(ctx, "ta.example.edu"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TrustAnchorByEntityId(ctx, "ta.example.edu"); !IsNotFound(err) {
		t.Fatalf("deleted TA must miss, got %v", err)
	}
	// empty entityId rejected (02 §3 unique).
	if err := s.SaveTrustAnchor(ctx, &FederationTrustAnchor{EntityID: ""}); err == nil {
		t.Fatal("empty entityId must fail")
	}
}
