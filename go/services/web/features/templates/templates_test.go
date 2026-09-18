package templates

import (
	"testing"
)

// Node JSON.stringify string escaping (the pinned corpus contains a
// trailing newline in author and a <p>-wrapped description).
func TestNodeJSONString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", `"plain"`},
		{`quo"te`, `"quo\"te"`},
		{"a/b", `"a/b"`},
		{"e2e-fixture\n", `"e2e-fixture\n"`},
		{"\t", `"\t"`},
		{`\`, `"\\"`},
		{">a&b<", `">a&b<"`}, // HTML chars NOT escaped (unlike Go default)
	}
	for _, c := range cases {
		if got := nodeJSONString(c.in); got != c.want {
			t.Errorf("nodeJSONString(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestCleanHtmlOptions(t *testing.T) {
	src := `<p>OlliTeX e2e fixture template</p>` + "\n"
	if got := cleanHtml(src, "plainText"); got != "OlliTeX e2e fixture template\n" {
		t.Errorf("plainText = %q", got)
	}
	if got := cleanHtml(src, "reachText"); got != `<p>OlliTeX e2e fixture template</p>`+"\n" {
		t.Errorf("reachText = %q", got)
	}
	if got := cleanHtml(src, "linksOnly"); got != "OlliTeX e2e fixture template\n" {
		t.Errorf("linksOnly = %q", got)
	}
	link := `<span>x</span><a href="https://ex.org/x">Author</a>`
	// linksOnly DROPS tag structure but KEEPS text (span text stays)
	if got := cleanHtml(link, "linksOnly"); got != `x<a href="https://ex.org/x">Author</a>` {
		t.Errorf("linksOnly(a) = %q", got)
	}
	if got := cleanHtml(link, "plainText"); got != "xAuthor" {
		t.Errorf("plainText(a) = %q", got)
	}
	// real flow: marked CustomRenderer strips raw HTML BEFORE cleanHtml
	// (so script never reaches it); h1 is in the reachText keep set
	if got := cleanHtml("<h1>Title</h1>", "reachText"); got != "<h1>Title</h1>" {
		t.Errorf("reachText(h1) = %q", got)
	}
	if got := cleanHtml("<h1>Title</h1>", "linksOnly"); got != "Title" {
		t.Errorf("linksOnly(h1) = %q", got)
	}
}

func TestNameSanitize(t *testing.T) {
	// Node oracle: name.replace(/[/:*?"<>|\s]+/g, '_')
	if got := tplNameSanitize("Parity Fixture Template"); got != "Parity_Fixture_Template" {
		t.Errorf("san1 = %q", got)
	}
	// runs of bad chars collapse to a single '_'
	if got := tplNameSanitize("ab/*:<>|c	d"); got != "ab_c_d" {
		t.Errorf("san2 = %q", got)
	}
	if got := tplNameSanitize("trail /"); got != "trail_" {
		t.Errorf("san3 = %q", got)
	}
	if got := tplNameSanitize("a  b\tc"); got != "a_b_c" {
		t.Errorf("san4 = %q", got)
	}
	if got := tplNameSanitize("<root>"); got != "_root_" {
		t.Errorf("san5 = %q", got)
	}
}

func TestTimeHelpers(t *testing.T) {
	if got := tplStringVal(int32(847)); got != "847" {
		t.Errorf("int32 = %q", got)
	}
}
