package userpages

import (
	"context"
	"encoding/json"
	"fmt"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/sitesettings"
	"ollitex/go/services/web/features/templates"
	"ollitex/go/services/web/views"
	"strings"
	"unicode"

	"go.mongodb.org/mongo-driver/bson"
)

func pageBase(cxt *core.Cxt, email, uid string) views.PageData {
	d := views.PageData{
		Nonce:     views.NewNonce(),
		Origin:    cxt.SiteURL,
		UserEmail: email,
		UserID:    uid,
		// U9 (live gate 2026-09-22): ol-ExposedSettings
		// canManageTemplatesMenu = the full template-admin ladder per
		// session role (Node page-level pin: admin true, member false),
		// and the navbar showSignUpLink = hasFeature('registration-page')
		// (false in this SAML stack — env/SSO resolution shared with
		// the authpages/hub/navbars).
		CanManageTemplateMenu: templates.MenuGrant(cxt.Req.Context(), cxt),
		ShowSignUpLink:        sitesettings.RegistrationEnabled(cxt.A, cxt.Req.Context()),
		NavSiteAdmin:          core.NavSiteAdmin(cxt.Sess),
	}
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
	}
	return d
}

func userDoc(a *core.App, ctx context.Context, uid string) (map[string]any, bool) {
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	doc := map[string]any{}
	err = db.Collection("users").FindOne(
		ctx, bson.D{{Key: "_id", Value: mustObjectID(uid)}}).Decode(&doc)
	if err != nil {
		return nil, false
	}
	return doc, true
}

// ---------- GET /user/settings ----------

func str(v any) bool {
	b, _ := v.(bool)
	return b
}

func rolesInclude(v any, role string) bool {
	rl, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range rl {
		if e == role {
			return true
		}
	}
	return false
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

func b(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func features_has(k string, m map[string]bool) bool {
	_, ok := m[k]
	return ok
}

func jsonEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// ---------- sessions helpers (UserSessionsManager parity) ----------

func nestedUser(doc map[string]json.RawMessage, first, second string) map[string]any {
	for _, name := range []string{first, second} {
		sub, ok := doc[name]
		if !ok {
			continue
		}
		var wrap struct {
			User any `json:"user"`
		}
		if json.Unmarshal(sub, &wrap) == nil && wrap.User != nil {
			if m, ok := wrap.User.(map[string]any); ok {
				return m
			}
		}
		var m map[string]any
		if json.Unmarshal(sub, &m) == nil {
			return m
		}
	}
	return nil
}

// ---------- body reading (order-aware) ----------

func contains(ks []string, v string) bool {
	for _, k := range ks {
		if k == v {
			return true
		}
	}
	return false
}

// readBody mirrors the P3.1 pattern: (fields, keyOrder, kind, ok) + raw
// JSON (needed for ORDER-sensitive nested objects like customKeybindings).

// ---------- JS semantics ----------

// jsBool — Boolean(value) for the six coerced settings (any input type).
func jsBool(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0 && t == t // NaN → false
	}
	return true
}

// sanitizeCtrl — Node sanitizeControlCharacters: [\u0000-\u001F\u007F-
// \u009F\u200B\u200C\u200D\u2060\uFEFF] → "\uXXXX" (4-hex escape text).

// sanitizeCtrl — Node sanitizeControlCharacters: [\u0000-\u001F\u007F-
// \u009F\u200B\u200C\u200D\u2060\uFEFF] → "\uXXXX" (4-hex escape text).
func sanitizeCtrl(s string) string {
	var out strings.Builder
	for _, r := range s {
		if isControlChar(r) {
			fmt.Fprintf(&out, `\u%04x`, r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func isControlChar(r rune) bool {
	return (r >= 0x00 && r <= 0x1F) || (r >= 0x7F && r <= 0x9F) ||
		r == 0x200B || r == 0x200C || r == 0x200D || r == 0x2060 || r == 0xFEFF
}

// jStrTrim — JS String.prototype.trim (Unicode whitespace + BOM + NBSP).

// jStrTrim — JS String.prototype.trim (Unicode whitespace + BOM + NBSP).
func jStrTrim(r rune) bool {
	return unicode.IsSpace(r) || r == 0x00A0 || r == 0xFEFF ||
		r == 0x2028 || r == 0x2029 || r == 0x180E
}

// ---------- moment ----------
