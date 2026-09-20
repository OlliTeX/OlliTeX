package projectlist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCrParseTypstBody(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		bare bool
		zod  string // expected zodMsg ("* ok" = ok, "" = not-zod/bare handled by bare)
		want *crBody
	}{
		{"empty", "", false, "ok", &crBody{}},
		{"name only", `{"projectName":"  x  "}`, false, "ok", &crBody{projectName: "  x  ", projectPresent: true}},
		{"extra keys OK (non-strict)", `{"projectName":"x","zz":1,"other":"y"}`, false, "ok", &crBody{projectName: "x", projectPresent: true}},
		{"name number", `{"projectName":5}`, false, `Invalid input: expected string, received number at "body.projectName"`, nil},
		{"tmpl number", `{"template":123}`, false, `Invalid input: expected string, received number at "body.template"`, nil},
		{"name null", `{"projectName":null}`, false, `Invalid input: expected string, received null at "body.projectName"`, nil},
		{"both bad order", `{"projectName":5,"template":6}`, false,
			`Invalid input: expected string, received number at "body.projectName"; Invalid input: expected string, received number at "body.template"`, nil},
		{"name 101", `{"projectName":"` + repeatStr("q", 101) + `"}`, false,
			`Too big: expected string to have <=100 characters at "body.projectName"`, nil},
		{"name 100 ok", `{"projectName":"` + repeatStr("q", 100) + `"}`, false, "ok", &crBody{projectName: repeatStr("q", 100), projectPresent: true}},
		{"tmpl 51", `{"template":"` + repeatStr("w", 51) + `"}`, false,
			`Too big: expected string to have <=50 characters at "body.template"`, nil},
		{"tmpl 50 ok (no name)", `{"template":"` + repeatStr("w", 50) + `"}`, false, "ok", &crBody{template: repeatStr("w", 50), tmplPresent: true}},
		{"array", `[1,2]`, false, `Invalid input: expected object, received array at "body"`, nil},
		{"bady", `{bad`, true, "", nil},
		{"str root", `"hi"`, true, "", nil},
		{"null root", `null`, true, "", nil},
		{"empty obj", `{}`, false, "ok", &crBody{}},
		{"utf16 surrogate pair counts 2", `{"projectName":"` + repeatStr("𝄞", 98) + `ab"}`, false,
			`Too big: expected string to have <=100 characters at "body.projectName"`, nil}, // 98*2+2 = 198 utf16 > 100
		{"utf16 boundary 100", `{"projectName":"` + repeatStr("𝄞", 50) + `"}`, false, "ok",
			&crBody{projectName: repeatStr("𝄞", 50), projectPresent: true}}, // 100 utf16 = boundary OK
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := crParseTypstBody([]byte(c.raw))
			if c.bare {
				if !got.bare {
					t.Fatalf("want bare, got %+v", got)
				}
				return
			}
			if got.zodMsg != "" {
				if got.zodMsg != c.zod {
					t.Fatalf("zod:\n got %q\nwant %q", got.zodMsg, c.zod)
				}
				return
			}
			if !got.ok {
				t.Fatalf("want ok, got %+v", got)
			}
			if got.body != nil && c.want != nil {
				if got.body.projectName != c.want.projectName || got.body.projectPresent != c.want.projectPresent ||
					got.body.template != c.want.template || got.body.tmplPresent != c.want.tmplPresent {
					t.Fatalf("body %+v want %+v", got.body, c.want)
				}
			}
		})
	}
}

func repeatStr(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func TestCrTypstDocLines(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(dir+"/article", 0755)
	os.MkdirAll(dir+"/example", 0755)
	// lodash does NOT HTML-escape interpolation values (pinned: live rendered
	// docs contain raw <, >, &, ").
	os.WriteFile(dir+"/mainbasic.typ", []byte("= <%= project_name %>\n\ntext\n"), 0644)
	os.WriteFile(dir+"/article/main.typ", []byte("#set\n= <%= project_name %>\n"), 0644)
	os.WriteFile(dir+"/article/references.bib", []byte("@article{k,\n}\n"), 0644)
	os.WriteFile(dir+"/example/main.typ", []byte("// <%= project_name %> never here in real file, but render must not crash\n"), 0644)
	os.WriteFile(dir+"/example/sample.bib", []byte("@article{g,\n}\n"), 0644)
	os.WriteFile(dir+"/example/frog.jpg", []byte("JPEGBYTES"), 0644)

	got := crTypstDocLines(dir, "mainbasic.typ", `Pin&A B<T>`)
	if len(got) != 4 || got[0] != "= Pin&A B<T>" || got[1] != "" || got[2] != "text" || got[3] != "" {
		t.Fatalf("render: %q", got)
	}
	// missing file → empty (same as Node read fail path would 500 in the
	// handler; the helper itself returns empty).
	if got := crTypstDocLines(dir, "nope.typ", "x"); len(got) != 0 {
		t.Fatalf("missing: %q", got)
	}
	// trailing newline → split produces the extra empty element (Node parity).
	got2 := crTypstDocLines(dir, "article/main.typ", "Z")
	if len(got2) != 3 || got2[1] != "= Z" || got2[2] != "" {
		t.Fatalf("article: %q", got2)
	}
}

func TestCrGitBlobHashFrogStable(t *testing.T) {
	// The git-blob hash of the same bytes is stable (the e2e stack's frog.jpg
	// hash is pinned in the gate via the fileRef).
	h1 := crGitBlobHash([]byte("abcdef"))
	h2 := crGitBlobHash([]byte("abcdef"))
	if h1 != h2 || len(h1) != 40 {
		t.Fatalf("hash: %s %s", h1, h2)
	}
	_ = filepath.Join
}
