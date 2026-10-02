package wordcount

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- typEscape (Node _typEscape) --------------------------------------------------

func TestTypEscape(t *testing.T) {
	if got := typEscape(`C:\dir\file.typ`); got != `C:\\dir\\file.typ` {
		t.Fatalf("backslash: %q", got)
	}
	if got := typEscape(`say "hi"`); got != `say \"hi\"` {
		t.Fatalf("quote: %q", got)
	}
	if got := typEscape("plain"); got != "plain" {
		t.Fatalf("plain: %q", got)
	}
}

// --- buildInjectedDoc (Node buildInjectedDoc) --------------------------------------

func TestBuildInjectedDoc(t *testing.T) {
	doc := buildInjectedDoc("main.typ")
	if !strings.Contains(doc, `#import "__clsi_wordometer.typ" as wo`) {
		t.Fatalf("import: %s", doc)
	}
	if !strings.Contains(doc, "#show: wo.word-count") {
		t.Fatalf("show: %s", doc)
	}
	if !strings.Contains(doc, `#include "main.typ"`) {
		t.Fatalf("include: %s", doc)
	}
	if !strings.Contains(doc, "WORDOMETER_OUTPUT_START TOTAL_WORDS: #wo.total-words HEADING_WORDS: #headWords NUM_HEADINGS: #heads.len() WORDOMETER_OUTPUT_END") {
		t.Fatalf("marker: %s", doc)
	}
	if !strings.Contains(doc, "#pagebreak()") {
		t.Fatalf("pagebreak: %s", doc)
	}
	// a quoted root resource is escaped in the include.
	doc2 := buildInjectedDoc(`dir/file "quoted".typ`)
	if !strings.Contains(doc2, `#include "dir/file \"quoted\".typ"`) {
		t.Fatalf("escaped include: %s", doc2)
	}
}

// --- ParseMarkerText (rendered PDF marker) ------------------------------------------

func TestParseMarkerText(t *testing.T) {
	// exact rendered marker
	total, heading, heads, ok := ParseMarkerText(
		"WORDOMETER_OUTPUT_START TOTAL_WORDS: 120 HEADING_WORDS: 10 NUM_HEADINGS: 3 WORDOMETER_OUTPUT_END")
	if !ok || total != 120 || heading != 10 || heads != 3 {
		t.Fatalf("marker: %d %d %d %v", total, heading, heads, ok)
	}
	// tolerance: pdfjs splits fragments — extra spaces/newlines are fine.
	total, heading, heads, ok = ParseMarkerText(
		"foo bar\nTOTAL_WORDS:  50   HEADING_WORDS: 5   NUM_HEADINGS: 2\nbaz")
	if !ok || total != 50 || heading != 5 || heads != 2 {
		t.Fatalf("tolerant: %d %d %d %v", total, heading, heads, ok)
	}
	// no marker
	_, _, _, ok = ParseMarkerText("no marker here")
	if ok {
		t.Fatal("expected no marker")
	}
	// empty
	_, _, _, ok = ParseMarkerText("")
	if ok {
		t.Fatal("expected no marker (empty)")
	}
}

// --- Inject (Node injectWordometer) --------------------------------------------------

func TestInjectWritesArtifacts(t *testing.T) {
	dir := t.TempDir()
	if err := Inject(dir, "main.typ"); err != nil {
		t.Fatal(err)
	}
	// the vendored wordometer is written
	if fi, err := os.Stat(filepath.Join(dir, WCWordometer)); err != nil || fi.Size() == 0 {
		t.Fatalf("wordometer: %v", fi)
	}
	// the injected driver references the root resource + wordometer
	data, err := os.ReadFile(filepath.Join(dir, WCMain))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `#import "__clsi_wordometer.typ" as wo`) ||
		!strings.Contains(s, `#include "main.typ"`) {
		t.Fatalf("main doc: %s", s)
	}
	// the wordometer content matches the vendored asset
	woo, err := os.ReadFile(filepath.Join(dir, WCWordometer))
	if err != nil {
		t.Fatal(err)
	}
	if len(woo) != len(wordometerTyp) || string(woo) != wordometerTyp {
		t.Fatal("wordometer content mismatch")
	}
}

func TestInjectIntoMissingSubdir(t *testing.T) {
	// writeFile mkdir-then-writes: a nested missing dir is created.
	dir := t.TempDir() + "/deep/nested"
	if err := Inject(dir, "main.typ"); err != nil {
		t.Fatalf("nested inject: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, WCMain)); err != nil {
		t.Fatalf("nested main: %v", fi)
	}
}

func TestInjectUnderFileFails(t *testing.T) {
	// compiling into a dir that is a FILE: the artifact write returns
	// ENOTDIR -> Inject propagates the error (no silent success).
	file := t.TempDir() + "/f"
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Inject(file, "main.typ"); err == nil {
		t.Fatal("expected error injecting under a file 'dir'")
	}
}

func TestWriteFileUnderFileFails(t *testing.T) {
	file := t.TempDir() + "/f"
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(file+"/child", "x"); err == nil {
		t.Fatal("expected ENOTDIR error writing under a file 'dir'")
	}
}

// --- Remove (Node removeArtifacts) ----------------------------------------------------

func TestRemoveMissingIsOK(t *testing.T) {
	dir := t.TempDir()
	// fs.rm({force:true}): a missing file is fine.
	if err := Remove(dir); err != nil {
		t.Fatalf("missing remove: %v", err)
	}
}

func TestRemovePresents(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{WCMain, WCWordometer, WCOutPDF} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Remove(dir); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, n := range []string{WCMain, WCWordometer, WCOutPDF} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Fatalf("%s still present", n)
		}
	}
}

func TestRemoveParentIsFile(t *testing.T) {
	// os.Remove on a path whose parent is a file -> ENOTDIR (not IsNotExist)
	// -> propagates.
	file := t.TempDir() + "/f"
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Remove(file); err == nil {
		t.Fatal("expected error removing under a file parent")
	}
}
