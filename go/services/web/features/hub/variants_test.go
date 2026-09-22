// U9: hub case/slash variant pins (Node Express semantics, oracle 2026-09-22).
package hub

import "testing"

func TestHubVariantPatterns(t *testing.T) {
	for _, p := range []string{"/hub", "/hub/", "/HUB", "/HUB/", "/HuB"} {
		if !hubRe.MatchString(p) {
			t.Fatalf("hubRe must match %q", p)
		}
	}
	for _, p := range []string{"/", "/hubs", "/hubx", "/hub/admin"} {
		if hubRe.MatchString(p) {
			t.Fatalf("hubRe must NOT match %q", p)
		}
	}
	for _, p := range []string{"/hub/admin", "/hub/admin/", "/HUB/ADMIN"} {
		if !hubAdminRe.MatchString(p) {
			t.Fatalf("hubAdminRe must match %q", p)
		}
	}
	if hubAdminRe.MatchString("/hub/admin/x") {
		t.Fatal("hubAdminRe must NOT match /hub/admin/x")
	}
	for _, p := range []string{"/hub/workspace", "/hub/workspace/", "/HUB/WORKSPACE/"} {
		if !hubWorkspaceRe.MatchString(p) {
			t.Fatalf("hubWorkspaceRe must match %q", p)
		}
	}
}
