package configschema

import "testing"

// TestRegistryVersionPins enforces the owner safety directive (2026-10-04):
// the configstore plane (registry + defaults — the single source of truth for
// the golang toolkit, /hub, configdb, and the TUI) must carry EXACTLY these
// image pins. Editing either file to a different pin fails CI; moving the
// pins requires a new directive.
func TestRegistryVersionPins(t *testing.T) {
	const (
		wantMongo = "mongo:9.0"
		wantRedis = "redis:8.10-alpine3.23"
	)
	got := map[string]string{}
	for k, want := range map[string]string{"MONGO_IMAGE": wantMongo, "REDIS_IMAGE": wantRedis} {
		p, ok := Find(k)
		if !ok {
			t.Fatalf("registry missing key %s", k)
		}
		if p.Default != want {
			t.Fatalf("registry %s = %q, want %q (owner directive 2026-10-04)", k, p.Default, want)
		}
		got[k] = p.Default
	}
	// MONGO_VERSION drives MONGOSH/mongo + --replSet args — must match the pin
	if p, ok := Find("MONGO_VERSION"); !ok {
		t.Fatal("registry missing MONGO_VERSION")
	} else if p.Default != "9" {
		t.Fatalf("registry MONGO_VERSION = %q, want 9", p.Default)
	}
	_ = got
}
