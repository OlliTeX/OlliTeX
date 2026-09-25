package configschema

import (
	"encoding/json"
	"testing"
)

// TestParseJSONC — comment/trailing-comma stripping must never touch the
// inside of strings.
func TestParseJSONC(t *testing.T) {
	src := `{
  // line comment with "quote
  "a": "http://keep://me", // trailing comment
  /* block /* not a comment
     comment */ "b": 1,
  "c": "string with /* and // inside",
  "d": true,
  "e": null,
  "f": "quote // escaped \\ " ,
}`
	out, err := ParseJSONC(src)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := unmarshalJSON(t, out, &m); err != nil {
		t.Fatalf("unmarshal(%q): %v", out, err)
	}
	if m["a"] != "http://keep://me" {
		t.Fatalf("a = %v", m["a"])
	}
	if m["c"] != "string with /* and // inside" {
		t.Fatalf("c = %v", m["c"])
	}
	if f, _ := m["f"].(string); f != `quote // escaped \ ` {
		t.Fatalf("f = %q", f)
	}
	if m["b"] != float64(1) || m["d"] != true || m["e"] != nil {
		t.Fatalf("b/d/e = %v %v %v", m["b"], m["d"], m["e"])
	}
	if len(m) != 6 {
		t.Fatalf("len = %d", len(m))
	}
	// unterminated block comment fails
	if _, err := ParseJSONC(`{ "a": 1 /* oops`); err == nil {
		t.Fatal("unterminated block comment should error")
	}
}

// TestDefaultsMatchesRegistry — the seed file and the registry must stay in
// lockstep (key sets equal; typed defaults equal; no stray keys).
func TestDefaultsMatchesRegistry(t *testing.T) {
	def, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	if len(def) != len(Registry) {
		t.Fatalf("defaults = %d keys, registry = %d", len(def), len(Registry))
	}
	for _, p := range Registry {
		v, ok := def[p.Key]
		if !ok {
			t.Errorf("registry key %s missing from defaults.jsonc", p.Key)
			continue
		}
		switch p.Kind {
		case KBool:
			if p.Default == "" {
				if v != nil {
					t.Errorf("%s: no static default, want nil, got %v", p.Key, v)
				}
			} else if b, ok := v.(bool); !ok || (p.Default == "true") != b {
				t.Errorf("%s: default %q vs %v", p.Key, p.Default, v)
			}
		case KInt:
			if p.Default == "" {
				if v != nil {
					t.Errorf("%s: no static default, want nil, got %v", p.Key, v)
				}
			} else if n, ok := v.(float64); !ok || int(n) != atoi(p.Default) {
				t.Errorf("%s: default %q vs %v", p.Key, p.Default, v)
			}
		default:
			if p.Default == "" {
				if v != nil {
					t.Errorf("%s: no static default, want nil, got %v", p.Key, v)
				}
			} else if s, ok := v.(string); !ok || s != p.Default {
				t.Errorf("%s: default %q vs %v", p.Key, p.Default, v)
			}
		}
	}
	for k := range def {
		if _, ok := Find(k); !ok {
			t.Errorf("defaults.jsonc key %s not in the registry", k)
		}
	}
	// spot checks
	if def["APP_NAME"] != "OlliTeX" {
		t.Fatalf("APP_NAME = %v", def["APP_NAME"])
	}
	if def["OVERLEAF_EMAIL_SMTP_PASS"] != nil {
		t.Fatalf("secret must have no static default, got %v", def["OVERLEAF_EMAIL_SMTP_PASS"])
	}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func unmarshalJSON(t *testing.T, src string, v any) error {
	t.Helper()
	return json.Unmarshal([]byte(src), v)
}
