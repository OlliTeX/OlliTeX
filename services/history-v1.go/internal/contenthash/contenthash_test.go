package contenthash

import (
	"crypto/sha1"
	"encoding/hex"
	"testing"
)

func TestEmptyHash(t *testing.T) {
	if EMPTY_HASH != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Errorf("EMPTY_HASH = %q", EMPTY_HASH)
	}
}

func TestContentHash(t *testing.T) {
	// sha1("hello") = a591a61d136e51e1...
	want := func(s string) string {
		h := sha1.Sum([]byte(s))
		return hex.EncodeToString(h[:])
	}
	if got := ContentHash("hello"); got != want("hello") {
		t.Errorf("ContentHash(hello) = %q", got)
	}
	if got := ContentHash(""); got != "da39a3ee5e6b4b0d3255bfef95601890afd80709" {
		t.Errorf("ContentHash(\"\") = %q, want sha1 of empty", got)
	}
}

func TestBlobHashMatchesGit(t *testing.T) {
	// Empty content: blob hash must equal EMPTY_HASH.
	if got := BlobHash(""); got != EMPTY_HASH {
		t.Errorf("BlobHash(\"\") = %q, want %q", got, EMPTY_HASH)
	}
	// The empty-blob hash is git's well-known empty-blob sha.
	if got := BlobHashForBytes(nil); got != EMPTY_HASH {
		t.Errorf("BlobHashForBytes(nil) = %q, want %q", got, EMPTY_HASH)
	}
	// Sanity: byte-strict header, non-ASCII content uses UTF-8 byte length.
	// content "é" is 2 bytes in UTF-8.
	want := ""
	{
		h := sha1.New()
		_, _ = h.Write([]byte("blob 2\x00\xc3\xa9"))
		want = hex.EncodeToString(h.Sum(nil))
	}
	if got := BlobHash("é"); got != want {
		t.Errorf("BlobHash(é) = %q, want %q", got, want)
	}
}

func TestHEXHashRX(t *testing.T) {
	if !HEXHashRX("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391") {
		t.Error("valid empty hash rejected")
	}
	if HEXHashRX("e69de29bb2d1d6434b8b29ae775ad8c2e48c539") {
		t.Error("short hash accepted")
	}
	if HEXHashRX("E69DE29BB2D1D6434B8B29AE775AD8C2E48C5391") {
		t.Error("uppercase rejected")
	}
	if HEXHashRX("") {
		t.Error("empty accepted")
	}
}
