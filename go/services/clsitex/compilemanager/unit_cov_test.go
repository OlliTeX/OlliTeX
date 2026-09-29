package compilemanager

import "testing"

// --- compileEnv (NODE: env setup in doCompile) ---------------------------------

func TestCompileEnvMatrix(t *testing.T) {
	// no checks, no overrides
	env := compileEnv("", "", "p1", nil, "main.tex")
	if env["OVERLEAF_PROJECT_ID"] != "p1" {
		t.Fatalf("env key: %v", env)
	}
	if len(env) != 1 {
		t.Fatalf("unexpected keys: %v", env)
	}
	oa, ml := "4", ""
	env = compileEnv(oa, ml, "p1", nil, "main.tex")
	if env["openout_any"] != "4" {
		t.Fatalf("openout_any override: %v", env)
	}
	both := "2"
	env = compileEnv(both, both, "p2", nil, "main.tex")
	if env["max_print_line"] != both {
		t.Fatalf("max_print_line override: %v", env)
	}
	err := "error"
	env = compileEnv("", "", "p3", &err, "main.tex")
	if env["CHKTEX_OPTIONS"] == "" || env["CHKTEX_EXIT_ON_ERROR"] == "" {
		t.Fatalf("chktex error env not set: %v", env)
	}
	val := "validate"
	env = compileEnv("", "", "p4", &val, "NOTTEX.Rtex")
	if env["CHKTEX_OPTIONS"] != "" {
		t.Fatalf("chktex env should be absent for non-tex root: %v", env)
	}
}
