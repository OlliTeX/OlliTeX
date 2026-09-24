package history

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// pattern coverage: case-insensitive segments + trailing slash tolerance
// (Node Express defaults).
func TestPatterns(t *testing.T) {
	type caseT struct {
		re *regexp.Regexp
		in string
		ok bool
	}
	cases := []caseT{
		{updatesPat, "/project/6aa4ba9c73ef0e5094f4ce33/updates", true},
		{updatesPat, "/Project/6AA4BA9C73EF0E5094F4CE33/UPDATES", true},
		{updatesPat, "/project/6aa4ba9c73ef0e5094f4ce33/updates/", true},
		{updatesPat, "/project/6aa4ba9c73ef0e5094f4ce33/update", false},
		{updatesPat, "/project/notanid/updates", true}, // Node: param matches, controller 404-JSONs
		{diffPat, "/project/6aa4ba9c73ef0e5094f4ce33/DIFF", true},
		{docDiffPat, "/project/6aa4ba9c73ef0e5094f4ce33/doc/6aafffd0abcdef000000aabb/diff", true},
		{docDiffPat, "/project/6aa4ba9c73ef0e5094f4ce33/doc/baddoc/diff", true}, // param matches; controller 404-JSONs
		{filetreePat, "/project/6aa4ba9c73ef0e5094f4ce33/filetree/diff", true},
		{latestPat, "/project/6aa4ba9c73ef0e5094f4ce33/latest/history", true},
		{changesPat, "/project/6aa4ba9c73ef0e5094f4ce33/changes", true},
		{labelsPat, "/project/6aa4ba9c73ef0e5094f4ce33/LABELS", true},
		{labelDelPat, "/project/6aa4ba9c73ef0e5094f4ce33/labels/6ab2bbd9c9f8a4f1a9025af6", true},
		{labelDelPat, "/project/6aa4ba9c73ef0e5094f4ce33/labels/badid", true},
		{zipPat, "/project/6aa4ba9c73ef0e5094f4ce33/version/1/zip", true},
		{zipPat, "/project/6aa4ba9c73ef0e5094f4ce33/version/xyz/zip", true},
		{blobPat, "/project/6aa4ba9c73ef0e5094f4ce33/blob/5075e7f8058697019905149437412c45900faf8c", true},
		{flushPat, "/project/6aa4ba9c73ef0e5094f4ce33/flush", true},
		{restorePat, "/project/6aa4ba9c73ef0e5094f4ce33/restore_file", true},
		{revertFilePat, "/project/6aa4ba9c73ef0e5094f4ce33/revert_file", true},
		{revertProjPat, "/project/6aa4ba9c73ef0e5094f4ce33/revert-project", true},
		{revertProjPat, "/project/6aa4ba9c73ef0e5094f4ce33/revert_project", false},
		{updatesPat, "/project/6aa4ba9c73ef0e5094f4ce33/updates-x", false},
	}
	for _, c := range cases {
		if got := c.re.MatchString(c.in); got != c.ok {
			t.Errorf("%s(%q) = %v want %v", c.re, c.in, got, c.ok)
		}
	}
}

// named capture groups (Re2 (?P<name>...)).
func TestPatternParams(t *testing.T) {
	in := "/project/6aa4ba9c73ef0e5094f4ce33/doc/6aafffd0abcdef000000aabb/diff"
	m := docDiffPat.FindStringSubmatch(in)
	if m == nil {
		t.Fatal("no match")
	}
	// submatch order: [full, group1, group2]
	if m[1] != "6aa4ba9c73ef0e5094f4ce33" || m[2] != "6aafffd0abcdef000000aabb" {
		t.Fatalf("params: %v", m)
	}
}

// validation message mirrors (pinned from the live Node oracle).
func TestValidationMessages(t *testing.T) {
	if got := versionIssue("xyz"); got != "Invalid input: expected number, received NaN" {
		t.Errorf("versionIssue(xyz) = %q", got)
	}
	if got := versionIssue("-1"); got != "Too small: expected number to be >=0" {
		t.Errorf("versionIssue(-1) = %q", got)
	}
	if got := versionIssue("1.5"); got != "Invalid input: expected integer, received float" {
		t.Errorf("versionIssue(1.5) = %q", got)
	}
	if got := versionIssue("2"); got != "" {
		t.Errorf("versionIssue(2) = %q", got)
	}

	if got := hashIssue("1111"); got != `Too small: expected string to have >=40 characters at \"params.hash\"` {

		t.Errorf("hashIssue(1111) = %q", got)
	}
	if got := hashIssue("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz0"); got != `Invalid string: must match pattern /^[0-9a-f]*$/ at \"params.hash\"; Too big: expected string to have <=40 characters at \"params.hash\"` {
		t.Errorf("hashIssue(zzz) = %q", got)
	}
	if got := hashIssue("5075e7f8058697019905149437412c45900faf8c"); got != "" {
		t.Errorf("hashIssue(40hex) = %q", got)
	}

	unmarshal := func(js string) map[string]any {
		var bm map[string]any
		_ = json.Unmarshal([]byte(js), &bm)
		return bm
	}
	if got := labelIssues(unmarshal(`{"comment":"x","version":"abc"}`)); got != `Invalid input: expected number, received string at \"body.version\"` {
		t.Errorf("labelIssues = %q", got)
	}
	if got := labelIssues(unmarshal(`{"version":2}`)); got != `Invalid input: expected string, received undefined at \"body.comment\"` {
		t.Errorf("labelIssues2 = %q", got)
	}
	if got := labelIssues(unmarshal(`{"comment":1}`)); got != `Invalid input: expected string, received number at \"body.comment\"; Invalid input: expected number, received undefined at \"body.version\"` {
		t.Errorf("labelIssues3 = %q", got)
	}
	if !strings.Contains(labelIssues(unmarshal(`{"comment":"x","version":2,"extra":1}`)), `Unrecognized key: \"extra\" at \"body\"`) {
		t.Errorf("labelIssues4 = %q", labelIssues(unmarshal(`{"comment":"x","version":2,"extra":1}`)))
	}
}

// opair: order preservation + append semantics.
func TestOpair(t *testing.T) {
	var o opair
	if err := o.UnmarshalJSON([]byte(`{"b":1,"a":"x","c":[1,2]}`)); err != nil {
		t.Fatal(err)
	}
	o.set("d", []byte(`true`))
	out, _ := json.Marshal(o)
	if string(out) != `{"b":1,"a":"x","c":[1,2],"d":true}` {
		t.Fatalf("opair order = %s", out)
	}
	// replace keeps position
	o.set("a", []byte(`"y"`))
	out, _ = json.Marshal(o)
	if string(out) != `{"b":1,"a":"y","c":[1,2],"d":true}` {
		t.Fatalf("opair replace = %s", out)
	}
	// nested via get/set
	var n opair
	_ = n.UnmarshalJSON([]byte(`{"meta":{"users":["u"],"z":0}}`))
	mo := opair{}
	_ = mo.UnmarshalJSON(n.get("meta"))
	mo.set("users", []byte(`[{"first_name":"E","last_name":"U","email":"e@u","id":"u"}]`))
	mb, _ := json.Marshal(mo)
	n.set("meta", mb)
	out, _ = json.Marshal(n)
	want := `{"meta":{"users":[{"first_name":"E","last_name":"U","email":"e@u","id":"u"}],"z":0}}`
	if string(out) != want {
		t.Fatalf("nested = %s", out)
	}
}

// userViewJSON key order (pinned: first_name,last_name,email,id).
func TestUserViewJSON(t *testing.T) {
	u := userView{FirstName: "E2e", LastName: "User", Email: "e@e2e.test", ID: "6aa4b8b573ef0e5094f4cbc0"}
	got := userViewJSON(u)
	want := `{"first_name":"E2e","last_name":"User","email":"e@e2e.test","id":"6aa4b8b573ef0e5094f4cbc0"}`
	if got != want {
		t.Fatalf("userViewJSON = %s", got)
	}
}

func TestParseSince(t *testing.T) {
	if s, e := parseSinceParam(""); e != "" || s != "0" {
		t.Fatalf("empty: %q %q", s, e)
	}
	if _, e := parseSinceParam("abc"); e != "Invalid input: expected number, received NaN" {
		t.Fatalf("abc: %q", e)
	}
	if _, e := parseSinceParam("-3"); e != "Too small: expected number to be >=0" {
		t.Fatalf("-3: %q", e)
	}
	if s, e := parseSinceParam("7"); e != "" || s != "7" {
		t.Fatalf("7: %q %q", s, e)
	}
}

func TestWriteBodyIssues(t *testing.T) {
	unmarshal := func(js string) map[string]any {
		var bm map[string]any
		_ = json.Unmarshal([]byte(js), &bm)
		return bm
	}
	// Node order (revertFileSchema body): version first, then pathname.
	got := writeValidateIssues(true, unmarshal(`{}`))
	want := `Invalid input: expected number, received undefined at \"body.version\"; Invalid input: expected string, received undefined at \"body.pathname\"`
	if got != want {
		t.Fatalf("writeValidateIssues({}) = %q", got)
	}
	got = writeValidateIssues(true, unmarshal(`{"version":"abc"}`))
	if !strings.Contains(got, `expected string, received undefined at \"body.pathname\"`) || !strings.Contains(got, `expected number, received string at \"body.version\"`) {
		t.Fatalf("writeValidateIssues = %q", got)
	}
	if got = writeValidateIssues(true, unmarshal(`{"version":1,"pathname":"/abs"}`)); got != `Path is absolute at \"body.pathname\"` {
		t.Fatalf("abs = %q", got)
	}
	if got = writeValidateIssues(true, unmarshal(`{"version":1,"pathname":"a/../b"}`)); got != `Path traversal detected at \"body.pathname\"` {
		t.Fatalf("trav = %q", got)
	}
	if got = writeValidateIssues(true, unmarshal(`{"version":1,"pathname":""}`)); got != `Path is empty at \"body.pathname\"` {
		t.Fatalf("empty = %q", got)
	}
	if got = writeValidateIssues(true, unmarshal(`{"version":1,"pathname":"sub/file.tex"}`)); got != "" {
		t.Fatalf("valid = %q", got)
	}
	// revertProject has no pathname field
	if got = writeValidateIssues(false, unmarshal(`{"version":1}`)); got != "" {
		t.Fatalf("revertProject = %q", got)
	}
}

// jsType mirrors zod received-words.
func TestJsType(t *testing.T) {
	if jsType("s") != "string" || jsType(true) != "boolean" || jsType(map[string]any{}) != "object" || jsType([]any{}) != "array" || jsType(1.0) != "number" {
		t.Fatal("jsType")
	}
}
