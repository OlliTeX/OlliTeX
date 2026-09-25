package blobstore

import (
	"testing"

	ch "history-v1/internal/contenthash"
)

func TestStringRoundTrip(t *testing.T) {
	s := NewFakeBlobStore()
	h, err := s.PutString("abc")
	if err != nil {
		t.Fatalf("PutString: %v", err)
	}
	if want := ch.BlobHash("abc"); h != want {
		t.Fatalf("hash = %q, want git blob %q", h, want)
	}
	got, err := s.GetString(h)
	if err != nil || got != "abc" {
		t.Fatalf("GetString = %q, %v; want \"abc\" nil", got, err)
	}
	gsh, err := s.GetHashBlob(h)
	if err != nil || gsh != "abc" {
		t.Fatalf("GetHashBlob = %q, %v", gsh, err)
	}
	if !s.Contains(h) {
		t.Fatal("Contains(hash) = false")
	}
}

func TestStringNotFound(t *testing.T) {
	s := NewFakeBlobStore()
	hash := "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" // EMPTY_HASH, not stored
	if _, err := s.GetString(hash); err == nil {
		t.Fatal("GetString absent: wanted *BlobNotFoundError")
	}
	if _, err := s.GetHashBlob(hash); err == nil {
		t.Fatal("GetHashBlob absent: wanted error")
	}
	if s.Contains(hash) {
		t.Fatal("Contains(absent) = true")
	}
}

func TestObjectRoundTrip(t *testing.T) {
	s := NewFakeBlobStore()
	canonical := `{"comments":[{"id":"c","ranges":[{"pos":0,"length":1}]}],"trackedChanges":[]}`
	h, err := s.PutObject([]byte(canonical))
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if want := ch.BlobHashForBytes([]byte(canonical)); h != want {
		t.Fatalf("object hash = %q, want %q", h, want)
	}
	got, err := s.GetObject(h)
	if err != nil || string(got) != canonical {
		t.Fatalf("GetObject = %q, %v; want %q", got, err, canonical)
	}
	r, err := s.GetRangesBlob(h)
	if err != nil || string(r) != canonical {
		t.Fatalf("GetRangesBlob = %q, %v", r, err)
	}
}

func TestObjectNotFound(t *testing.T) {
	s := NewFakeBlobStore()
	if _, err := s.GetObject("0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("GetObject absent: wanted error")
	}
}

func TestBinaryPut(t *testing.T) {
	s := NewFakeBlobStore()
	data := []byte{0x00, 0x01, 0xff, 0x00}
	h, err := s.PutBytes(data)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	if want := ch.BlobHashForBytes(data); h != want {
		t.Fatalf("binary hash = %q, want %q", h, want)
	}
	gsh, err := s.GetHashBlob(h)
	if err != nil || gsh != string(data) {
		t.Fatalf("GetHashBlob(binary) = %q, %v", gsh, err)
	}
}

func TestEmptyHashRoundTrip(t *testing.T) {
	s := NewFakeBlobStore()
	h, err := s.PutString("")
	if err != nil {
		t.Fatalf("PutString(): %v", err)
	}
	if want := ch.EMPTY_HASH; h != want {
		t.Fatalf("empty string hash = %q, want %q", h, want)
	}
	got, err := s.GetString(h)
	if err != nil || got != "" {
		t.Fatalf("GetString(empty) = %q, %v", got, err)
	}
}

// TestStringBlob pins the BlobStoreI metadata contract the load path
// consults to pick binary vs. lazy/hollow file kind (Node Blob
// getByteLength/getStringLength).
func TestStringBlob(t *testing.T) {
	s := NewFakeBlobStore()

	// not found
	if _, _, ok := s.StringBlob(ch.BlobHashForBytes([]byte{0x01, 0x02})); ok {
		t.Fatal("StringBlob(unstored) found, want false")
	}

	// string blob: byteLength = UTF-8 bytes, stringLength = UTF-16 units
	sh, err := s.PutString("h\u00e9llo \U0001F600") // 8 UTF-16 units, 11 UTF-8 bytes
	if err != nil {
		t.Fatal(err)
	}
	bl, sl, ok := s.StringBlob(sh)
	if !ok {
		t.Fatal("StringBlob(string) not found")
	}
	if bl != 11 || sl != 8 {
		t.Fatalf("string blob = %d/%d, want 11/8 (utf8/utf16)", bl, sl)
	}

	// object blob: treated as a string blob (stringLength known)
	obj, _ := s.PutObject([]byte(`{"a":1}`))
	obl, osl, ok := s.StringBlob(obj)
	if !ok || osl < 0 || obl != 7 {
		t.Fatalf("object blob = %d/%d ok=%v, want byteLength 7 and stringLength >=0", obl, osl, ok)
	}

	// binary blob: byteLength known, stringLength == -1 (Node: null)
	bh, err := s.PutBytes([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D})
	if err != nil {
		t.Fatal(err)
	}
	hbl, hsl, ok := s.StringBlob(bh)
	if !ok || hbl != 5 || hsl != -1 {
		t.Fatalf("binary blob = %d/%d ok=%v, want 5/-1 found", hbl, hsl, ok)
	}

	// EMPTY_HASH special-cased like Node EMPTY_BLOB{0,0}
	if bl, sl, ok := s.StringBlob(ch.EMPTY_HASH); !ok || bl != 0 || sl != 0 {
		t.Fatalf("StringBlob(EMPTY_HASH) = %d/%d ok=%v, want 0/0 found", bl, sl, ok)
	}
}
