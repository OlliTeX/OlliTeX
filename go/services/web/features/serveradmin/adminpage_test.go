// U9: /admin page pattern pins (case + slash, Node oracle 2026-09-22).
package serveradmin

import "testing"

func TestAdminPagePattern(t *testing.T) {
	for _, p := range []string{"/admin", "/admin/", "/Admin", "/ADMIN", "/aDmIn"} {
		if !adminPageRe.MatchString(p) {
			t.Fatalf("adminPageRe must match %q", p)
		}
	}
	for _, p := range []string{"/", "/admins", "/admin/editor-state", "/admin2"} {
		if adminPageRe.MatchString(p) {
			t.Fatalf("adminPageRe must NOT match %q", p)
		}
	}
}
