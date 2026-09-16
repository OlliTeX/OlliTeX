package hub

import "testing"

// Parity pins against services/web/modules/ollitex-hub/app/src/HubTheme.mjs
// (validateMode / COLOR_RE / message strings — byte-exact contract, the
// 400 body is {"error":"<message>"}.

func TestValidateThemeOrder(t *testing.T) {
	good := mode{
		"primary": "#4f46e5", "background": "#f8fafc", "surface": "#ffffff",
		"text": "#0f172a", "dimmed": "#64748b", "border": "#e2e8f0",
		"button": "#4f46e5", "buttonText": "#ffffff",
		"fontFamily": "Inter, sans-serif", "fontSize": float64(15), "radius": float64(10),
	}

	// 1. fully valid
	light, dark, err := validateTheme(good, good)
	if err != "" {
		t.Fatalf("valid: %q", err)
	}
	if light["primary"] != "#4f46e5" || dark["radius"] != float64(10) {
		t.Fatalf("round trip: %v %v", light, dark)
	}

	// 2. first bad color key (COLOR_KEYS order: primary first)
	bad := cloneMode(good)
	bad["primary"] = "red"
	_, _, err = validateTheme(bad, good)
	if err != "light.primary: must be a hex color (#rrggbb)" {
		t.Fatalf("color: %q", err)
	}

	// 3. hex digit counts: exactly 3,4,6,8 allowed; 5,7,9 rejected
	for _, v := range []string{"#abc", "#abcd", "#abc123"} {
		ok := colorHex(v)
		if !ok {
			t.Fatalf("should accept %s", v)
		}
	}
	for _, v := range []string{"#ab", "#abcde", "#abcdef1", "#123456789", "abc", "#GGG"} {
		if colorHex(v) {
			t.Fatalf("should reject %s", v)
		}
	}

	// 4. fontFamily: empty / whitespace / >300 chars / non-string
	bad = cloneMode(good)
	bad["fontFamily"] = "   "
	_, _, err = validateTheme(bad, good)
	if err != "light.fontFamily: must be a non-empty css font stack" {
		t.Fatalf("fontFamily empty: %q", err)
	}
	long := make([]byte, 301)
	for i := range long {
		long[i] = 'a'
	}
	bad = cloneMode(good)
	bad["fontFamily"] = string(long)
	_, _, err = validateTheme(bad, good)
	if err != "light.fontFamily: must be a non-empty css font stack" {
		t.Fatalf("fontFamily long: %q", err)
	}
	// exactly 300 is allowed
	exact := make([]byte, 300)
	for i := range exact {
		exact[i] = 'a'
	}
	bad = cloneMode(good)
	bad["fontFamily"] = string(exact)
	if _, _, err := validateTheme(bad, good); err != "" {
		t.Fatalf("fontFamily 300: %q", err)
	}

	// 5. fontSize: string fails (typeof number), range inclusive
	bad = cloneMode(good)
	bad["fontSize"] = "15"
	_, _, err = validateTheme(bad, good)
	if err != "light.fontSize: must be a number between 12 and 22" {
		t.Fatalf("fontSize string: %q", err)
	}
	for _, f := range []float64{11.999, 22.001} {
		bad = cloneMode(good)
		bad["fontSize"] = f
		_, _, err = validateTheme(bad, good)
		if err != "light.fontSize: must be a number between 12 and 22" {
			t.Fatalf("fontSize %v: %q", f, err)
		}
	}
	for _, f := range []float64{12, 22, 15.5} {
		bad = cloneMode(good)
		bad["fontSize"] = f
		if _, _, err := validateTheme(bad, good); err != "" {
			t.Fatalf("fontSize %v ok: %q", f, err)
		}
	}

	// 6. radius: 2..24 inclusive
	bad = cloneMode(good)
	bad["radius"] = float64(1)
	_, _, err = validateTheme(bad, good)
	if err != "light.radius: must be a number between 2 and 24" {
		t.Fatalf("radius 1: %q", err)
	}
	bad = cloneMode(good)
	bad["radius"] = float64(24)
	if _, _, err := validateTheme(bad, good); err != "" {
		t.Fatalf("radius 24 ok: %q", err)
	}

	// 7. light is validated completely before dark
	bad = cloneMode(good)
	darkBad := cloneMode(good)
	bad["primary"] = "xx"
	darkBad["primary"] = "yy"
	_, _, err = validateTheme(bad, darkBad)
	if err != "light.primary: must be a hex color (#rrggbb)" {
		t.Fatalf("light-first: %q", err)
	}

	// 8. non-object lights
	for _, v := range []interface{}{nil, "obj", float64(1), true, []interface{}{}} {
		_, _, err = validateTheme(v, good)
		switch v.(type) {
		case []interface{}:
			// JS: typeof [] === 'object' → passes object check → first
			// color key fails
			if err != "light.primary: must be a hex color (#rrggbb)" {
				t.Fatalf("array: %q", err)
			}
		default:
			if err != "light: must be an object" {
				t.Fatalf("%T: %q", v, err)
			}
		}
	}
	// dark missing
	_, _, err = validateTheme(good, nil)
	if err != "dark: must be an object" {
		t.Fatalf("dark missing: %q", err)
	}
}

func TestThemeJSONOrder(t *testing.T) {
	good := mode{
		"primary": "#4f46e5", "background": "#f8fafc", "surface": "#ffffff",
		"text": "#0f172a", "dimmed": "#64748b", "border": "#e2e8f0",
		"button": "#4f46e5", "buttonText": "#ffffff",
		"fontFamily": `Inter, "Segoe UI", sans-serif`, "fontSize": float64(15), "radius": float64(10),
	}
	s := themeJSON(good, good)
	want := `{"version":1,"light":{"primary":"#4f46e5","background":"#f8fafc","surface":"#ffffff","text":"#0f172a","dimmed":"#64748b","border":"#e2e8f0","button":"#4f46e5","buttonText":"#ffffff","fontFamily":"Inter, \"Segoe UI\", sans-serif","fontSize":15,"radius":10},"dark":{"primary":"#4f46e5","background":"#f8fafc","surface":"#ffffff","text":"#0f172a","dimmed":"#64748b","border":"#e2e8f0","button":"#4f46e5","buttonText":"#ffffff","fontFamily":"Inter, \"Segoe UI\", sans-serif","fontSize":15,"radius":10}}`
	if s != want {
		t.Fatalf("themeJSON:\n got: %s\nwant: %s", s, want)
	}
}
