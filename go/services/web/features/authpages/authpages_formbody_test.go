package authpages

import (
	"net/http/httptest"
	"strings"
	"testing"

	"ollitex/go/services/web/core"
)

// TestDecodeBodyFormParity — regression (2026-10-04): the old
// `strings.Contains(ct, "application/")` catch-all routed
// x-www-form-urlencoded bodies into the JSON branch, so form login
// 400'd with empty fields (browser login broken; e2e seed login 400/401).
// Node contract: the urlencoded parser handles form bodies.
func TestDecodeBodyFormParity(t *testing.T) {
	type in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	mk := func(ct, body string) *core.Cxt {
		req := httptest.NewRequest("POST", "/login", strings.NewReader(body))
		req.Header.Set("Content-Type", ct)
		return &core.Cxt{Req: req}
	}
	// form CT variants: with charset, and UPPER-CASE (case-insensitive
	// content-type matching; the exact MIME string, no separators added).
	formCT1 := "application/x-www-form-urlencoded; charset=UTF-8"
	formCT2 := strings.ToUpper(strings.TrimSuffix(formCT1, "; charset=UTF-8"))
	cases := []struct {
		name string
		ct   string
		body string
	}{
		{"form-with-charset", formCT1, "email=a%40b.c&password=secret"},
		{"form-uppercase", formCT2, "email=a%40b.c&password=secret"},
		{"json", "application/json", `{"email":"a@b.c","password":"secret"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v in
			if err := decodeBody(mk(tc.ct, tc.body), &v); err != nil {
				t.Fatalf("decodeBody: %v", err)
			}
			if v.Email != "a@b.c" || v.Password != "secret" {
				t.Fatalf("fields: email=%q password=%q", v.Email, v.Password)
			}
		})
	}
}
