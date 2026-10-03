// exportttl_test.go — 2d (09 §3) cap arithmetic (e91d1eaf acceptance:
// "maxExportTtlSeconds HARD cap enforced (TTL = min(request, grant
// remaining, cap))").

package federation

import (
	"testing"
	"time"
)

func TestExportTTL_CapTable(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	noGrant := time.Time{}
	grant48h := now.Add(48 * time.Hour)
	grant10m := now.Add(10 * time.Minute)
	expired := now.Add(-time.Hour)

	cases := []struct {
		name        string
		requested   int
		capSeconds  int
		grantExpiry time.Time
		want        int
	}{
		{"cap beats request", 7200, 3600, noGrant, 3600},
		{"request beats cap", 1800, 3600, noGrant, 1800},
		{"equal", 3600, 3600, noGrant, 3600},
		{"grant remaining beats both", 7200, 86400, grant10m, 600},
		{"grant remaining beats cap", 86400, 7200, grant10m, 600},
		{"request smallest", 120, 3600, grant48h, 120},
		{"no cap when unset", 7200, 0, noGrant, 7200},
		{"grant already expired → 0", 7200, 86400, expired, 0},
		{"requested zero → 0", 0, 3600, noGrant, 0},
		{"requested negative → 0", -5, 3600, noGrant, 0},
	}
	for _, c := range cases {
		if got := exportTTL(c.requested, c.capSeconds, c.grantExpiry, now); got != c.want {
			t.Errorf("%s: exportTTL(%d, %d, %v, now) = %d, want %d",
				c.name, c.requested, c.capSeconds, c.grantExpiry, got, c.want)
		}
	}
}

func TestGrantExpiryAt(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		requested  int
		capSeconds int
		want       time.Time
	}{
		{"fresh: cap applies", 7200, 3600, now.Add(3600 * time.Second)},
		{"fresh: request under cap", 1800, 3600, now.Add(1800 * time.Second)},
		{"fresh: cap unset", 7200, 0, now.Add(7200 * time.Second)},
		{"unusable: zero request", 0, 3600, time.Time{}},
		{"unusable: negative request", -1, 3600, time.Time{}},
	}
	for _, c := range cases {
		if got := grantExpiryAt(c.requested, c.capSeconds, now); !got.Equal(c.want) {
			t.Errorf("%s: grantExpiryAt(%d, %d) = %v, want %v", c.name, c.requested, c.capSeconds, got, c.want)
		}
	}
}

// TestGrantExpiryAt_StoreRoundTrip — the capped expiry survives a grant
// save (MapStore), i.e. the persistence shape the S2S mint leg will use.
func TestGrantExpiryAt_StoreRoundTrip(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	st := NewMapStore()
	g := &FederationExportGrant{
		ProjectID:  "65000000000000000000c0de",
		HomeOrigin: "a.example",
		Scope:      "federation:git_bridge",
		Status:     "active",
		ExpiresAt:  grantExpiryAt(7200, 3600, now), // capped to 3600 s
		CreatedAt:  now,
	}
	if err := st.SaveExportGrant(contextBackground(), g); err != nil {
		t.Fatal(err)
	}
	got, err := st.ExportGrantsForProject(contextBackground(), g.ProjectID)
	if err != nil || len(got) != 1 {
		t.Fatalf("round trip: err=%v grants=%v", err, got)
	}
	if !got[0].ExpiresAt.Equal(now.Add(3600 * time.Second)) {
		t.Errorf("capped expiry: got %v, want %v", got[0].ExpiresAt, now.Add(3600*time.Second))
	}
	// and the capped grant is no longer issuable after it lapses:
	if ttl := exportTTL(7200, 3600, g.ExpiresAt, now.Add(4000*time.Second)); ttl != 0 {
		t.Errorf("lapsed grant must yield TTL 0, got %d", ttl)
	}
}
