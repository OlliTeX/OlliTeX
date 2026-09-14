package userpages

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMomentUTC(t *testing.T) {
	for _, in := range []struct {
		iso string
		out string
	}{
		{"2026-09-14T08:56:33Z", "14th Sep 2026, 8:56 am"},
		{"2026-09-01T23:05:00Z", "1st Sep 2026, 11:05 pm"},
		{"2026-09-02T09:00:00Z", "2nd Sep 2026, 9:00 am"},
		{"2026-09-03T10:00:00Z", "3rd Sep 2026, 10:00 am"},
		{"2026-09-11T11:00:00Z", "11th Sep 2026, 11:00 am"},
		{"2026-09-21T00:30:00Z", "21st Sep 2026, 12:30 am"},
		{"2026-09-22T01:30:00Z", "22nd Sep 2026, 1:30 am"},
		{"2026-09-23T02:30:00Z", "23rd Sep 2026, 2:30 am"},
		{"2026-09-24T15:30:00Z", "24th Sep 2026, 3:30 pm"},
		{"2000-01-07T12:00:00Z", "7th Jan 2000, 12:00 pm"},
	} {
		parsed, err := time.Parse(time.RFC3339, in.iso)
		if err != nil {
			t.Fatal(err)
		}
		if got := momentUTC(parsed, true); got != in.out {
			t.Errorf("momentUTC(%s) = %q, want %q", in.iso, got, in.out)
		}
	}
	if got := momentUTC(time.Time{}, false); got != "Invalid date" {
		t.Errorf("invalid = %q", got)
	}
}

func TestJsBoolCoercion(t *testing.T) {
	// Node Boolean(): "" 0 null → false; "0" "x" → true
	cases := map[any]bool{
		nil:        false,
		false:      false,
		true:       true,
		"":         false,
		"0":        true,
		"false":    true,
		float64(0): false,
		"x":        true,
	}
	for in, want := range cases {
		if got := jsBool(in); got != want {
			t.Errorf("jsBool(%#v) = %v, want %v", in, got, want)
		}
	}
}

func TestSanitizeControlCharacters(t *testing.T) {
	in := "a\nb\u200bc\t d\uFEFF"
	out := sanitizeCtrl(in)
	// Node: each char → \u + 4-hex lowercase (pinned fact)
	if !strings.Contains(out, `a\u000ab`) {
		t.Errorf("newline escape: %q", out)
	}
	if !strings.Contains(out, `\u200b`) {
		t.Errorf("zwsp escape: %q", out)
	}
	if !strings.Contains(out, `\u0009`) {
		t.Errorf("tab escape: %q", out)
	}
	if !strings.Contains(out, `\ufeff`) {
		t.Errorf("bom escape: %q", out)
	}
	if got := strings.TrimFunc(out, jStrTrim); got == in {
		t.Error("sanitize should change control chars")
	}
}

func TestValidationMatrix(t *testing.T) {
	// Pinned against Node (zod 4.x, REQ_VALIDATION_MODE=enforce-log).
	// want == "" means valid (no issues).
	cases := []struct {
		name string
		want string
	}{
		{`{"role":2}`, `Invalid input: expected string, received number at "body.role"`},
		{`{"role":null}`, `Invalid input: expected string, received null at "body.role"`},
		{`{"email":5}`, `Invalid input: expected string, received number at "body.email"`},
		{`{"first_name":"x"}`, ``},
		{`{"first_name":null}`, ``}, // nullish
		{`{"first_name":"` + strings.Repeat("x", 256) + `"}`,
			`Too big: expected string to have <=255 characters at "body.first_name"`},
		{`{"breadcrumbs":"0","previewTabs":1,"darkModePdf":"2","editorTabs":null}`, ``}, // coerced six
		{`{"unknown1":1}`, `Unrecognized key: "unknown1" at "body"`},
		{`{"unknown1":1,"unknown2":2}`, `Unrecognized keys: "unknown1", "unknown2" at "body"`},
		{`{"customKeybindings":"nope"}`,
			`Invalid input: expected record, received string at "body.customKeybindings"`},
		{`{"customKeybindings":null}`, ``}, // nullish
		{`{"customKeybindings":{"a":5}}`,
			`Invalid input: expected string, received number at "body.customKeybindings.a" or Invalid input: expected null, received number at "body.customKeybindings.a"`},
		{`{"zotero":"junk"}`, `Invalid input: expected object, received string at "body.zotero"`},
		{`{"zotero":{"junk":true}}`, `Unrecognized key: "junk" at "body.zotero"`},
		{`{"zotero":{"enabled":"no"}}`, `Invalid input: expected boolean, received string at "body.zotero.enabled"`},
		{`{"zotero":{"groups":"nope"}}`, `Invalid input: expected array, received string at "body.zotero.groups"`},
		{`{"zotero":{"groups":["x"]}}`, `Invalid input: expected object, received string at "body.zotero.groups[0]"`},
		{`{"zotero":{"groups":[{"id":5}]}}`, `Invalid input: expected string, received number at "body.zotero.groups[0].id"`},
		{`{"zotero":{"groups":[{"id":null}]}}`, `Invalid input: expected string, received null at "body.zotero.groups[0].id"`},
		{`{"zotero":{"groups":[{"junk":1}]}}`, `Invalid input: expected string, received undefined at "body.zotero.groups[0].id"`},
		{`{"zotero":{"groups":[{"id":5},{"id":6}]}}`,
			`Invalid input: expected string, received number at "body.zotero.groups[0].id"; Invalid input: expected string, received number at "body.zotero.groups[1].id"`},
		{`{"email":9,"first_name":"` + strings.Repeat("x", 300) + `","zotero":"junk"}`,
			`Too big: expected string to have <=255 characters at "body.first_name"; Invalid input: expected string, received number at "body.email"; Invalid input: expected object, received string at "body.zotero"`},
		{`{"unknown1":1,"first_name":42}`,
			`Invalid input: expected string, received number at "body.first_name"; Unrecognized key: "unknown1" at "body"`},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/x", strings.NewReader(c.name))
		body, _, _, _, raw := readBody(req)
		got := validateSettings(body, raw)
		if got != c.want {
			t.Errorf("%s\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestValidationErrorMessageShape(t *testing.T) {
	got := validationError(`Invalid input: expected string, received undefined at "body.passwordResetToken"`)
	var body map[string]any
	if err := json.Unmarshal([]byte(got), &body); err != nil {
		t.Fatal(err)
	}
	if body["statusCode"] != float64(400) {
		t.Errorf("statusCode: %v", body["statusCode"])
	}
	// wire shape (Node JSON.stringify): the inner quotes are escaped
	want := `Validation error: Invalid input: expected string, received undefined at "body.passwordResetToken"`
	if body["error"] != want {
		t.Errorf("error: %v", body["error"])
	}
}

func TestSubObjectRawAndOrderedPairs(t *testing.T) {
	raw := []byte(`{"first_name":"x","customKeybindings":{"b":"2","a":"1"}}`)
	sub, ok := subObjectRaw(raw, "customKeybindings")
	if !ok {
		t.Fatal("subObjectRaw not found")
	}
	m, keys, ok := orderedPairs(sub)
	if !ok {
		t.Fatal("orderedPairs")
	}
	if len(keys) != 2 || keys[0] != "b" || keys[1] != "a" {
		t.Errorf("order: %v", keys)
	}
	if m["a"] != "1" || m["b"] != "2" {
		t.Errorf("values: %v", m)
	}
}

func TestRefProviderValidationOrder(t *testing.T) {
	// enabled type error is reported before unknown keys (zod field
	// order: strictObject issues surface per declared field; pinned).
	raw := []byte(`{"zotero":{"enabled":"x","junk":true}}`)
	body, _, _, _, r := readBody(httptest.NewRequest("POST", "/x", bytes.NewReader(raw)))
	_ = r
	msg := validateSettings(body, raw)
	if msg != `Invalid input: expected boolean, received string at "body.zotero.enabled"; Unrecognized key: "junk" at "body.zotero"` {
		t.Errorf("got %q", msg)
	}
}

func TestValidationOrder(t *testing.T) {
	// a field-level issue (first_name wrong type) beats the unknown key
	// (zod reports schema issues before unrecognized keys).
	raw := []byte(`{"first_name":1,"zz":true}`)
	body, _, _, _, _ := readBody(httptest.NewRequest("POST", "/x", bytes.NewReader(raw)))
	msg := validateSettings(body, raw)
	if msg != `Invalid input: expected string, received number at "body.first_name"; Unrecognized key: "zz" at "body"` {
		t.Errorf("got %q", msg)
	}
}

func TestUnknownKeysReporting(t *testing.T) {
	allowed := []string{"a", "b"}
	report := func(keys []string) string {
		var unknown []string
		for _, k := range keys {
			if !contains(allowed, k) {
				unknown = append(unknown, k)
			}
		}
		switch len(unknown) {
		case 1:
			return `Unrecognized key: "` + unknown[0] + `" at "body"`
		case 0:
			return ""
		default:
			parts := make([]string, len(unknown))
			for i, k := range unknown {
				parts[i] = `"` + k + `"`
			}
			return "Unrecognized keys: " + strings.Join(parts, ", ") + ` at "body"`
		}
	}
	if got := report([]string{"a", "zz"}); got != `Unrecognized key: "zz" at "body"` {
		t.Errorf("single: %q", got)
	}
	if got := report([]string{"zzz", "aaa"}); got != `Unrecognized keys: "zzz", "aaa" at "body"` {
		t.Errorf("multi: %q", got)
	}
	if got := report([]string{"a", "b"}); got != "" {
		t.Errorf("none: %q", got)
	}
}
