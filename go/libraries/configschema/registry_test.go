package configschema

import "testing"

// TestRegistryUniqueness — the registry is the single source of truth, so
// duplicate keys would be a real defect (the CLI would set twice).
func TestRegistryUniqueness(t *testing.T) {
	seen := map[string]int{}
	for i, p := range Registry {
		if j, dup := seen[p.Key]; dup {
			t.Fatalf("duplicate key %q at index %d (first at %d)", p.Key, i, j)
		}
		seen[p.Key] = i
	}
}

// TestRegistryShape — every entry must be well-formed.
func TestRegistryShape(t *testing.T) {
	kinds := map[Kind]bool{KString: true, KBool: true, KInt: true}
	groups := map[string]bool{}
	for _, g := range Groups() {
		groups[g] = true
	}
	for i, p := range Registry {
		if p.Key == "" {
			t.Fatalf("%d: empty key", i)
		}
		if !kinds[p.Kind] {
			t.Fatalf("%d (%s): bad kind %q", i, p.Key, p.Kind)
		}
		if !groups[p.Group] {
			t.Fatalf("%d (%s): group %q missing from Groups()", i, p.Key, p.Group)
		}
	}
}

func TestFindAndPredicates(t *testing.T) {
	if IsSecret("OVERLEAF_EMAIL_SMTP_PASS") != true {
		t.Fatal("SMTP pass must be secret")
	}
	if IsSecret("APP_NAME") {
		t.Fatal("APP_NAME is not secret")
	}
	if !Known("SITE_URL") || Known("NO_SUCH_KEY") {
		t.Fatal("Known() wrong")
	}
	p, ok := Find("APP_NAME")
	if !ok || p.Default != "OlliTeX" {
		t.Fatalf("APP_NAME = %+v ok=%v", p, ok)
	}
	if len(Groups()) < 8 {
		t.Fatalf("expected a rich group set, got %v", Groups())
	}
}
