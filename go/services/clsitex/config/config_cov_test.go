package config

import (
	"os"
	"testing"
)

func TestForTestAndGetSingleton(t *testing.T) {
	os.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/c")
	t.Cleanup(func() {
		os.Unsetenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES")
		currentMu.Lock()
		currentConfig = nil
		currentMu.Unlock()
	})
	c1 := ForTest()
	if c1 == nil {
		t.Fatal("ForTest returned nil")
	}
	if c1.PathSandbox.Compiles != "/c" {
		t.Fatalf("compiles = %q", c1.PathSandbox.Compiles)
	}
	// Get() returns the cached singleton (ForTest stored it)
	c2 := Get()
	if c2 != c1 {
		t.Fatal("Get() did not return the ForTest-stored singleton")
	}
	// a second Get() is the same pointer (lazily cached)
	c3 := Get()
	if c3 != c1 {
		t.Fatal("Get() is not cached")
	}
}

func TestGetBuildsFromEnv(t *testing.T) {
	clearEnv()
	os.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/c")
	t.Cleanup(func() {
		os.Unsetenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES")
	})
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.PathSandbox.Compiles != "/c" {
		t.Fatalf("sandbox compiles = %q", c.PathSandbox.Compiles)
	}
}
