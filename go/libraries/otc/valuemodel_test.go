package otc

import (
	"testing"
	"time"
)

// Oracle: libraries/overleaf-editor-core/test/unit/label.test.js
func TestLabelFromRawAnonymousAuthor(t *testing.T) {
	label := LabelFromRaw(&RawLabel{
		Text:      "test",
		AuthorID:  nil,
		Timestamp: "2016-01-01T00:00:00Z",
		Version:   123,
	})
	if label.GetAuthorID() != nil {
		t.Fatalf("expected null author id, got %v", label.GetAuthorID())
	}
	if label.GetText() != "test" {
		t.Fatalf("expected text 'test', got %q", label.GetText())
	}
	if label.GetVersion() != 123 {
		t.Fatalf("expected version 123, got %d", label.GetVersion())
	}
	want := time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC)
	if !label.GetTimestamp().Equal(want) {
		t.Fatalf("expected timestamp %v, got %v", want, label.GetTimestamp())
	}
	// toRaw renders the timestamp as ISO-8601 with milliseconds (Node toISOString)
	raw := label.ToRaw()
	if raw.Timestamp != "2016-01-01T00:00:00.000Z" {
		t.Fatalf("expected timestamp %q, got %q", "2016-01-01T00:00:00.000Z", raw.Timestamp)
	}
	if raw.AuthorID != nil {
		t.Fatalf("expected nil raw author id, got %v", raw.AuthorID)
	}
}

func TestLabelGetters(t *testing.T) {
	id := int64(42)
	ts := time.Date(2020, 6, 15, 12, 30, 0, 0, time.UTC)
	l := NewLabel("note", &id, ts, 7)
	if l.GetText() != "note" || *l.GetAuthorID() != 42 || l.GetVersion() != 7 {
		t.Fatalf("getters mismatch: %+v", l)
	}
	if got := l.ToRaw().Timestamp; got != "2020-06-15T12:30:00.000Z" {
		t.Fatalf("unexpected raw timestamp %q", got)
	}
}

// Oracle: libraries/overleaf-editor-core/test/unit/file_metadata.test.js
func TestIsDocumentMetadata(t *testing.T) {
	if !IsDocumentMetadata(nil) {
		t.Error("nil metadata should be document metadata")
	}
	if !IsDocumentMetadata(map[string]any{}) {
		t.Error("empty metadata should be document metadata")
	}
	cases := map[string]bool{
		"doc flags only": IsDocumentMetadata(map[string]any{"main": true, "mainBibliography": true}),
		"main only":      IsDocumentMetadata(map[string]any{"main": true}),
		"mainBib only":   IsDocumentMetadata(map[string]any{"mainBibliography": true}),
		"importedAt":     IsDocumentMetadata(map[string]any{"importedAt": "2026-01-02T00:00:00.000Z"}),
		"main+provider":  IsDocumentMetadata(map[string]any{"main": true, "provider": "zotero"}),
	}
	casesExpect := []struct {
		md   map[string]any
		want bool
	}{
		{map[string]any{"main": true}, true},
		{map[string]any{"mainBibliography": true}, true},
		{map[string]any{"main": true, "mainBibliography": true}, true},
		{map[string]any{"importedAt": "2026-01-02T00:00:00.000Z"}, false},
		{map[string]any{"main": true, "provider": "zotero"}, false},
	}
	_ = cases
	for _, c := range casesExpect {
		if got := IsDocumentMetadata(c.md); got != c.want {
			t.Errorf("IsDocumentMetadata(%v) = %v, want %v", c.md, got, c.want)
		}
	}
}

func TestWithDocumentMetadataFlag(t *testing.T) {
	// sets a flag on metadata that has none
	if got := WithDocumentMetadataFlag(map[string]any{}, "mainBibliography", true); len(got) != 1 || got["mainBibliography"] != true {
		t.Errorf("got %v, want {mainBibliography:true}", got)
	}
	// keeps the other flag when setting one
	if got := WithDocumentMetadataFlag(map[string]any{"main": true}, "mainBibliography", true); got["main"] != true || got["mainBibliography"] != true {
		t.Errorf("got %v, want {main:true, mainBibliography:true}", got)
	}
	// keeps the other flag when clearing one
	got := WithDocumentMetadataFlag(map[string]any{"main": true, "mainBibliography": true}, "mainBibliography", false)
	if len(got) != 1 || got["main"] != true {
		t.Errorf("got %v, want {main:true}", got)
	}
	// leaves a cleared flag out rather than setting it false
	if got := WithDocumentMetadataFlag(map[string]any{"main": true}, "main", false); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
	// drops metadata that is not a doc flag
	got = WithDocumentMetadataFlag(map[string]any{"main": true, "importedAt": "2026-01-02T00:00:00.000Z"}, "mainBibliography", true)
	if len(got) != 2 || got["main"] != true || got["mainBibliography"] != true {
		t.Errorf("got %v, want {main:true, mainBibliography:true}", got)
	}
}

func TestHasDocumentMetadataFlag(t *testing.T) {
	if !HasDocumentMetadataFlag(map[string]any{"main": true}, "main") {
		t.Error("expected main flag true")
	}
	if HasDocumentMetadataFlag(map[string]any{"mainBibliography": true}, "main") {
		t.Error("expected main flag false")
	}
	if HasDocumentMetadataFlag(nil, "main") {
		t.Error("expected nil metadata to have no flag")
	}
}

// Oracle (pinned from text_file_defaults.js constants + file_type_detector usage)
func TestTextFileDefaults(t *testing.T) {
	if len(DefaultTextExtensions) != 42 {
		t.Fatalf("expected 42 text extensions, got %d", len(DefaultTextExtensions))
	}
	for _, want := range []string{"tex", "bib", "cls", "md", "lean4", "ltx", "inc"} {
		if !containsStr(DefaultTextExtensions, want) {
			t.Errorf("missing text extension %q", want)
		}
	}
	// no leading dots
	for _, ext := range DefaultTextExtensions {
		if len(ext) == 0 || ext[0] == '.' {
			t.Fatalf("unexpected extension %q", ext)
		}
	}
	if len(DefaultRootDocExtensions) != 4 {
		t.Fatalf("expected 4 root doc extensions, got %d", len(DefaultRootDocExtensions))
	}
	for _, want := range []string{"tex", "Rtex", "ltx", "Rnw"} {
		if !containsStr(DefaultRootDocExtensions, want) {
			t.Errorf("missing root doc extension %q", want)
		}
	}
	if len(DefaultEditableFilenames) != 4 {
		t.Fatalf("expected 4 editable filenames, got %d", len(DefaultEditableFilenames))
	}
	for _, want := range []string{"latexmkrc", ".latexmkrc", "makefile", "gnumakefile"} {
		if !containsStr(DefaultEditableFilenames, want) {
			t.Errorf("missing editable filename %q", want)
		}
	}
}

// Oracle (author.js + author_list.js; authors exercised via the Change suite in Node)
func TestAuthor(t *testing.T) {
	a := NewAuthor(7, "a@b.c", "A. B.")
	if a.GetID() != 7 || a.GetEmail() != "a@b.c" || a.GetName() != "A. B." {
		t.Fatalf("author getters mismatch: %+v", a)
	}
	raw := a.ToRaw()
	if raw["id"] != int64(7) || raw["email"] != "a@b.c" || raw["name"] != "A. B." {
		t.Fatalf("toRaw mismatch: %v", raw)
	}
	if AuthorFromRaw(nil) != nil {
		t.Fatalf("AuthorFromRaw(nil) should be nil")
	}
	if got := AuthorFromRaw(a); got.ID != 7 {
		t.Fatalf("AuthorFromRaw round-trip failed: %+v", got)
	}
}

func TestAssertAuthorsV1(t *testing.T) {
	// all numbers
	AssertAuthorsV1([]any{1, 2, 3}, "")
	// all Authors
	AssertAuthorsV1([]any{NewAuthor(1, "x", "X"), NewAuthor(2, "y", "Y")}, "")
	// nils are disregarded
	AssertAuthorsV1([]any{1, nil, 2}, "")
	AssertAuthorsV1(nil, "") // empty
	// mixed number + string must panic
	assertPanics(t, func() { AssertAuthorsV1([]any{1, "notanauthor"}, "") })
	// string first (not a number) then a non-Author must panic
	assertPanics(t, func() { AssertAuthorsV1([]any{"a", 3}, "") })
}

func TestAssertAuthorsV2(t *testing.T) {
	valid := "0123456789abcdef01234567"
	AssertAuthorsV2([]any{valid, valid}, "")
	// maybe-regex: nil is allowed
	AssertAuthorsV2([]any{nil, valid}, "")
	// wrong length / non-hex must panic
	assertPanics(t, func() { AssertAuthorsV2([]any{"xyz"}, "") })
	assertPanics(t, func() { AssertAuthorsV2([]any{"0123456789abcdef0123456g"}, "") })
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
