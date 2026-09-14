package serveradmin

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"ollitex/go/services/web/core"
)

// P3.1 validation parity — the 400 issue strings are pinned verbatim
// against the Node oracle (p3pin*.json), so test them directly.

func TestReadBodyRootKind(t *testing.T) {
	cases := []struct {
		body string
		kind string
		ok   bool
	}{
		{`5`, "scalar", false},
		{`12.5`, "scalar", false},
		{`"str"`, "scalar", false},
		{`true`, "scalar", false},
		{`false`, "scalar", false},
		{`5x`, "scalar", false}, // unparseable → body-parser rejection shape
		{`[1]`, "array", false}, // array root → zod 'received array' verbose 400
		{`["a","b"]`, "array", false},
		{`{}`, "", true},
		{`null`, "null", false}, // express.json accepts null; zod → 'received null'
		{"", "", true},          // truly empty body → express.json {}
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/admin/messages", strings.NewReader(c.body))
		_, _, kind, ok := readBody(r)
		if ok != c.ok {
			t.Fatalf("body %q: ok=%v want %v", c.body, ok, c.ok)
		}
		if !ok && kind != c.kind {
			t.Fatalf("body %q: kind=%q want %q", c.body, kind, c.kind)
		}
	}
}

func TestBareWriteStripsBaseline(t *testing.T) {
	rw := httptest.NewRecorder()
	// Simulate the core pre-handler baseline (app.go) that must NOT leak
	// into express.json-rejection 400s (pinned: no CSP / nosniff / cookie).
	for _, kv := range [][2]string{
		{"Content-Security-Policy", "base-uri 'none'"},
		{"X-Content-Type-Options", "nosniff"},
		{"Referrer-Policy", "origin-when-cross-origin"},
		{"Cache-Control", "no-store"},
		{"Set-Cookie", "overleaf.sid=s:abc; Path=/; HttpOnly"},
		{"X-Powered-By", "Express"},
	} {
		rw.Header().Set(kv[0], kv[1])
	}
	res := &core.Res{W: rw}
	res.BareWrite(400, []byte(`{}`))
	if got := rw.Code; got != 400 {
		t.Fatalf("status=%d want 400", got)
	}
	if got := rw.Body.String(); got != `{}` {
		t.Fatalf("body=%q want `{}`", got)
	}
	for _, h := range []string{ // must be stripped
		"Content-Security-Policy", "X-Content-Type-Options",
		"Referrer-Policy", "Cache-Control", "Set-Cookie",
	} {
		if v := rw.Header().Get(h); v != "" {
			t.Fatalf("header %s=%q should be absent", h, v)
		}
	}
	if v := rw.Header().Get("Content-Type"); v != "application/json; charset=utf-8" {
		t.Fatalf("content-type=%q", v)
	}
	if v := rw.Header().Get("X-Powered-By"); v != "Express" {
		t.Fatalf("x-powered-by=%q", v)
	}
	if v := rw.Header().Get("Content-Length"); v != "2" {
		t.Fatalf("content-length=%q", v)
	}
	if rw.Header().Get("ETag") == "" {
		t.Fatal("ETag missing")
	}
}

func TestCreateContentIssues(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"placements":["editor"]}`,
			`{"error":"Validation error: Invalid input: expected string, received undefined at \"body.content\"","statusCode":400}`},
		{`{"content":123}`,
			`{"error":"Validation error: Invalid input: expected string, received number at \"body.content\"","statusCode":400}`},
		{`{"zz":1}`,
			`{"error":"Validation error: Invalid input: expected string, received undefined at \"body.content\"; Unrecognized key: \"zz\" at \"body\"","statusCode":400}`},
		{`{"content":"a","zzz":1,"aaa":2}`,
			`{"error":"Validation error: Unrecognized keys: \"zzz\", \"aaa\" at \"body\"","statusCode":400}`},
		{`{"content":5,"placements":["bogus"],"junk":1}`,
			`{"error":"Validation error: Invalid input: expected string, received number at \"body.content\"; Invalid option: expected one of \"editor\"|\"hub\"|\"auth\" at \"body.placements[0]\"; Unrecognized key: \"junk\" at \"body\"","statusCode":400}`},
	}
	for _, c := range cases {
		b, keyOrder := parseForTest(t, c.body)
		issues := createIssues(b, keyOrder)
		got := string(validationError(joinIssues(issues)))
		if got != c.want {
			t.Fatalf("\ngot:  %s\nwant: %s", got, c.want)
		}
	}
}

func TestPlacementsIssues(t *testing.T) {
	// valid array (≤4, all enum) → no issues
	if _, bad := placementsIssues([]any{"editor", "hub", "auth"}); bad {
		t.Fatalf("valid placements must not produce issues")
	}
	// string → type issue; >4 chars adds the characters-too-big issue
	one, _ := placementsIssues("ed")
	if len(one) != 1 || one[0] != `Invalid input: expected array, received string at \"body.placements\"` {
		t.Fatalf("short string: %+v", one)
	}
	two, _ := placementsIssues("editor")
	if len(two) != 2 {
		t.Fatalf("long string needs 2 issues: %+v", two)
	}
	if two[1] != `Too big: expected string to have <=4 characters at \"body.placements\"` {
		t.Fatalf("too-big string: %+v", two)
	}
	// array >4 → items too-big
	five, _ := placementsIssues([]any{"editor", "hub", "auth", "editor", "hub"})
	want := `Too big: expected array to have <=4 items at \"body.placements\"`
	if len(five) != 1 || five[0] != want {
		t.Fatalf("5 valid: %+v", five)
	}
	// 4 with one bad → element issue only
	mixed, _ := placementsIssues([]any{"editor", "hub", "auth", "xxx"})
	if len(mixed) != 1 || mixed[0] != `Invalid option: expected one of \"editor\"|\"hub\"|\"auth\" at \"body.placements[3]\"` {
		t.Fatalf("mixed: %+v", mixed)
	}
	// number → type issue, no size issue
	num, _ := placementsIssues(float64(5))
	if len(num) != 1 || num[0] != `Invalid input: expected array, received number at \"body.placements\"` {
		t.Fatalf("number: %+v", num)
	}
	// null → type issue (null)
	got, _ := placementsIssues(nil)
	if len(got) != 1 || got[0] != `Invalid input: expected array, received null at \"body.placements\"` {
		t.Fatalf("null: %+v", got)
	}
}

func TestCloseEditorIssues(t *testing.T) {
	// missing isOpen → no issue (optional)
	body, order := parseForTest(t, `{}`)
	if u, bad := unrecognized(body, order, "isOpen"); bad {
		t.Fatalf("empty body: %q", u)
	}
	// wrong type
	body, order = parseForTest(t, `{"isOpen":-1,"xx":"y"}`)
	var issues []string
	isOpen, present := body["isOpen"]
	if present && !isBool(isOpen) {
		issues = append(issues, `Invalid input: expected boolean, received `+zodType(isOpen)+` at \"body.isOpen\"`)
	}
	if u, bad := unrecognized(body, order, "isOpen"); bad {
		issues = append(issues, u)
	}
	want := `Invalid input: expected boolean, received number at \"body.isOpen\"; Unrecognized key: \"xx\" at \"body\"`
	if joinIssues(issues) != want {
		t.Fatalf("got %q want %q", joinIssues(issues), want)
	}
}

// parseForTest mirrors readBody for unit tests (no request).
func parseForTest(t *testing.T, body string) (map[string]any, []string) {
	t.Helper()
	r := httptest.NewRequest("POST", "/admin/messages", strings.NewReader(body))
	b, order, _, ok := readBody(r)
	if !ok {
		t.Fatalf("expected object body for %q", body)
	}
	return b, order
}

func TestFinishActionWants(t *testing.T) {
	cases := []struct {
		accept, xrw string
		wantJSON    bool
	}{
		{"text/html,*/*", "", false},
		{"text/html,*/*", "XMLHttpRequest", true},
		{"application/json", "", true},
		{"application/json, text/plain, */*", "", true},
	}
	for _, c := range cases {
		r := httptest.NewRequest("PATCH", "/admin/messages/x", nil)
		r.Header.Set("Accept", c.accept)
		if c.xrw != "" {
			r.Header.Set("X-Requested-With", c.xrw)
		}
		xhr := r.Header.Get("X-Requested-With") == "XMLHttpRequest"
		got := xhr || containsJSON(r.Header.Get("Accept"))
		if got != c.wantJSON {
			t.Fatalf("accept=%q xrw=%q: got %v want %v", c.accept, c.xrw, got, c.wantJSON)
		}
	}
}

func TestEditorStateBodyShape(t *testing.T) {
	// editor-state JSON key order + booleans (pinned:
	// {"editorIsOpen":true,"siteIsOpen":true}).
	for _, e := range []bool{true, false} {
		s := `{"editorIsOpen":` + boolJSON(e) + `,"siteIsOpen":` + boolJSON(true) + `}`
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatal(err)
		}
	}
}

func containsJSON(accept string) bool {
	return strings.Contains(accept, "application/json")
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
