package otc

import (
	"ollitex/go/libraries/otpure"
	"os"
	"path/filepath"
	"testing"
)

const (
	helloWorld     = "hello world\n"
	helloWorldHash = "3b18e512dba79e4c8300dd08aeb37f8e728b8dad"
	emptyHash      = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
)

func TestBlobForFile(t *testing.T) {
	dir := t.TempDir()
	write := func(b []byte) string {
		p := filepath.Join(dir, "file")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		return p
	}

	// editable file
	blob, err := BlobForFile(write([]byte(helloWorld)))
	if err != nil {
		t.Fatalf("BlobForFile: %v", err)
	}
	if blob.GetHash() != helloWorldHash || blob.GetByteLength() != int64(len(helloWorld)) ||
		blob.GetStringLength() == nil || *blob.GetStringLength() != int64(len(helloWorld)) {
		t.Fatalf("editable file mismatch: %+v", blob)
	}

	// multi-byte: 'ol\xc3\xa9\n' = o,l,é(2 bytes),\n -> 5 bytes, 4 chars
	p := write([]byte("ol\xc3\xa9\n"))
	b, _ := BlobForFile(p)
	if b.GetByteLength() != 5 || b.GetStringLength() == nil || *b.GetStringLength() != 4 {
		t.Fatalf("multi-byte mismatch: byteLen=%d strLen=%v", b.GetByteLength(), b.GetStringLength())
	}

	// non-editable leaves stringLength off
	p = write([]byte{0xff, 0xfe, 0x00})
	if b, _ := BlobForFile(p); b.GetStringLength() != nil || b.GetByteLength() != 3 {
		t.Fatalf("non-editable mismatch: %+v", b)
	}

	// empty file
	p = write(nil)
	if b, _ := BlobForFile(p); b.GetHash() != otpure.EmptyHash || b.GetByteLength() != 0 || b.GetStringLength() == nil || *b.GetStringLength() != 0 {
		t.Fatalf("empty mismatch: %+v", b)
	}

	// missing file -> error
	if _, err := BlobForFile(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// Blob value-model oracle (blob.js). Pin the hash validation, fromRaw/toRaw,
// and the constants.
func TestBlob(t *testing.T) {
	valid := "3b18e512dba79e4c8300dd08aeb37f8e728b8dad"
	sl := int64(12)
	b := NewBlob(valid, 12, &sl)
	if b.GetHash() != valid || b.GetByteLength() != 12 || *b.GetStringLength() != 12 {
		t.Fatalf("blob getters mismatch: %+v", b)
	}
	raw := b.ToRaw()
	if raw.Hash != valid || raw.ByteLength != 12 || raw.StringLength == nil || *raw.StringLength != 12 {
		t.Fatalf("toRaw mismatch: %+v", raw)
	}
	if BlobFromRaw(nil) != nil {
		t.Fatal("BlobFromRaw(nil) should be nil")
	}
	if got := BlobFromRaw(raw); got.GetHash() != valid {
		t.Fatalf("fromRaw round-trip mismatch: %+v", got)
	}
	// stringLength may be nil (non-editable)
	if b2 := NewBlob(valid, 3, nil); b2.GetStringLength() != nil {
		t.Fatalf("expected nil stringLength, got %v", *b2.GetStringLength())
	}

	// a bad hash (not 40 hex) panics a typed TypeError
	assertPanicsWithValue(t, "blob: bad hash "+shortString, func() { NewBlob(shortString, 1, nil) })

	// NotFoundError shape
	ne := NewBlobNotFound(valid)
	want := "blob " + valid + " not found"
	if ne.Error() != want || ne.Hash != valid {
		t.Fatalf("NotFoundError mismatch: %q (hash %q)", ne.Error(), ne.Hash)
	}

	// constants
	if MaxEditableByteLengthBound != 3*otpure.MaxStringLength {
		t.Fatalf("MaxEditableByteLengthBound = %d, want %d", MaxEditableByteLengthBound, 3*otpure.MaxStringLength)
	}
	if otpure.EmptyHash != emptyHash {
		t.Fatalf("otpure.EmptyHash mismatch")
	}
	if otpure.HexHashRxString != `^[0-9a-f]{40,40}$` {
		t.Fatalf("otpure.HexHashRxString mismatch")
	}
}

const shortString = "not-a-hash"

func repeat(s string, n int) string {
	b := make([]byte, n)
	rs := []byte(s)
	for i := range b {
		b[i] = rs[i%len(rs)]
	}
	return string(b)
}
