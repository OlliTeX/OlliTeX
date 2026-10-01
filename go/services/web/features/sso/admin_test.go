package sso

import (
	"encoding/json"
	"html"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"strings"
	"testing"

	"ollitex/go/services/web/core"
)

// --- masking / sanitizing (Node _maskConfig/_sanitizeConfig parity) ---

func TestMaskConfigDoc(t *testing.T) {
	doc := map[string]any{
		"ldap": map[string]any{"enabled": true, "bindCredentials": "secret-pw"},
		"spMetadata": map[string]any{
			"privateKey": "PEM-PRIV",
			"publicCert": "PEM-CERT",
		},
		"providers": []any{
			map[string]any{"id": "p1", "clientSecret": "cs", "privateKey": "pk", "decryptionPvk": "dpvk", "name": "OIDC"},
			map[string]any{"id": "p2"},
		},
	}
	var m json.RawMessage
	raw, _ := json.Marshal(maskConfigDoc(doc))
	m = raw
	s := string(m)
	for _, must := range []string{"bindCredentials\":\"" + maskSentinel, "clientSecret\":\"" + maskSentinel, "privateKey\":\"" + maskSentinel, "decryptionPvk\":\"" + maskSentinel, "publicCert\":\"" + maskSentinel} {
		if !strings.Contains(s, must) {
			t.Errorf("missing %q in %s", must, s)
		}
	}
	if strings.Contains(s, "secret-pw") || strings.Contains(s, "PEM-") {
		t.Error("secrets leaked")
	}
}

func TestSanitizeConfigDoc_MaskRestore(t *testing.T) {
	existing := map[string]any{
		"providers":  []any{map[string]any{"id": "p1", "clientSecret": "real-secret"}},
		"ldap":       map[string]any{"bindCredentials": "real-bc"},
		"spMetadata": map[string]any{"privateKey": "real-pem"},
	}
	body := map[string]any{
		"providers":  []any{map[string]any{"id": "p1", "clientSecret": maskSentinel, "name": "x"}},
		"ldap":       map[string]any{"bindCredentials": maskSentinel},
		"spMetadata": map[string]any{"privateKey": maskSentinel},
	}
	out := sanitizeConfigDoc(body, existing)
	prov, _ := out["providers"].([]any)
	p1, _ := prov[0].(bson.M)
	if p1["clientSecret"] != "real-secret" {
		t.Errorf("mask restore failed: %q (p1=%#v)", p1["clientSecret"], p1)
	}
	ldap, _ := out["ldap"].(bson.M)
	if ldap["bindCredentials"] != "real-bc" {
		t.Errorf("ldap restore failed: %q (ldap=%#v)", ldap["bindCredentials"], ldap)
	}
	sp, _ := out["spMetadata"].(bson.M)
	if sp["privateKey"] != "real-pem" {
		t.Errorf("spMetadata restore failed: %q (sp=%#v)", sp["privateKey"], sp)
	}
}

func TestSanitizeConfigDoc_AttrFilter(t *testing.T) {
	body := map[string]any{
		"providers": []any{
			map[string]any{"id": "p1", "attrFilter": []any{
				map[string]any{"attribute": "dept", "values": []any{"eng"}, "role": "admin", "match": "bogus"},
				map[string]any{"role": "guest"}, // no attribute → dropped
				map[string]any{"attribute": "group", "values": []any{"staff"}, "role": "guest"},
			}},
		},
	}
	out := sanitizeConfigDoc(body, nil)
	prov, _ := out["providers"].([]any)
	if len(prov) != 1 {
		t.Fatalf("providers: %#v", out)
	}
	p1, _ := prov[0].(bson.M)
	af, _ := p1["attrFilter"].([]bson.M)
	if len(af) != 2 {
		t.Fatalf("expected 2 surviving rows, got %d: %#v", len(af), p1["attrFilter"])
	}
	found := false
	for _, row := range af {
		if row["attribute"] == "dept" {
			found = true
			if row["role"] != "local" {
				t.Errorf("admin role should coerce to local, got %v", row["role"])
			}
			if row["match"] != "equals" {
				t.Errorf("bogus match should default to equals, got %v", row["match"])
			}
		}
	}
	if !found {
		t.Error("dept row missing")
	}
}

func TestSanitizeConfigDoc_AttrFilterRoleCoercion(t *testing.T) {
	body := map[string]any{
		"providers": []any{
			map[string]any{"id": "p1", "attrFilter": []any{
				map[string]any{"attribute": "a", "values": []any{"x"}, "role": "local"},
			}},
		},
	}
	out := sanitizeConfigDoc(body, nil)
	prov, _ := out["providers"].([]any)
	p1, _ := prov[0].(bson.M)
	af, _ := p1["attrFilter"].([]bson.M)
	sawRole := ""
	for _, row := range af {
		if row["attribute"] == "a" {
			sawRole, _ = row["role"].(string)
		}
	}
	if sawRole != "local" {
		t.Errorf("local role should stay, got %q in %#v", sawRole, p1["attrFilter"])
	}
}

func TestMaskSentinelValue(t *testing.T) {
	if maskSentinel != "••••••••" {
		t.Errorf("sentinel = %q", maskSentinel)
	}
}

// --- login slot (fedgap-2) ---

func TestLoginSlotJSON_NoMongo(t *testing.T) {
	a := core.New(&core.Config{Profile: "web"}, nil) // no Mongo
	if a.Mongo != nil {
		t.Fatal("expected nil Mongo")
	}
	s := loginSlotJSON(a, &core.Cxt{Req: &http.Request{}})
	if !strings.Contains(s, html.EscapeString(`"sso":[]`)) {
		t.Errorf("expected empty sso array, got %q", s)
	}
	if !strings.Contains(s, html.EscapeString(`"ldapEnabled":false`)) {
		t.Errorf("expected ldapEnabled:false, got %q", s)
	}
}

// toMapVal — test helper (normalize row map for assertions).
func toMapVal(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if m, ok := v.(bson.M); ok {
		out := map[string]any{}
		for k, x := range m {
			out[k] = x
		}
		return out
	}
	return map[string]any{}
}
