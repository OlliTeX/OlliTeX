package sharejstxtcomp

import (
	"testing"

	stypes "document-updater/internal/sharejstypes"
)

func TestRegistryRegistration(t *testing.T) {
	tt := stypes.Lookup("text-composable")
	if tt == nil {
		t.Fatal("text-composable not registered")
	}
	if tt.Name() != "text-composable" {
		t.Fatalf("name = %q", tt.Name())
	}
}
