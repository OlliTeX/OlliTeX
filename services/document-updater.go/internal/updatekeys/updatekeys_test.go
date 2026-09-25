package updatekeys

import "testing"

func TestCombineProjectIdAndDocId(t *testing.T) {
	if got := CombineProjectIdAndDocId("project1", "doc1"); got != "project1:doc1" {
		t.Fatalf("combine = %q, want project1:doc1", got)
	}
	if got := CombineProjectIdAndDocId("p", "d"); got != "p:d" {
		t.Fatalf("combine = %q, want p:d", got)
	}
}

func TestSplitProjectIdAndDocId(t *testing.T) {
	p, d, ok := SplitProjectIdAndDocId("project1:doc1")
	if !ok || p != "project1" || d != "doc1" {
		t.Fatalf("split = (%q, %q, %v), want (project1, doc1, true)", p, d, ok)
	}
	if _, _, ok := SplitProjectIdAndDocId("justproject"); ok {
		t.Fatalf("split with no colon should yield ok=false")
	}
	p, d, ok = SplitProjectIdAndDocId(":doc")
	if !ok || p != "" || d != "doc" {
		t.Fatalf("split empty-project = (%q, %q, %v)", p, d, ok)
	}
}
