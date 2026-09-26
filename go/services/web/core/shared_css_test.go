package core

// shared_css_test.go — D39a regression pin: SHARED:CSS must resolve the
// entry's shared CSS chunks (the .js?-only regex bug silently dropped every
// shared stylesheet — owner-reported login regression; register/detached
// pages were equally unstyled once webpack emitted per-chunk CSS).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSharedCSSResolution(t *testing.T) {
	dir := t.TempDir()
	// generation fixture: entry footer e.O(0,[42,7,99]); chunks 42,7 have
	// BOTH js+css; chunk 99 has js ONLY (css missing on disk).
	writeFixture(t, dir, "js/pages-x-cccccccccccccccc.js",
		"/*entry*/e.O(0,[42,7,99],function(){return 1});")
	writeFixture(t, dir, "js/42-aaaaaaaaaaaaaaaa.js", "/*c42*/")
	writeFixture(t, dir, "js/7-bbbbbbbbbbbbbbbb.js", "/*c7*/")
	writeFixture(t, dir, "js/99-dddddddddddddddd.js", "/*c99*/")
	writeFixture(t, dir, "stylesheets/42-aaaaaaaaaaaaaaaa.css", "/*42*/")
	writeFixture(t, dir, "stylesheets/7-bbbbbbbbbbbbbbbb.css", "/*7*/")

	AssetManifest["pages/x"] = "/js/pages-x-cccccccccccccccc.js"
	SetPublicDir(dir)

	cssTags := SharedTags("pages/x", "css")
	if !strings.Contains(cssTags, `/stylesheets/42-aaaaaaaaaaaaaaaa.css`) ||
		!strings.Contains(cssTags, `/stylesheets/7-bbbbbbbbbbbbbbbb.css`) {
		t.Fatalf("shared css links missing: %q", cssTags)
	}
	if strings.Contains(cssTags, "99-") {
		t.Fatalf("css-missing chunk must not emit a link: %q", cssTags)
	}
	// Entry order preserved.
	if strings.Index(cssTags, `/stylesheets/42-`) > strings.Index(cssTags, `/stylesheets/7-`) {
		t.Fatalf("order: %q", cssTags)
	}

	jsTags := SharedTags("pages/x", "js")
	wantJS := []string{`/js/42-aaaaaaaaaaaaaaaa.js`, `/js/7-bbbbbbbbbbbbbbbb.js`, `/js/99-dddddddddddddddd.js`}
	if len(wantJS) != 3 {
		t.Fatal("fixture drift")
	}
	for _, u := range wantJS {
		if !strings.Contains(jsTags, `src="`+u+`"`) {
			t.Fatalf("js link missing %s in: %q", u, jsTags)
		}
	}
	if strings.Contains(jsTags, ".css") {
		t.Fatalf("js tags must not reference css files: %q", jsTags)
	}

	// ops observability: the css-missing id must be in sharedMissing.
	found := false
	for _, s := range SharedMissing() {
		if strings.Contains(s, "css:pages/x#99") {
			found = true
		}
	}
	if !found {
		t.Fatalf("sharedMissing lacks css:pages/x#99: %v", SharedMissing())
	}
}
