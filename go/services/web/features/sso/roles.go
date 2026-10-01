package sso

// roles.go — P1c attrFilter role evaluation (overleaf-fed
// ssoRoleEvaluator.mjs parity).
//
// Pins:
//   - Rows: {attribute, values[] (≤10 strings), match:
//     equals|includes|regex, role: local|guest|blocked, caseSensitive
//     (default true)}. sanitize: rows missing attribute or a role
//     other than guest/blocked are dropped to 'local'; values
//     stringified & truncated to 10; match defaults to equals.
//   - evaluateAttrFilter: ARRAY ORDER, first matching row wins; a row
//     with role 'local' is inert; no match ⇒ 'local'.
//     equals: any claim value == any configured value (case-folded when
//     caseSensitive false). includes: list claim ⇒ membership; scalar
//     claim ⇒ substring. regex: any claim value matches (invalid regex
//     ⇒ no-match).
//   - persistedRoleForProvider(ssoRoles, providerId): entry absent ⇒
//     'local'; only 'guest'/'blocked' persist as restrictive.

import (
	"regexp"
)

type roleEval struct {
	Role       string
	ReasonAttr string
	FilterID   int
}

func (r roleEval) role() string { return r.Role }

// sanitizeAttrRule — Node sanitizeAttrFilter row parity.
func sanitizeAttrRule(in AttrRule) AttrRule {
	out := in
	if out.Attribute == "" {
		return out
	}
	if out.Match != "includes" && out.Match != "regex" {
		out.Match = "equals"
	}
	vs := make([]string, 0, len(in.Values))
	for _, v := range in.Values {
		vs = append(vs, v)
		if len(vs) == 10 {
			break
		}
	}
	out.Values = vs
	if out.Role != "guest" && out.Role != "blocked" {
		out.Role = "local"
	}
	if out.CaseSensitive == false {
		// keep; Node: caseSensitive !== false ⇒ default true
	} else {
		out.CaseSensitive = true
	}
	return out
}

// evaluateAttrFilter — Node parity (pure).
func evaluateAttrFilter(attrFilter []AttrRule, profile ssoProfile) roleEval {
	for i, in := range attrFilter {
		row := sanitizeAttrRule(in)
		if row.Role != "guest" && row.Role != "blocked" {
			continue // local rows inert
		}
		if rowMatches(row, profile) {
			return roleEval{Role: row.Role, ReasonAttr: row.Attribute, FilterID: i}
		}
	}
	return roleEval{Role: "local"}
}

func rowMatches(row AttrRule, profile ssoProfile) bool {
	raw, ok := profile[row.Attribute]
	if !ok || raw == nil {
		return false
	}
	cs := row.CaseSensitive
	norm := func(v string) string {
		if !cs {
			return toLower(v)
		}
		return v
	}
	claims := []string{}
	switch t := raw.(type) {
	case string:
		claims = append(claims, t)
	case []string:
		claims = append(claims, t...)
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok {
				claims = append(claims, s)
			}
		}
	}
	values := make([]string, len(row.Values))
	copy(values, row.Values)
	listIsArray := isListClaim(raw)
	switch row.Match {
	case "includes":
		if listIsArray {
			for _, cv := range claims {
				for _, v := range values {
					if norm(cv) == norm(v) {
						return true
					}
				}
			}
			return false
		}
		for _, v := range values {
			if stringsContains(norm(claims[0]), norm(v)) {
				return true
			}
		}
		return false
	case "regex":
		for _, cv := range claims {
			for _, v := range values {
				re, err := regexp.Compile(v)
				if err != nil {
					continue // invalid regex: no-match (Node G5)
				}
				if !cs {
					// case-insensitive: recompile
					if rc, err2 := regexp.Compile("(?i)" + v); err2 == nil {
						re = rc
					} else {
						continue
					}
				}
				if re.MatchString(cv) {
					return true
				}
			}
		}
		return false
	default: // equals
		for _, cv := range claims {
			for _, v := range values {
				if norm(cv) == norm(v) {
					return true
				}
			}
		}
		return false
	}
}

func isListClaim(raw any) bool {
	switch raw.(type) {
	case []string:
		return true
	case []any:
		return true
	}
	return false
}

func stringsContains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// toLower — ASCII lowercase (Node String(...).toLowerCase() for the
// ASCII claim values in practice).
func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

// persistedRoleForProvider — Node parity (user.ssoRoles map access).
func persistedRoleForProvider(ssoRoles map[string]map[string]any, providerID string) string {
	e, ok := ssoRoles[providerID]
	if !ok {
		return "local"
	}
	role, _ := e["role"].(string)
	if role == "guest" || role == "blocked" {
		return role
	}
	return "local"
}
