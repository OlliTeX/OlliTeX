package views

import (
	"strings"
	"testing"
)

// U9: AdminShell slot splicing must leave no placeholder residue and must
// pin the llm on/off branches + msg escaping exactly.

func TestAdminShellSlotsFull(t *testing.T) {
	p := AdminShellParams{
		Nonce:        "N-ONCE",
		CSRF:         "CSRF-VAL",
		Email:        "admin@e2e.test",
		UID:          "6aa4b8a873ef0e5094f4cba3",
		OverallTheme: "light-",
		Origin:       "http://127.0.0.1:7420",
		CurrentURL:   "/admin",
		Exposed:      `{"a":1}`,
		LLMEnabled:   true,
		SystemMessages: []string{
			`plain`,
			`a<b & "q" 'x'`, // pug escape order
		},
	}
	out := AdminShell(p)
	for _, slot := range []string{"__NONCE__", "__CSRF__", "__EMAIL__", "__UID__", "__OVERALLTHEME__", "__ORIGIN__", "__CURRENTURL__", "__EXPOSED__", "__LLMHDR__", "__LLMPANE__", "__SYSMSGS__"} {
		if strings.Contains(out, slot) {
			t.Fatalf("residual slot %q", slot)
		}
	}
	want := []string{
		`name="ol-csrfToken" content="CSRF-VAL"`,
		`value="CSRF-VAL"`,
		`nonce="N-ONCE"`,
		`<div class="disabled dropdown-item">admin@e2e.test</div>`,
		`name="ol-user_id" content="6aa4b8a873ef0e5094f4cba3"`,
		`name="ol-adminOverallTheme" content="light-"`,
		`<link rel="alternate" href="http://127.0.0.1:7420/admin" hreflang="en">`,
		`content="{&quot;a&quot;:1}"`,
		// llm ON: tab + pane
		`href="#llm-configuration" aria-controls="llm-configuration"`,
		`<iframe src="/admin/llm/settings" title="LLM Configuration" style="width: 100%; height: calc(100vh - 280px); min-height: 480px; border: 0;"></iframe>`,
		// sysmsg rows (one <ul> per message, pug-escaped)
		`<ul class="system-messages"><li class="system-message row-spaced">plain</li></ul>`,
		`<ul class="system-messages"><li class="system-message row-spaced">a&lt;b &amp; &quot;q&quot; &#39;x&#39;</li></ul>`,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q", w)
		}
	}
	// csrf meta appears once; form inputs 6 (navbar logout + 5 admin forms)
	if got := strings.Count(out, `value="CSRF-VAL"`); got != 6 {
		t.Fatalf("csrf inputs = %d, want 6", got)
	}
	// the 6 backticks of the Node comments survive verbatim
	if got := strings.Count(out, "`"); got != 10 {
		t.Fatalf("backticks = %d, want 10 (Node oracle)", got)
	}
}

func TestAdminShellLLMOff(t *testing.T) {
	p := AdminShellParams{Nonce: "n", CSRF: "c", LLMEnabled: false}
	out := AdminShell(p)
	if strings.Contains(out, "#llm-configuration") {
		t.Fatal("llm tab must be ABSENT when llmEnabled=false (Node: if (llmEnabled) header)")
	}
	if !strings.Contains(out, `<p class="text-muted">LLM is disabled on this deployment (set LLM_ENABLED=true to enable).</p>`) {
		t.Fatal("missing the disabled-llm pane note")
	}
}

func TestAdminShellEmptyTheme(t *testing.T) {
	p := AdminShellParams{OverallTheme: ""}
	out := AdminShell(p)
	if !strings.Contains(out, `name="ol-adminOverallTheme" content=""`) {
		t.Fatal("adminOverallTheme defaults to empty string (Node: ?? '')")
	}
}

// pugEscape pins the Node (Pug) entity order & set.
func TestPugEscapeOrder(t *testing.T) {
	got := pugEscape(`<a href="x">&it's</a>`)
	want := `&lt;a href=&quot;x&quot;&gt;&amp;it&#39;s&lt;/a&gt;`
	if got != want {
		t.Fatalf("pugEscape = %q, want %q", got, want)
	}
}
