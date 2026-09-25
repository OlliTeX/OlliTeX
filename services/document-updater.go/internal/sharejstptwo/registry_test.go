package sharejstptwo

import (
	"testing"

	stypes "document-updater/internal/sharejstypes"
)

func TestRegistryRegistration(t *testing.T) {
	tt := stypes.Lookup("text-tp2")
	if tt == nil {
		t.Fatal("text-tp2 not registered")
	}
	if tt.Name() != "text-tp2" {
		t.Fatalf("name = %q", tt.Name())
	}
}
