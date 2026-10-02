package wordcount

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMarkerFixture(t *testing.T) {
	path := filepath.Join("testdata", "markerproj", "marker.pdf")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture marker.pdf missing (regenerate per HANDOFF T13)")
	}
	total, heading, heads, ok := ReadMarker(path)
	// main.typ (committed fixture): 12 words, no headings (bare text lines).
	if !ok || total != 12 || heading != 0 || heads != 0 {
		t.Fatalf("fixture marker read: total=%d heading=%d heads=%d ok=%v",
			total, heading, heads, ok)
	}
}

func TestReadMarkerControlPDF(t *testing.T) {
	// Same document compiled WITHOUT the injected driver: no rendered
	// marker -> ok=false (the fallback trigger, plan §9).
	path := filepath.Join("testdata", "markerproj", "control.pdf")
	if _, err := os.Stat(path); err != nil {
		t.Skip("fixture control.pdf missing")
	}
	if _, _, _, ok := ReadMarker(path); ok {
		t.Fatal("control PDF must not match the marker")
	}
}

func TestReadMarkerMissingFile(t *testing.T) {
	if _, _, _, ok := ReadMarker(filepath.Join("testdata", "nope.pdf")); ok {
		t.Fatal("missing pdf must return ok=false (graceful degrade)")
	}
}
