package toolkit

// zz_live_hub_test.go — LIVE round-trip of the hub views against the
// canonical stack's content DB. Skipped when the stack (or its mongo) is
// not reachable — the hermetic tests carry the predicate coverage.
//
// Run:  go test ./go/services/toolkit/ -run TestZZLiveHub -v -timeout 120s

import (
	"context"
	"testing"
	"time"
)

func TestZZLiveHub(t *testing.T) {
	tk, _ := offlineToolkit(t)
	ctx0, c0 := context.WithTimeout(context.Background(), 15*time.Second)
	dsn, dbName, host, rerr := tk.HubResolve(ctx0)
	c0()
	if rerr != nil {
		t.Skipf("no live content DB: %v", rerr)
	}
	t.Logf("round-trip: %s (db %s, host %s)", dsn, dbName, host)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := tk.HubCollect(ctx, 10)
	if err != nil {
		t.Skipf("live hub round-trip skipped: %v (start the canonical stack)", err)
	}

	// internal consistency (the identities the hub leaves rely on)
	p, u := s.Projects, s.Users
	if p.All < p.Deleted {
		t.Errorf("projects: All(%d) < Deleted(%d)", p.All, p.Deleted)
	}
	if p.All == 0 {
		t.Errorf("projects: the shared store has the imported projects — expect >0")
	}
	if u.All < u.Deleted {
		t.Errorf("users: All(%d) < Deleted(%d)", u.All, u.Deleted)
	}
	if len(p.Sample) != 10 && p.All > 10 {
		t.Errorf("projects sample = %d, want 10", len(p.Sample))
	}
	t.Logf("LIVE hub ok: db=%s host=%s", s.DB, s.MongoHost)
	t.Logf("  projects: all=%d inactive=%d trashed=%d deleted=%d", p.All, p.Inactive, p.Trashed, p.Deleted)
	for _, r := range p.Sample[:hubMin(3, len(p.Sample))] {
		t.Logf("    sample: %q owner=%s T=%v I=%v D=%v", r.Name, r.Owner, r.Trashed, r.Inactive, r.Deleted)
	}
	t.Logf("  users: all=%d admins=%d suspended=%d inactive=%d deleted=%d", u.All, u.Admins, u.Suspended, u.Inactive, u.Deleted)
	for _, r := range u.Sample[:hubMin(3, len(u.Sample))] {
		t.Logf("    sample: %s admin=%v susp=%v inact=%v", r.Email, r.IsAdmin, r.Suspended, r.Inactive)
	}
}

func hubMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
