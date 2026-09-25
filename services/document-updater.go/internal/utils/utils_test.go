package utils

import (
	"crypto/sha1"
	"encoding/hex"
	"testing"
)

func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func ptr(s string) *string { return &s }

func TestOpKinds(t *testing.T) {
	if !IsInsert(Op{I: ptr("x")}) {
		t.Fatal("I present should be insert")
	}
	if !IsDelete(Op{D: ptr("x")}) {
		t.Fatal("D present should be delete")
	}
	if IsInsert(Op{}) || IsDelete(Op{}) {
		t.Fatal("empty op is neither insert nor delete")
	}
	if IsComment(Op{}) {
		t.Fatal("empty op is not a comment")
	}
}

func TestGetDocLength(t *testing.T) {
	if got := GetDocLength([]string{"hello"}); got != 5 {
		t.Fatalf("single = %d, want 5", got)
	}
	if got := GetDocLength([]string{"hello", "there", "world"}); got != 17 {
		t.Fatalf("multiline = %d, want 17 (5+5+5 + (3-1) newlines)", got)
	}
	if got := GetDocLength([]string{}); got != 0 {
		t.Fatalf("empty = %d, want 0", got)
	}
	if got := GetDocLength([]string{"a", "", "c"}); got != 4 {
		t.Fatalf("empty middle = %d, want 4 ((1+0+1) + (3-1) newlines)", got)
	}
}

func TestAddTrackedDeletesToContent(t *testing.T) {
	// mirrors UtilsTests.js: inserts are skipped, deletes re-inserted at p.
	content := "the brown fox jumps over the dog"
	changes := []TrackedChange{
		{Op: Op{D: ptr("quick "), P: 4}},
		{Op: Op{I: ptr("brown "), P: 5}},
		{Op: Op{D: ptr("lazy "), P: 29}},
	}
	if got := AddTrackedDeletesToContent(content, changes); got != "the quick brown fox jumps over the lazy dog" {
		t.Fatalf("got %q", got)
	}
	if got := AddTrackedDeletesToContent("the quick brown fox", nil); got != "the quick brown fox" {
		t.Fatalf("no deletes: got %q", got)
	}
}

func TestComputeDocHash(t *testing.T) {
	if got, want := ComputeDocHash([]string{}), sha1Hex(""); got != want {
		t.Fatalf("empty hash = %s, want %s", got, want)
	}
	if got, want := ComputeDocHash([]string{"hello"}), sha1Hex("hello"); got != want {
		t.Fatalf("single hash = %s, want %s", got, want)
	}
	if got, want := ComputeDocHash([]string{"hello", "there", "world"}), sha1Hex("hello\nthere\nworld"); got != want {
		t.Fatalf("multi hash = %s, want %s", got, want)
	}
}

func TestExtractOriginOrSource(t *testing.T) {
	src, origin := ExtractOriginOrSource("git-bridge")
	if src != "git-bridge" || origin != nil {
		t.Fatalf("string input: src=%q origin=%v", src, origin)
	}
	src, origin = ExtractOriginOrSource(map[string]any{"kind": "git-bridge"})
	if src != "" || origin == nil {
		t.Fatalf("object input: src=%q origin is nil", src)
	}
	src, origin = ExtractOriginOrSource(nil)
	if src != "" || origin != nil {
		t.Fatalf("nil input: src=%q origin=%v", src, origin)
	}
}
