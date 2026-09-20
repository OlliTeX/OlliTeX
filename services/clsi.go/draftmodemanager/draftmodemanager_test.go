package draftmodemanager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInjectDraftMode(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "main.tex")
	const orig = "\\documentclass{article}\n"
	if err := os.WriteFile(f, []byte(orig), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := InjectDraftMode(f); err != nil {
		t.Fatalf("inject: %v", err)
	}
	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	want := PREFIX + orig
	if string(got) != want {
		t.Errorf("after inject = %q, want %q", got, want)
	}
}

func TestInjectDraftModeMissingFile(t *testing.T) {
	if err := InjectDraftMode("/tmp/__definitely_missing__.tex"); err == nil {
		t.Error("InjectDraftMode on a missing file must return an error")
	}
}
