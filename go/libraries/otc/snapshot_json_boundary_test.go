package otc

import (
	"encoding/json"
	"testing"
)

// TestSnapshotFromRawJSONShaped pins the JSON boundary: a raw snapshot
// decoded from JSON carries files as map[string]any (heterogeneous file
// objects), NOT the native map[string]map[string]any. The former assertion
// silently dropped every file, yielding an empty snapshot with no error.
func TestSnapshotFromRawJSONShaped(t *testing.T) {
	const rawJSON = `{"files":{"main.tex":{"content":"hello"},"mystery":{"byteLength":42}}}`
	var raw map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		t.Fatal(err)
	}
	s, err := SnapshotFromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	paths := s.GetFilePathnames()
	if len(paths) != 2 {
		t.Fatalf("want 2 files, got %v", paths)
	}
	main := s.GetFile("main.tex")
	if main == nil {
		t.Fatal("main.tex missing")
	}
	c := main.GetContent(true)
	if c == nil || *c != "hello" {
		t.Fatalf("main.tex content not restored: %v", c)
	}
	m := s.GetFile("mystery")
	if m == nil {
		t.Fatal("mystery missing")
	}
	if m.GetHash() != nil {
		t.Fatal("mystery must be hash-less (byteLength only)")
	}
}
