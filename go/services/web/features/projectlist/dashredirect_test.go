package projectlist

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"ollitex/go/services/web/core"
)

// Parity pins for the legacy project-dashboard redirects (Node
// services/web/app/src/router.mjs projectDashboardRedirects, owner queue 7
// 2026-09-10; captured 2026-09-22 on the e2e stack against Node v22.21.1):
//
//	authed:     301, Location=<hub>, text/plain
//	           body "Moved Permanently. Redirecting to <hub>"
//	anonymous:  302 /login (requireLogin — verified by the e2e suite; the
//	           requireLogin bounce itself is core-pinned in core_test.go)
//
// The table is 1:1 with Node, including the literal tags target
// `/hub#/projects.tags.tags` (Node's own static string).
func TestDashboardRedirectTable(t *testing.T) {
	f := Feature(nil)
	want := []struct {
		method string
		path   string
		loc    string
	}{
		{"GET", "/project", "/hub#/projects.all"},
		{"GET", "/project/owned", "/hub#/projects.owned"},
		{"GET", "/project/shared", "/hub#/projects.shared"},
		{"GET", "/project/archived", "/hub#/projects.archived"},
		{"GET", "/project/trashed", "/hub#/projects.trashed"},
		{"GET", "/project/untagged", "/hub#/projects.all"},
	}
	seen := map[string]bool{}
	for _, r := range f.Routes {
		if r.Method != "GET" || r.Path == "" || r.NoLogin {
			continue
		}
		if r.Path == "/project" ||
			r.Path == "/project/owned" ||
			r.Path == "/project/shared" ||
			r.Path == "/project/archived" ||
			r.Path == "/project/trashed" ||
			r.Path == "/project/untagged" {
			seen[r.Path] = true
			w := httptest.NewRecorder()
			req := httptest.NewRequest(r.Method, r.Path, nil)
			req.Header.Set("Accept", "*/*")
			r.Handler(&core.Cxt{Req: req}, &core.Res{W: w})
			if w.Code != 301 {
				t.Fatalf("%s: status %d want 301", r.Path, w.Code)
			}
			for _, wrow := range want {
				if wrow.path == r.Path {
					if got := w.Header().Get("Location"); got != wrow.loc {
						t.Fatalf("%s: Location %q want %q", r.Path, got, wrow.loc)
					}
					if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
						t.Fatalf("%s: content-type %q", r.Path, ct)
					}
					wantBody := "Moved Permanently. Redirecting to " + wrow.loc
					if got := w.Body.String(); got != wantBody {
						t.Fatalf("%s: body %q want %q", r.Path, got, wantBody)
					}
				}
			}
		}
	}
	for _, wrow := range want {
		if !seen[wrow.path] {
			t.Fatalf("route %s not registered", wrow.path)
		}
		// registration order: the redirect block must come FIRST (Node
		// registers projectDashboardRedirects before the other /project
		// routes; first match wins in core)
	}
	first := f.Routes
	if first[0].Path != "/project" || first[0].Method != "GET" {
		t.Fatalf("dash redirects must be registered first, got %s %s", first[0].Method, first[0].Path)
	}

	// /project/tags/:tag — pattern route, literal Node target
	var tag *core.Route
	for i := range f.Routes {
		if f.Routes[i].Pattern != nil && f.Routes[i].Pattern == dashTagPat {
			tag = &f.Routes[i]
		}
	}
	if tag == nil {
		t.Fatal("dashTagPat route not registered")
	}
	if tag.NoLogin {
		t.Fatal("dashTagPat must require login")
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/project/tags/some-tag", nil)
	req.Header.Set("Accept", "*/*")
	tag.Handler(&core.Cxt{Req: req}, &core.Res{W: w})
	if w.Code != 301 {
		t.Fatalf("tags: status %d want 301", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/hub#/projects.tags.tags" {
		t.Fatalf("tags: Location %q (Node's literal target)", got)
	}
	if wantBody := "Moved Permanently. Redirecting to /hub#/projects.tags.tags"; w.Body.String() != wantBody {
		t.Fatalf("tags: body %q", w.Body.String())
	}

	// U2 — /project/ (any case) — Node/Express case-insensitive + trailing
	// slash: same dashboard 301 (pinned live 2026-09-22).
	var slash *core.Route
	for i := range f.Routes {
		if f.Routes[i].Pattern != nil && f.Routes[i].Pattern == dashSlashPat {
			slash = &f.Routes[i]
		}
	}
	if slash == nil {
		t.Fatal("dashSlashPat route not registered")
	}
	for _, p := range []string{"/project/", "/Project/", "/PROJECT/"} {
		if !dashSlashPat.MatchString(p) {
			t.Fatalf("dashSlashPat must match %q", p)
		}
		ws := httptest.NewRecorder()
		req := httptest.NewRequest("GET", p, nil)
		req.Header.Set("Accept", "*/*")
		slash.Handler(&core.Cxt{Req: req}, &core.Res{W: ws})
		if ws.Code != 301 || ws.Header().Get("Location") != "/hub#/projects.all" {
			t.Fatalf("%s: want 301 /hub#/projects.all, got %d %q", p, ws.Code, ws.Header().Get("Location"))
		}
	}
	for _, p := range []string{"/project", "/project/owned", "/project/x/y"} {
		if dashSlashPat.MatchString(p) {
			t.Fatalf("dashSlashPat must NOT match %q", p)
		}
	}
	_ = http.MethodGet
}
