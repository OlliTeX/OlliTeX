package emailtemplates

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// ---- registry integrity ---------------------------------------------------

func TestRegistryIntegrity(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Registry {
		if s.Name == "" || s.Label == "" || s.Subject == "" || s.Text == "" {
			t.Errorf("slot %q: name/label/subject/text are required", s.Name)
		}
		if seen[s.Name] {
			t.Errorf("duplicate slot %q", s.Name)
		}
		seen[s.Name] = true
		if _, ok := Lookup(s.Name); !ok {
			t.Errorf("Lookup(%q) failed", s.Name)
		}
		for _, v := range s.Vars {
			if !validVarRe.MatchString(v) {
				t.Errorf("slot %q: bad var %q", s.Name, v)
			}
		}
		// every {{var}} the defaults reference must be declared
		for _, f := range []string{s.Subject, s.Text, s.HTML} {
			if f == "" {
				continue
			}
			allowed := map[string]bool{}
			for _, v := range s.Vars {
				allowed[v] = true
			}
			for _, m := range varRe.FindAllStringSubmatch(f, -1) {
				if !allowed[m[1]] {
					t.Errorf("slot %q: template references undeclared var %q", s.Name, m[1])
				}
			}
		}
	}
	if got := len(Registry); got != 13 {
		t.Errorf("registry size = %d, want 13", got)
	}
	want := map[string]bool{
		"password-reset": true, "instance-stats-test": true, "sessions-cleared": true,
		"activate-account": true, "mail-config-test": true,
		"collab-access-requested": true, "collab-access-declined": true,
		"collab-access-granted": true, "ownership-transfer": true,
		"project-invite": true, "security-note": true, "git-token": true,
		"test-mail": true,
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("missing slot %q", name)
		}
	}
}

var validVarRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ---- rendering ------------------------------------------------------------

func TestRenderDefaults(t *testing.T) {
	empty := map[string]Override{}
	must := map[string]string{
		"app":  "OlliTeX",
		"link": "http://site.test/reset/123",
	}
	r, err := Render(Registry, empty, "password-reset", must)
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != "Password Reset - OlliTeX" {
		t.Errorf("subject = %q", r.Subject)
	}
	wantText := "We got a request to reset your OlliTeX password.\n\nReset password: http://site.test/reset/123\n\nIf you ignore this message, your password won't be changed.\nIf you didn't request a password reset, let us know."
	if r.Text != wantText {
		t.Errorf("text = %q", r.Text)
	}
	if !strings.Contains(r.HTML, `<a href="http://site.test/reset/123">Reset password</a>`) {
		t.Errorf("html = %q", r.HTML)
	}
}

func TestRenderInviteParity(t *testing.T) {
	// byte-identity with the pre-move caller strings (the OlliTeX brand
	// resolution at app="OlliTeX")
	r, err := Render(Registry, map[string]Override{}, "project-invite", map[string]string{
		"app": "OlliTeX", "project": "My Paper", "owner": "a@b.test",
		"url": "http://site.test/project/abc/invite/token/tok", "site": "http://site.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, "View project: http://site.test/project/abc/invite/token/tok") {
		t.Errorf("text missing view-project line: %q", r.Text)
	}
	if !strings.Contains(r.Text, "You have been invited to an OlliTeX project.") {
		t.Errorf("text brand wrong: %q", r.Text)
	}
	if !strings.Contains(r.Text, "The OlliTeX Team - http://site.test") {
		t.Errorf("text footer wrong: %q", r.Text)
	}
	if !strings.Contains(r.Subject, `"My Paper" — shared by a@b.test`) {
		t.Errorf("subject = %q", r.Subject)
	}
	// HTML: slots substituted, brand resolved, structure intact
	if strings.Contains(r.HTML, "{{") {
		t.Errorf("html has unsubstituted vars:\n%s", r.HTML)
	}
	for _, want := range []string{
		`<span style="font-family: Arial, Helvetica, sans-serif; font-size: 30px; font-weight: bold; color: #04652f;">OlliTeX</span>`,
		"<b>My Paper</b>",
		"View project",
		`"name":"View project"`,
	} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("html missing %q", want)
		}
	}
	// count the URL occurrences (5x in the shipped template: button href,
	// fallback link, ld+json target/targetUrl + one more — pinned against
	// the ORIGINAL invMailHTML slot count)
	if got := strings.Count(r.HTML, "http://site.test/project/abc/invite/token/tok"); got != 5 {
		t.Errorf("invite URL occurrences = %d, want 5", got)
	}
}

func TestRenderOverridePrecedence(t *testing.T) {
	ov := map[string]Override{"test-mail": {
		Subject: "Hello from custom",
		Text:    "custom body {{app}}",
	}}
	r, err := Render(Registry, ov, "test-mail", map[string]string{"app": "OlliTeX", "site": "http://s"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != "Hello from custom" {
		t.Errorf("override subject not applied: %q", r.Subject)
	}
	if r.Text != "custom body OlliTeX" {
		t.Errorf("override text not applied: %q", r.Text)
	}
	if !strings.Contains(r.HTML, "a test Email from OlliTeX") {
		t.Errorf("default html must still apply: %q", r.HTML)
	}
	// other slots untouched
	r2, _ := Render(Registry, ov, "test-mail", map[string]string{})
	_ = r2
}

func TestRenderMissingVarIsEmpty(t *testing.T) {
	r, err := Render(Registry, map[string]Override{}, "instance-stats-test", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != "[] Instance stats alert test" {
		t.Errorf("missing var should render empty: %q", r.Subject)
	}
}

func TestRenderUnknownSlot(t *testing.T) {
	if _, err := Render(Registry, nil, "nope", nil); err == nil {
		t.Fatal("want error for unknown slot")
	}
}

func TestUnknownVariableRejected(t *testing.T) {
	ov := map[string]Override{"test-mail": {Subject: "Hi {{bogus}}"}}
	_, err := Render(Registry, ov, "test-mail", map[string]string{"app": "x", "site": "y"})
	if err == nil {
		t.Fatal("want unknown-variable error")
	}
	ve, ok := err.(*VarError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if len(ve.Unknown) != 1 || ve.Unknown[0] != "bogus" {
		t.Errorf("Unknown = %v", ve.Unknown)
	}
	if !strings.Contains(ve.Error(), "allowed:") {
		t.Errorf("message = %q", ve.Error())
	}
}

// ---- store ----------------------------------------------------------------

func TestMapStore(t *testing.T) {
	ctx := context.Background()
	s := NewMapStore()
	got, _ := s.GetOverride(ctx, "x")
	if got.Subject != "" {
		t.Fatalf("want empty override")
	}
	s.SetOverride(ctx, "x", Override{Subject: "s", Text: "t"})
	got, _ = s.GetOverride(ctx, "x")
	if got.Subject != "s" || got.Text != "t" {
		t.Fatalf("got %+v", got)
	}
	all, _ := s.LoadAll(ctx)
	if len(all) != 1 || all["x"].Subject != "s" {
		t.Fatalf("all = %+v", all)
	}
	s.ClearOverride(ctx, "x")
	all, _ = s.LoadAll(ctx)
	if len(all) != 0 {
		t.Fatalf("all = %+v", all)
	}
}

// ---- CollabHTML parity ----------------------------------------------------

func TestCollabHTML(t *testing.T) {
	// exactly the Node port's behavior: only & is escaped (collab.go)
	got := CollabHTML("a & b")
	if got != "<p>a &amp; b</p>" {
		t.Errorf("got %q", got)
	}
	if CollabHTML("plain") != "<p>plain</p>" {
		t.Errorf("got %q", CollabHTML("plain"))
	}
}
