package tags

import (
	"regexp"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------- VA message matrix (pinned against the live Node 400s) ----------

func TestCreateIssuesMissingName(t *testing.T) {
	msg, name, color, ok := createEditIssues(map[string]any{}, nil)
	if ok || name != "" || color != "" {
		t.Fatalf("expected failure, got ok=%v name=%q color=%q", ok, name, color)
	}
	want := `Invalid input: expected string, received undefined at \"body.name\"`
	if msg != want {
		t.Fatalf("message:\n got  %s\n want %s", msg, want)
	}
}

func TestCreateIssuesNameType(t *testing.T) {
	msg, _, _, ok := createEditIssues(map[string]any{"name": float64(5), "zz": 1}, []string{"name", "zz"})
	if ok {
		t.Fatal("expected failure")
	}
	want := `Invalid input: expected string, received number at \"body.name\"; Unrecognized key: \"zz\" at \"body\"`
	if msg != want {
		t.Fatalf("message:\n got  %s\n want %s", msg, want)
	}
}

func TestCreateIssuesEmptyName(t *testing.T) {
	msg, _, _, ok := createEditIssues(map[string]any{"name": ""}, nil)
	if ok {
		t.Fatal("expected failure")
	}
	want := `Too small: expected string to have >=1 characters at \"body.name\"`
	if msg != want {
		t.Fatalf("message:\n got  %s\n want %s", msg, want)
	}
}

func TestCreateIssuesColorPattern(t *testing.T) {
	cases := []struct {
		color string
		ok    bool
	}{
		{"#a1b2c3", true},
		{"#A1B2C3", true}, // [a-fA-F0-9]
		{"#a1b2c", false},
		{"hsl(120, 70%, 45%)", true}, // the exact auto-generated form
		{"hsl(120,70%,45%)", false},
		{"hsl(12, 70%, 45%)", true},
		{"hsl(012, 70%, 45%)", true},
		{"hsl(120, 69%, 45%)", false},
		{"notacolor", false},
		{"", false},
	}
	for _, c := range cases {
		_, _, _, ok := createEditIssues(map[string]any{"name": "x", "color": c.color}, nil)
		if ok != c.ok {
			t.Fatalf("color %q: got ok=%v want %v", c.color, ok, c.ok)
		}
	}
}

func TestCreateIssuesColorPatternMessage(t *testing.T) {
	msg, _, _, ok := createEditIssues(map[string]any{"name": "x", "color": "notacolor"}, nil)
	if ok {
		t.Fatal("expected failure")
	}
	want := `Invalid string: must match pattern /^(#[a-fA-F0-9]{6}|hsl\\(\\d{1,3}, 70%, 45%\\))$/ at \"body.color\"`
	if msg != want {
		t.Fatalf("message:\n got  %s\n want %s", msg, want)
	}
}

func TestCreateIssuesUnknownsInOrder(t *testing.T) {
	msg, _, _, ok := createEditIssues(map[string]any{"name": "x", "b": 1, "a": 2}, []string{"name", "b", "a"})
	if ok {
		t.Fatal("expected failure")
	}
	want := `Unrecognized keys: \"b\", \"a\" at \"body\"`
	if msg != want {
		t.Fatalf("message:\n got  %s\n want %s", msg, want)
	}
}

func TestRenameIssues(t *testing.T) {
	msg, _, ok := renameIssues(map[string]any{"name": "x", "color": 3}, []string{"name", "color"})
	if ok {
		t.Fatal("expected failure")
	}
	want := `Unrecognized key: \"color\" at \"body\"`
	if msg != want {
		t.Fatalf("message:\n got  %s\n want %s", msg, want)
	}
	if _, _, ok = renameIssues(map[string]any{"name": "ok"}, nil); !ok {
		t.Fatal("expected success")
	}
}

func TestProjectIdsIssues(t *testing.T) {
	good := "aaaaaaaaaaaaaaaaaaaaaaaa"
	_, ids, ok := projectIdsIssues(map[string]any{"projectIds": []any{good}}, nil)
	if !ok {
		t.Fatal("expected success")
	}
	if len(ids) != 1 || ids[0] != good {
		t.Fatalf("ids: %v", ids)
	}
	msg, _, ok := projectIdsIssues(map[string]any{"projectIds": []any{good, "nope"}}, nil)
	if ok {
		t.Fatal("expected failure")
	}
	want := `Invalid Mongo ObjectId at \"body.projectIds[1]\"`
	if msg != want {
		t.Fatalf("message:\n got  %s\n want %s", msg, want)
	}
	msg, _, ok = projectIdsIssues(map[string]any{}, nil)
	if ok {
		t.Fatal("expected failure")
	}
	want = `Invalid input: expected array, received undefined at \"body.projectIds\"`
	if msg != want {
		t.Fatalf("message:\n got  %s\n want %s", msg, want)
	}
}

func TestJstype(t *testing.T) {
	cases := map[string]any{
		"null":    nil,
		"string":  "x",
		"number":  float64(1),
		"boolean": true,
		"array":   []any{},
		"object":  map[string]any{},
	}
	for want, v := range cases {
		if got := jstype(v); got != want {
			t.Fatalf("jstype(%v) = %s want %s", v, got, want)
		}
	}
}

// ---------- doc JSON ----------

func TestDJSONTagStoredOrder(t *testing.T) {
	oid, _ := primitive.ObjectIDFromHex("6ab225a7db71f85bd15ffaae")
	d := primitive.D{
		{Key: "_id", Value: oid},
		{Key: "user_id", Value: "6aa4b8a873ef0e5094f4cba3"},
		{Key: "name", Value: "p7u1-alpha"},
		{Key: "color", Value: "#a1b2c3"},
		{Key: "project_ids", Value: primitive.A{"6ab225bedb71f85bd15ffaeb"}},
		{Key: "__v", Value: int32(0)},
	}
	got := string(dJSONTag(d))
	want := `{"_id":"6ab225a7db71f85bd15ffaae","user_id":"6aa4b8a873ef0e5094f4cba3","name":"p7u1-alpha","color":"#a1b2c3","project_ids":["6ab225bedb71f85bd15ffaeb"],"__v":0}`
	if got != want {
		t.Fatalf("doc json:\n got  %s\n want %s", got, want)
	}
}

func TestDJSONTagNoColor(t *testing.T) {
	oid, _ := primitive.ObjectIDFromHex("6ab225a7db71f85bd15ffab1")
	d := primitive.D{
		{Key: "_id", Value: oid},
		{Key: "user_id", Value: "u"},
		{Key: "name", Value: "n"},
		{Key: "project_ids", Value: primitive.A{}},
		{Key: "__v", Value: int32(0)},
	}
	got := string(dJSONTag(d))
	want := `{"_id":"6ab225a7db71f85bd15ffab1","user_id":"u","name":"n","project_ids":[],"__v":0}`
	if got != want {
		t.Fatalf("doc json:\n got  %s\n want %s", got, want)
	}
}

func TestMalformedParam(t *testing.T) {
	got := string(malformedParam("tagId"))
	want := `{"error":"Validation error: Invalid Mongo ObjectId at \"params.tagId\"","statusCode":404}`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	got = string(malformedParam("projectId"))
	want = `{"error":"Validation error: Invalid Mongo ObjectId at \"params.projectId\"","statusCode":404}`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestPatterns(t *testing.T) {
	cases := []struct {
		re   *regexp.Regexp
		path string
		ok   bool
	}{
		{renamePat, "/tag/abc/rename", true},
		{renamePat, "/tag/abc/renames", false},
		{editPat, "/tag/abc/edit", true},
		{tagIdPat, "/tag/abc", true},
		{memberPat, "/tag/a/project/b", true},
		{memberPat, "/tag/a/projects", false},
		{addManyPat, "/tag/a/projects", true},
		{addManyPat, "/tag/a/project/b", false},
		{removeManyPat, "/tag/a/projects/remove", true},
		{removeManyPat, "/tag/a/projects", false},
		{privTagPat, "/user/u/tag", true},
		{privTagPat, "/user/u/tags", false},
	}
	for _, c := range cases {
		if c.re.MatchString(c.path) != c.ok {
			t.Fatalf("pattern %s vs %s: got %v want %v", c.re, c.path, !c.ok, c.ok)
		}
	}
}
