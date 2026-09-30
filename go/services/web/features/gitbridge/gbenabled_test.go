package gitbridge

import "testing"

func TestGbEnabledEnvWins(t *testing.T) {
	t.Setenv("GIT_BRIDGE_ENABLED", "true")
	if got := gbEnabled(nil); got != "true" {
		t.Errorf("env true -> %q", got)
	}
	t.Setenv("GIT_BRIDGE_ENABLED", "false")
	if got := gbEnabled(nil); got != "false" {
		t.Errorf("env false -> %q", got)
	}
	t.Setenv("GIT_BRIDGE_ENABLED", "")
	// unset + no mongo -> disabled (Node b(undefined) === "false")
	if got := gbEnabled(nil); got != "false" {
		t.Errorf("unset, no mongo -> %q", got)
	}
}

func TestFeatureDisabledWithoutEnv(t *testing.T) {
	t.Setenv("GIT_BRIDGE_ENABLED", "")
	f := Feature(nil)
	if len(f.Routes) != 0 {
		t.Errorf("expected no routes when disabled, got %d", len(f.Routes))
	}
}
