package cronmail

import (
	"encoding/json"
	"os"
	"testing"
)

// TestRenderGolden pins the Go renderers to the NODE oracle: testdata
// golden.json was produced by the real Node EmailBuilder
// (services/web/app/src/Features/Email/EmailBuilder.mjs) under
// APP_NAME=OlliTeX, PUBLIC_URL=http://127.0.0.1:4000. Every fixture must
// match byte-for-byte on subject, html and text.
func TestRenderGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		Env      json.RawMessage `json:"env"`
		Fixtures []struct {
			Type    string         `json:"type"`
			Opts    map[string]any `json:"opts"`
			Subject string         `json:"subject"`
			HTML    string         `json:"html"`
			Text    string         `json:"text"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}
	if len(golden.Fixtures) == 0 {
		t.Fatal("golden has no fixtures")
	}

	// oracle env: renderers must run under the identical settings
	s := Settings{AppName: "OlliTeX", SiteURL: "http://127.0.0.1:4000", Env: "server-ce"}

	for i, f := range golden.Fixtures {
		projectName, _ := f.Opts["projectName"].(string)
		projectId, _ := f.Opts["projectId"].(string)
		isComment, _ := f.Opts["isComment"].(bool)

		got, err := render(s, f.Type, projectName, projectId, isComment)
		if err != nil {
			t.Fatalf("[%d] %s: render: %v", i, f.Type, err)
		}
		if got.Subject != f.Subject {
			t.Errorf("[%d] subject mismatch:\n got  %q\n want %q", i, got.Subject, f.Subject)
		}
		if got.Text != f.Text {
			t.Errorf("[%d] text mismatch:\n got  %q\n want %q", i, got.Text, f.Text)
		}
		if got.HTML != f.HTML {
			t.Errorf("[%d] html mismatch (%d vs %d bytes)\n got  %.600q...\n want %.600q...",
				i, len(got.HTML), len(f.HTML), got.HTML, f.HTML)
			diffAt(got.HTML, f.HTML, t)
		}
	}
}

// diffAt prints the first divergence index to speed up splice debugging.
func diffAt(got, want string, t *testing.T) {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for k := 0; k < n; k++ {
		if got[k] != want[k] {
			lo := k - 40
			if lo < 0 {
				lo = 0
			}
			hi := k + 40
			if hi > len(got) {
				hi = len(got)
			}
			whi := k + 40
			if whi > len(want) {
				whi = len(want)
			}
			t.Logf("first diff @ %d:\n  got[%d:%d]=%q\n  want[%d:%d]=%q",
				k, lo, hi, got[lo:hi], lo, whi, want[lo:whi])
			return
		}
	}
	t.Logf("prefix identical; len diff (got %d, want %d)\n  got tail  %q\n  want tail %q",
		len(got), len(want), tail(got, 80), tail(want, 80))
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// TestEscapePins — the lodash-_.escape mapping (order-sensitive: & first).
func TestEscapePins(t *testing.T) {
	cases := []struct{ in, want string }{
		{`a & b`, `a &amp; b`},
		{`<tag>`, `&lt;tag&gt;`},
		{`"q"`, `&quot;q&quot;`},
		{`'a'`, `&#39;a&#39;`},
		{`&amp;`, `&amp;amp;`}, // already-escaped input escapes the &
	}
	for _, c := range cases {
		if got := escape(c.in); got != c.want {
			t.Errorf("escape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCleanEntityPins — the oracle-pinned sanitize behavior on the
// template domain (lodash-escaped strings, no tags).
func TestCleanEntityPins(t *testing.T) {
	in := `A &amp; B &lt;tag&gt; &quot;q&quot; &#39;a&#39;`
	want := "A &amp; B &lt;tag&gt; \"q\" 'a'"
	if got := cleanEntityText(in); got != want {
		t.Errorf("cleanEntityText: got %q want %q", got, want)
	}
}

// TestJsonForScriptPins — JS key order + script-safety escapes.
func TestJsonForScriptPins(t *testing.T) {
	g := gmailAction{
		Target:      "http://x/p/1",
		Name:        "View comment",
		Description: `A & B "<t>" 'c'`,
	}
	got := jsonForScript(g)
	// node: JSON.stringify(...).replace(/[\u2028\u2029\u0026\u003e\u003c]/g)
	// -> & -> \u0026, < -> \u003c, > -> \u003e (quotes escaped by stringify)
	want := `{"@context":"http://schema.org","@type":"EmailMessage","potentialAction":{"@type":"ViewAction","target":"http://x/p/1","url":"http://x/p/1","name":"View comment"},"description":"A \u0026 B \"\u003ct\u003e\" 'c'"}`
	if got != want {
		t.Errorf("jsonForScript:\n got  %s\n want %s", got, want)
	}
}

// TestRenderUnknownType — unknown emailTypes are a hard error (the node
// chain would crash with 'scheduled email notification is missing emailType'
// or an unknown-template miss; dispatch marks the doc failed either way).
func TestRenderUnknownType(t *testing.T) {
	s := Settings{AppName: "OlliTeX", SiteURL: "http://x:80", Env: "server-ce"}
	if _, err := render(s, "mysteryType", "p", "id", false); err == nil {
		t.Fatal("expected error for unknown emailType")
	}
}
