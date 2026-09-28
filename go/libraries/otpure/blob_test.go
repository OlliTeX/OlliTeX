package otpure

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Oracle: libraries/overleaf-editor-core/test/unit/blob_utils.test.js
// (carved out of otc/blob_test.go — d5dd23dd S1a).

const (
	helloWorld     = "hello world\n"
	helloWorldHash = "3b18e512dba79e4c8300dd08aeb37f8e728b8dad"
	emptyHash      = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
)

func TestBlobHashFromString(t *testing.T) {
	if got := BlobHashFromString(helloWorld); got != helloWorldHash {
		t.Fatalf("BlobHashFromString(helloWorld) = %q, want %q", got, helloWorldHash)
	}
	if got := BlobHashFromString(""); got != emptyHash {
		t.Fatalf("BlobHashFromString(\"\") = %q, want %q", got, emptyHash)
	}
	if emptyHash != EmptyHash {
		t.Fatalf("EMPTY_HASH mismatch: %q vs %q", emptyHash, EmptyHash)
	}
	// 'héllo\n' is 7 bytes / 6 chars: hashing char count would disagree with git.
	if got := BlobHashFromString("héllo\n"); got != "5fb50d3c93474f139362304b663fe44e9d17a26e" {
		t.Fatalf("BlobHashFromString('héllo\\n') = %q", got)
	}
	// real GitHub-reported shas (from the github-sync acceptance suite)
	cases := map[string]string{
		"Hello world":       "70c379b63ffa0795fdbfbc128e5a2818397b7ef8",
		"Chapter 1 content": "ae3a11ab13155c7d114fa05590a1c1e23b1c0913",
		"Chapter 2 content": "6986b1ac5bf8e4aa890b57b71c1d215f8f9a27e5",
		"":                  emptyHash,
	}
	for in, want := range cases {
		if got := BlobHashFromString(in); got != want {
			t.Errorf("BlobHashFromString(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlobHashFromBuffer(t *testing.T) {
	if got := BlobHashFromBuffer([]byte(helloWorld)); got != helloWorldHash {
		t.Fatalf("blobHashFromBuffer(helloWorld) = %q, want %q", got, helloWorldHash)
	}
	if got := BlobHashFromBuffer(nil); got != emptyHash {
		t.Fatalf("blobHashFromBuffer(empty) = %q, want %q", got, emptyHash)
	}
	content := "héllo wörld\n"
	if a, b := BlobHashFromBuffer([]byte(content)), BlobHashFromString(content); a != b {
		t.Fatalf("buffer vs string disagree: %q vs %q", a, b)
	}
	if got := BlobHashFromBuffer([]byte{0x00, 0x01, 0xff, 0xfe}); got != "ad2f38543fc2bba3468a77f36137c23378420463" {
		t.Fatalf("blobHashFromBuffer(binary) = %q", got)
	}
}

func TestBlobHashFromStream(t *testing.T) {
	if got, err := BlobHashFromStream(int64(len(helloWorld)), bytes.NewBufferString(helloWorld)); err != nil || got != helloWorldHash {
		t.Fatalf("stream(helloWorld) = %q err=%v", got, err)
	}
	// chunk boundary: a large single-buffer stream agrees with the string hash
	content := repeat("x", 128*1024) + "end\n"
	want := BlobHashFromString(content)
	r := &chunkedReader{data: []byte(content), chunk: 4096}
	if got, err := BlobHashFromStream(int64(len(content)), r); err != nil || got != want {
		t.Fatalf("stream(chunked) = %q err=%v, want %q", got, err, want)
	}
}

// a reader that caps Read at `chunk` bytes to exercise multiple Read calls.
type chunkedReader struct {
	data  []byte
	pos   int
	chunk int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := len(r.data) - r.pos
	if n > r.chunk {
		n = r.chunk
	}
	if len(p) < n {
		n = len(p)
	}
	copy(p, r.data[r.pos:r.pos+n])
	r.pos += n
	return n, nil
}

func TestBlobHashFromFile(t *testing.T) {
	dir := t.TempDir()
	write := func(b []byte) string {
		p := filepath.Join(dir, "file")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		return p
	}

	if got, err := BlobHashFromFile(write([]byte(helloWorld))); err != nil || got != helloWorldHash {
		t.Fatalf("file(helloWorld) = %q err=%v", got, err)
	}
	if got, err := BlobHashFromFile(write(nil)); err != nil || got != emptyHash {
		t.Fatalf("file(empty) = %q err=%v", got, err)
	}
	content := "héllo wörld\n"
	if a, _ := BlobHashFromFile(write([]byte(content))); a != BlobHashFromString(content) {
		t.Fatalf("file vs string disagree for multibyte")
	}
	// larger than one chunk
	big := repeat("x", 128*1024) + "end\n"
	if a, _ := BlobHashFromFile(write([]byte(big))); a != BlobHashFromString(big) {
		t.Fatalf("file vs string disagree for large content")
	}
	if got, _ := BlobHashFromFile(write([]byte{0x00, 0x01, 0xff, 0xfe})); got != "ad2f38543fc2bba3468a77f36137c23378420463" {
		t.Fatalf("file(binary) = %q", got)
	}
	// missing file -> *PathError (ENOENT class)
	_, err := BlobHashFromFile(filepath.Join(dir, "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGetStringLengthOfBuffer(t *testing.T) {
	if got := GetStringLengthOfBuffer([]byte("hello")); got == nil || *got != 5 {
		t.Fatalf("expected 5, got %v", got)
	}
	// 'é' is 2 bytes, 'ᚠ' is 3; 8 bytes but 5 characters
	if b, got := []byte("héllᚠ"), GetStringLengthOfBuffer([]byte("héllᚠ")); len(b) != 8 || got == nil || *got != 5 {
		t.Fatalf("expected len 8 / 5 chars, got %d / %v", len(b), got)
	}
	// invalid utf-8
	if got := GetStringLengthOfBuffer([]byte{0x61, 0x80, 0x62}); got != nil {
		t.Fatalf("expected nil for invalid utf-8, got %v", *got)
	}
	// NUL
	if got := GetStringLengthOfBuffer([]byte("a\x00b")); got != nil {
		t.Fatalf("expected nil for NUL, got %v", *got)
	}
	// non-BMP
	if got := GetStringLengthOfBuffer([]byte("a \U0001F642 face")); got != nil {
		t.Fatalf("expected nil for non-BMP, got %v", *got)
	}
	// over max
	if got := GetStringLengthOfBuffer([]byte(repeat("a", MaxStringLength+1))); got != nil {
		t.Fatalf("expected nil for over-max, got %v", *got)
	}
	// at max
	if got := GetStringLengthOfBuffer([]byte(repeat("a", MaxStringLength))); got == nil || *got != MaxStringLength {
		t.Fatalf("expected max, got %v", got)
	}
}

func TestGetStringLengthOfFile(t *testing.T) {
	dir := t.TempDir()
	write := func(b []byte) string {
		p := filepath.Join(dir, "file")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		return p
	}
	p := write([]byte("héllᚠ"))
	if got, err := GetStringLengthOfFile(int64(len("héllᚠ")), p); err != nil || got == nil || *got != 5 {
		t.Fatalf("expected 5, got %v err=%v", got, err)
	}
	p2 := write([]byte{0x61, 0x00, 0x62})
	if got, _ := GetStringLengthOfFile(3, p2); got != nil {
		t.Fatalf("expected nil for non-editable, got %v", *got)
	}
	// does not read a file too large to be editable (nonexistent path would throw)
	if got, err := GetStringLengthOfFile(MaxEditableByteLengthBound+1, filepath.Join(dir, "does-not-exist")); err != nil || got != nil {
		t.Fatalf("expected nil without reading, got %v err=%v", got, err)
	}
}

func repeat(s string, n int) string {
	b := make([]byte, n)
	rs := []byte(s)
	for i := range b {
		b[i] = rs[i%len(rs)]
	}
	return string(b)
}
