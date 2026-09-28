package hashmanager

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Oracle: git hash-object (byte-exact "blob <len>\0" framing), cross-checked
// against Node app/js/HashManager.js (live: _getBlobHashFromString("hello
// world") == 95d09f2b10159... == git hash-object of same bytes).
// Node unit tests: none direct for HashManager (indirect via BlobManagerTests).

func gitBlobSHA1(t *testing.T, data []byte) string {
	t.Helper()
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func TestGetBlobHashFromString(t *testing.T) {
	cases := []string{
		"",
		"hello world",
		"common prefix suffix",
		"héllo wörld §€",
		"a\nb\x00c\n",
		strings.Repeat("x", 1000),
		"ü",
		"€2.00",
		`{"op":1,"r":5}`,
		"café au lait\n\nsecond",
	}
	for _, s := range cases {
		if got, want := GetBlobHashFromString(s), gitBlobSHA1(t, []byte(s)); got != want {
			t.Errorf("string hash %q: got %s want %s", s, got, want)
		}
	}
	// Byte-exact git golden: empty blob.
	if h := GetBlobHashFromString(""); h != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Errorf("empty blob hash: %s", h)
	}
	// Golden from Node oracle probe (2026-09-19).
	if h := GetBlobHashFromString("hello world"); h != "95d09f2b10159347eece71399a7e2e907ea3df4f" {
		t.Errorf("hello world hash: %s", h)
	}
}

func TestGetBlobHash(t *testing.T) {
	for _, data := range [][]byte{
		{},
		[]byte("hello world"),
		[]byte(strings.Repeat("file-content\n", 803)),
		[]byte("überlatex \x00 bin"),
	} {
		dir := t.TempDir()
		p := filepath.Join(dir, "blob.txt")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		got, sz, err := GetBlobHash(p)
		if err != nil {
			t.Fatal(err)
		}
		if int(sz) != len(data) {
			t.Errorf("size: got %d want %d", sz, len(data))
		}
		if want := gitBlobSHA1(t, data); got != want {
			t.Errorf("file hash len %d: got %s want %s", len(data), got, want)
		}
	}
	// missing file
	_, _, err := GetBlobHash("/nonexistent/nope")
	if err == nil {
		t.Error("missing file: expected error, got nil")
	}
}
