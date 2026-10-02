// Package main tests (T11): the cmd-layer seams that production main wires
// but the package constructors can't expose:
//
//   - productionFind (the §3.4 READER/WRITER fork lever): happy-path
//     geometry (writer dir -> contentDir-relative paths), ENOENT
//     propagation (-> the frozen READER's OpenErrorToNotFound -> 404).
//   - startLifespanGuard: zero-limit no-op + flip-at-limit.
//
// Everything else is covered by the package tests (>=90% gate, Makefile).
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// happyDir seeds <contentDir>/generated-files/<build>/ and returns the
// READER-style dir arg (contentDir + build + "/.", exactly what the frozen
// READER assembles: GetContentDir + PathForBuild(build, ".")).
func happyDir(t *testing.T, contentDir, build string, files map[string]string) string {
	buildDir := filepath.Join(contentDir, "generated-files", build)
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		p := filepath.Join(buildDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return contentDir + "/" + build + "/."
}

// TestProductionFind_HappyPath: files persisted at
// <contentDir>/generated-files/<build>/ come back as contentDir-relative
// paths, and the frozen READER's ArchiveTo contract holds — open(
// filepath.Join(contentDir, f.Path)) must resolve to the seeded bytes.
func TestProductionFind_HappyPath(t *testing.T) {
	contentDir := t.TempDir()
	readerDir := happyDir(t, contentDir, "b1", map[string]string{
		"mainpdf":            "PDFDATA",
		"deep/nested.typ":    "nested-bytes",
		"wordcount-tex.json": `{"x":1}`,
	})

	got, err := productionFind()(nil, readerDir)
	if err != nil {
		t.Fatalf("happy path: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("file count: got %d, want 3 (%v)", len(got), got)
	}
	for _, f := range got {
		full := filepath.Join(contentDir, filepath.FromSlash(f.Path))
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("ArchiveTo open contract violated for %v: %v", f.Path, err)
		}
		want, ok := map[string]string{
			"generated-files/b1/deep/nested.typ":    "nested-bytes",
			"generated-files/b1/mainpdf":            "PDFDATA",
			"generated-files/b1/wordcount-tex.json": `{"x":1}`,
		}[f.Path]
		if !ok {
			t.Fatalf("unexpected contentDir-relative path: %v", f.Path)
		}
		if string(data) != want {
			t.Fatalf("path %v: want %q, got %q", f.Path, want, data)
		}
	}
}

// TestProductionFind_ENOENT: a missing build dir propagates os.ErrNotExist
// (the frozen READER maps it to NotFoundError -> otc (404, nil) -> 404).
func TestProductionFind_ENOENT(t *testing.T) {
	_, err := productionFind()(nil, t.TempDir()+"/nope/.")
	if err == nil {
		t.Fatal("want ENOENT for missing build dir, got nil")
	}
	if os.IsNotExist(err) {
		// Contract: the frozen READER's OpenErrorToNotFound maps exactly
		// os.ErrNotExist -> NotFoundError -> (404, nil).
	} else {
		t.Fatalf("want os.ErrNotExist-shaped error for the READER mapping, got: %v", err)
	}
}

// TestStartLifespanGuard_ZeroLimit: limitMs <= 0 => no-op, no flip
// (deployments with PROCESS_LIFE_SPAN_LIMIT_MS unset/0 never age out).
func TestStartLifespanGuard_ZeroLimit(t *testing.T) {
	var flipped bool
	stop := make(chan struct{})
	close(stop)
	startLifespanGuard(func(bool) { flipped = true }, 0, stop)
	if flipped {
		t.Fatal("zero limit: want no flip, got flip")
	}
}

// TestStartLifespanGuard_Fires: a small positive limit must flip the flag
// before the 1s window (jitter of rand.Float64() can only shrink the limit).
func TestStartLifespanGuard_Fires(t *testing.T) {
	flipCh := make(chan struct{}, 1)
	stopCh := make(chan struct{})
	go startLifespanGuard(func(bool) { flipCh <- struct{}{} }, 40, stopCh)
	select {
	case <-flipCh:
	case <-time.After(1 * time.Second):
		close(stopCh)
		t.Fatal("lifespan guard did not fire within expected window")
	}
}
