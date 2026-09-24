package editorpages

import (
	"testing"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TestEditorRoutePatternsCase — U2 pin: Node/Express matches the editor
// route family case-INsensitively (prefix + hex id), pinned live on the
// e2e stack 2026-09-22 (200 on /Project|/project|/Editor in any case; the
// gate asserts the full matrix against Node).
func TestEditorRoutePatternsCase(t *testing.T) {
	valid := []string{
		"/Project/6aaa7f570accd346715942b5",
		"/project/6aaa7f570accd346715942b5",
		"/Project/6AAA7F570ACCD346715942B5",
		"/project/6AAA7F570ACCD346715942B5",
		"/editor/6aaa7f570accd346715942b5",
		"/Editor/6aaa7f570accd346715942b5",
		"/EDITOR/6aaa7f570accd346715942b5",
		"/Project/6aaa7f570accd346715942b5/detacher",
		"/project/6aaa7f570accd346715942b5/detached",
		"/editor/6aaa7f570accd346715942b5/detached",
	}
	for _, p := range valid {
		m := editorPagePattern.FindStringSubmatch(p)
		if m == nil {
			t.Fatalf("valid pattern must match %q", p)
		}
		names := editorPagePattern.SubexpNames()
		id, role := "", ""
		for i := 1; i < len(m); i++ {
			switch names[i] {
			case "id":
				id = m[i]
			case "role":
				role = m[i]
			}
		}
		if id != "6aaa7f570accd346715942b5" && id != "6AAA7F570ACCD346715942B5" {
			t.Fatalf("%q: id param = %q", p, id)
		}
		if (p == "/Project/6aaa7f570accd346715942b5/detacher") && role != "/detacher" {
			t.Fatalf("%q: role = %q", p, role)
		}
	}

	// NOTE: the bad-id pattern is a SUPERSET of the valid one (any non-empty
	// id). What makes the valid route win is dispatch ORDER (same feature,
	// valid registered first; core/app.go walks routes in slice order) —
	// pinned at the end of this test.
	bad := []string{
		"/Project/xyz",
		"/project/xyz",
		"/editor/x",
		"/Project/12345",
		"/Project/zzzzzzzzzzzzzzzzzzzzzzzz",
		"/Project/6aaa7f570accd346715942b57", // 25 chars
		"/Project/6aaa7f570accd346715942b",   // 23 chars
		"/Project/xyz/detached",
		"/Project/xyz/detacher",
	}
	for _, p := range bad {
		if !editorBadIdPattern.MatchString(p) {
			t.Fatalf("bad-id pattern must match %q", p)
		}
	}

	// route order pin: valid route is registered before the bad-id catch
	// (first match wins in core dispatch).
	f := Feature(&core.App{})
	if len(f.Routes) < 2 {
		t.Fatalf("expected 2 routes, got %d", len(f.Routes))
	}
	if f.Routes[0].Pattern != editorPagePattern || f.Routes[1].Pattern != editorBadIdPattern {
		t.Fatal("route order must be valid-before-badid")
	}

	// empty id: NEITHER editor pattern matches (Node: /editor/ → generic 404
	// page; /Project/ → dashboard 301 handled by projectlist)
	for _, p := range []string{"/editor/", "/Editor/", "/Project/", "/project/"} {
		if editorPagePattern.MatchString(p) || editorBadIdPattern.MatchString(p) {
			t.Fatalf("empty-id %q must not match the editor patterns", p)
		}
	}
}

// TestInitialLoadingScreenTheme — U2 pin: Node getInitialTheme(
// getOverallTheme(user)) (pinned live 2026-09-22: e2e-admin 'light-'
// → light; e2e-user 'dark' → dark; missing overall+signUpDate → system;
// old signUpDate → dark).
func TestInitialLoadingScreenTheme(t *testing.T) {
	cases := map[string]struct {
		doc  map[string]any
		want string
	}{
		"light enum":  {map[string]any{"ace": map[string]any{"overallTheme": "light-"}}, "light"},
		"dark value":  {map[string]any{"ace": map[string]any{"overallTheme": "dark"}}, "dark"},
		"system":      {map[string]any{"ace": map[string]any{"overallTheme": "system"}}, "system"},
		"odd value":   {map[string]any{"ace": map[string]any{"overallTheme": "nope"}}, "dark"},
		"no ace":      {map[string]any{}, "system"},
		"old signup":  {map[string]any{"signUpDate": int64(1600000000000)}, "dark"},
		"new signup":  {map[string]any{"signUpDate": int64(1789391285099)}, "system"},
		"dt signup":   {map[string]any{"signUpDate": primitive.DateTime(1789391285099)}, "system"},
		"dt old":      {map[string]any{"signUpDate": primitive.DateTime(1600000000000)}, "dark"},
		"ace nullish": {map[string]any{"ace": map[string]any{}, "signUpDate": int64(1789391285099)}, "system"},
	}
	for name, c := range cases {
		if got := initialLoadingScreenTheme(c.doc); got != c.want {
			t.Fatalf("%s: got %q want %q", name, got, c.want)
		}
	}
}
