package library

import (
	"strings"
	"testing"
)

// P6.5 — pure-function parity tests against the Node oracle pins
// (/tmp/p65_node.json): validation reasons (c_* pins), normalize/empty-removes
// (c_ok fields), the .bib serializer (download pins), the search pipeline
// (SaaS machine-extracted helpers), and the suggestion base sanitizer.

func TestValidateEntryReasons(t *testing.T) {
	cases := []struct {
		entry map[string]any
		want  string
	}{
		{nil, "entry-not-object"},
		{map[string]any{}, "key-missing"},
		{map[string]any{"key": 42, "type": "article"}, "key-missing"},
		{map[string]any{"key": strings.Repeat("k", 129), "type": "article"}, "key-too-long"},
		{map[string]any{"key": "bad key!", "type": "article"}, "key-invalid"},
		{map[string]any{"key": "a_b-C.d", "type": "article"}, "ok"},
		{map[string]any{"key": "k", "type": ""}, "type-invalid"},
		{map[string]any{"key": "k", "type": strings.Repeat("t", 65)}, "type-invalid"},
		{map[string]any{"key": "k", "type": "bogus"}, "type-unknown"},
		{map[string]any{"key": "k", "type": "BOOK"}, "ok"},
		{map[string]any{"key": "k", "type": "article", "fields": "nope"}, "fields-not-array"},
		{map[string]any{"key": "k", "type": "article", "fields": []any{42}}, "field-not-object"},
		{map[string]any{"key": "k", "type": "article", "fields": []any{map[string]any{"name": "Bad", "value": "v"}}}, "ok"},
		{map[string]any{"key": "k", "type": "article", "fields": []any{map[string]any{"name": "9bad", "value": "v"}}}, "field-name-invalid"},
		{map[string]any{"key": "k", "type": "article", "fields": []any{map[string]any{"name": "a", "value": 42}}}, "field-value-not-string"},
		{map[string]any{"key": "k", "type": "article", "fields": []any{map[string]any{"name": "a", "value": strings.Repeat("x", 32769)}}}, "field-value-too-long"},
		// field value null/absent = allowed (Node: null/undefined value = empty)
		{map[string]any{"key": "k", "type": "article", "fields": []any{map[string]any{"name": "a"}}}, "ok"},
	}
	for i, c := range cases {
		got := validateEntry(c.entry).reason
		if got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

func TestValidateBatch(t *testing.T) {
	if r := validationReasonFromBody(nil); r != "entries-missing" {
		t.Errorf("nil entries: got %q", r)
	}
	if r := validationReasonFromBody([]any{map[string]any{"key": "a", "type": "article"}, 42}); r != "entry-not-object" {
		t.Errorf("mixed: got %q", r)
	}
	many := []any{}
	for i := 0; i < maxEntryCount+1; i++ {
		many = append(many, map[string]any{"key": "k", "type": "article"})
	}
	if r := validationReasonFromBody(many); r != "entries-too-many" {
		t.Errorf("too many: got %q", r)
	}
}

func TestNormalizeEntry(t *testing.T) {
	// c_ok pin: last-wins on name; `empty:"drop me"` kept (value non-empty)
	// and lowercased; a fresh EMPTY value never creates the name.
	fields := []any{
		map[string]any{"name": "TITLE", "value": "Old Title"},
		map[string]any{"name": "title", "value": "Last Wins Title"},
		map[string]any{"name": "EMPTY", "value": "drop me"},
		map[string]any{"name": "author", "value": "A and B"},
		map[string]any{"name": "note", "value": ""},
		map[string]any{"name": "BAD NAME!", "value": "x"},
	}
	_, _, out := normalizeEntry(" OracleKey ", " ARTICLE ", fields)
	want := []fieldOut{
		{Name: "title", Value: "Last Wins Title"},
		{Name: "empty", Value: "drop me"},
		{Name: "author", Value: "A and B"},
	}
	if len(out) != len(want) {
		t.Fatalf("len %d want %d: %v", len(out), len(want), out)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("field %d: got %+v want %+v", i, out[i], want[i])
		}
	}
}

func TestNormalizeRemovesEmpty(t *testing.T) {
	// A later EMPTY value for an existing name REMOVES it (map.delete).
	fields := []any{
		map[string]any{"name": "author", "value": "keep"},
		map[string]any{"name": "title", "value": "gone"},
		map[string]any{"name": "title", "value": ""},
		map[string]any{"name": "author", "value": "keep"},
	}
	_, _, out := normalizeEntry("k", "misc", fields)
	if len(out) != 1 || out[0].Name != "author" || out[0].Value != "keep" {
		t.Fatalf("got %+v", out)
	}
}

func TestEscapeBibValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a}b", "a\\}b"},
		{"{ok {unbalanced \\}end", "{ok {unbalanced \\}end"}, // preserved: escaped \} + balanced braces
		{"balanced {pair} here", "balanced {pair} here"},
		{"\\{ already \\} escaped", "\\{ already \\} escaped"},
		{"plain", "plain"},
	}
	for _, c := range cases {
		if got := escapeBibValue(c.in); got != c.want {
			t.Errorf("escape %q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestSerializeBibEntry(t *testing.T) {
	e := bibEntry{
		Key:  "key2026",
		Type: "article",
		Fields: []fieldOut{
			{Name: "title", Value: "A } Title"},
			{Name: "EMPTY", Value: ""},
			{Name: "note", Value: "  keep  "},
		},
	}
	want := "@article{key2026,\n" +
		"  title = {A \\} Title},\n" +
		"  note = {keep}\n}"
	if got := serializeBibEntry("key2026", "article", e.Fields); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	// no-fields shape
	if got := serializeBibEntry("k", "misc", nil); got != "@misc{k}\n" {
		t.Errorf("no-fields: got %q", got)
	}
}

func TestSerializeBibFile(t *testing.T) {
	if got := serializeBibFile(nil); got != "" {
		t.Errorf("empty: got %q", got)
	}
	entries := []bibEntry{
		{Key: "a", Type: "misc"},
		{Key: "b", Type: "book", Fields: []fieldOut{{Name: "title", Value: "T"}}},
	}
	want := "@misc{a}\n" + "\n" + "@book{b,\n  title = {T}\n}\n"
	if got := serializeBibFile(entries); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestNormalizeSearchText(t *testing.T) {
	cases := []struct{ in, want string }{
		{" Café ", " cafe "}, // NFD strip (é → e)
		{"café", "cafe"},
		{"Straße", "strasse"}, // ß → ss
		{"Ligature œuf", "ligature oeuf"},
		{"Ångström", "angstrom"}, // Å → a (NFD), ö → o
		{"UPPER", "upper"},
	}
	for _, c := range cases {
		if got := normalizeSearchText(c.in); got != c.want {
			t.Errorf("norm %q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestTokenizeSearchQuery(t *testing.T) {
	got := tokenizeSearchQuery("Ernst, E. & Müller (2007) 'ernst'")
	want := []string{"ernst", "e", "muller", "2007"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	// de-dup within the same normalized text
	got = tokenizeSearchQuery("cafe café")
	if len(got) != 1 || got[0] != "cafe" {
		t.Errorf("dedup: got %v", got)
	}
}

func TestEntrySearchBlob(t *testing.T) {
	blob := entrySearchBlob("Ernst2007", "article", []fieldOut{
		{Name: "title", Value: "Efficient Caching, E. & Müller 2007"},
		{Name: "author", Value: "Ernst, E."},
	})
	for _, tok := range []string{"ernst2007", "article", "title", "efficient", "caching", "muller", "2007", "author", "ernst", "e"} {
		if !strings.Contains(blob, tok) {
			t.Errorf("blob missing token %q: %q", tok, blob)
		}
	}
}

func TestSanitizeCitationKey(t *testing.T) {
	if got := sanitizeCitationKey("My-Key 42!"); got != "mykey42" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("a", 100)
	if got := sanitizeCitationKey(long); len(got) != 64 {
		t.Errorf("cap: got len %d", len(got))
	}
}

func TestJSLenUTF16(t *testing.T) {
	// JS "ab".length=2; a surrogate pair is 2 utf16 units.
	s := "ab" + "😀"
	if jsLen(s) != 4 {
		t.Errorf("jsLen got %d want 4", jsLen(s))
	}
}

func TestJstrEscaping(t *testing.T) {
	cases := map[string]string{
		`a"b`:     `"a\"b"`,
		`a\b`:     `"a\\b"`,
		"a\nb":    `"a\nb"`,
		"a\tb":    `"a\tb"`,
		"ünïcode": `"ünïcode"`,
	}
	for in, want := range cases {
		if got := jstr(in); got != want {
			t.Errorf("jstr %q: got %q want %q", in, got, want)
		}
	}
}

func TestToApiEntryShape(t *testing.T) {
	doc := map[string]any{
		"key":  "oracle2026a",
		"type": "article",
		"fields": []any{
			map[string]any{"name": "title", "value": "Last Wins Title"},
			map[string]any{"name": "empty", "value": "drop me"},
		},
	}
	got := toApiEntry(doc, 0)
	want := `{"key":"oracle2026a","type":"article","fields":[{"name":"title","value":"Last Wins Title"},{"name":"empty","value":"drop me"}],"_id":"","occurrenceIndex":0,"updatedAt":null,"createdAt":null}`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestValidationMessagesTable(t *testing.T) {
	// The 400 pin table (LibraryController VALIDATION_MESSAGES verbatim).
	want := map[string]string{
		"entries-missing":   "At least one reference is required.",
		"key-missing":       "A citation key is required.",
		"key-invalid":       "The citation key is invalid (letters, numbers, dot, underscore and dash only).",
		"type-invalid":      "A valid entry type is required.",
		"nothing-to-delete": "Provide ids or a search term to delete.",
	}
	for r, m := range want {
		if validationMessages[r] != m {
			t.Errorf("message[%s]: got %q", r, validationMessages[r])
		}
	}
	// fallback passthrough for unknown reason
	if got := validationMessage("mystery", "FB"); got != "FB" {
		t.Errorf("fallback: got %q", got)
	}
}
