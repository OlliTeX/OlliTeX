package otc

import (
	"strings"
	"testing"
)

// a1..a0 40-hex hashes (Node oracle literals).
var (
	testHash    = "a5675307b61ec2517330622a6e649b4ca1ee5612"
	testRngHash = "380de212d09bf8498065833dbf242aaf11184316"
)

func TestHashFileDataBasics(t *testing.T) {
	fd, err := newHashFileData(testHash, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h := fd.GetHash(); h == nil || *h != testHash {
		t.Fatalf("GetHash = %v", h)
	}
	if r := fd.GetRangesHash(); r != nil {
		t.Fatalf("GetRangesHash = %v, want nil", *r)
	}

	fd2, err := newHashFileData(testHash, &testRngHash)
	if err != nil {
		t.Fatal(err)
	}
	if h := fd2.GetHash(); h == nil || *h != testHash {
		t.Fatalf("GetHash = %v", h)
	}
	if r := fd2.GetRangesHash(); r == nil || *r != testRngHash {
		t.Fatalf("GetRangesHash = %v", r)
	}

	// invalid hash -> error (Node: assert.match HEX_HASH_RX)
	if _, err = newHashFileData("nothex", nil); err == nil {
		t.Fatal("expected an error for a bad hash")
	}

	raw, err := FromRawFileData(map[string]any{"hash": testHash, "rangesHash": testRngHash})
	if err != nil {
		t.Fatal(err)
	}
	if h := raw.GetHash(); h == nil || *h != testHash {
		t.Fatalf("fromRaw GetHash = %v", h)
	}
	if r := raw.GetRangesHash(); r == nil || *r != testRngHash {
		t.Fatalf("fromRaw GetRangesHash = %v", r)
	}
}

func TestBinaryFileDataBasics(t *testing.T) {
	fd, err := newBinaryFileData(testHash, 42)
	if err != nil {
		t.Fatal(err)
	}
	if h := fd.GetHash(); h == nil || *h != testHash {
		t.Fatalf("GetHash = %v", h)
	}
	if b := fd.GetByteLength(); b == nil || *b != 42 {
		t.Fatalf("GetByteLength = %v", b)
	}
	if ed := fd.IsEditable(); ed == nil || *ed {
		t.Fatalf("IsEditable = %v, want false", fd.IsEditable())
	}
	if _, err = newBinaryFileData("nothex", 1); err == nil {
		t.Fatal("expected an error for a bad hash")
	}
}

func TestHollowVariants(t *testing.T) {
	// hollowString: stringLength 7
	hs, err := newHollowStringFileData(7)
	if err != nil {
		t.Fatal(err)
	}
	if s := hs.GetStringLength(); s == nil || *s != 7 {
		t.Fatalf("GetStringLength = %v", s)
	}
	if ed := hs.IsEditable(); ed == nil || !*ed {
		t.Fatal("hollowString should be editable")
	}
	if _, err = newHollowStringFileData(-1); err == nil {
		t.Fatal("expected an error for a negative stringLength")
	}

	// hollowBinary: byteLength 9
	hb, err := newHollowBinaryFileData(9)
	if err != nil {
		t.Fatal(err)
	}
	if b := hb.GetByteLength(); b == nil || *b != 9 {
		t.Fatalf("GetByteLength = %v", b)
	}
	if _, err = newHollowBinaryFileData(-3); err == nil {
		t.Fatal("expected an error for a negative byteLength")
	}
}

func TestHollowStringEditValidatesLength(t *testing.T) {
	// Node hollow_string_file_data.test.js: "validates string length when edited"
	maxLen := MaxStringLength
	fd, err := newHollowStringFileData(int64(maxLen))
	if err != nil {
		t.Fatal(err)
	}
	if s := fd.GetStringLength(); s == nil || *s != int64(maxLen) {
		t.Fatalf("GetStringLength = %v", s)
	}

	op := NewTextOperation()
	if err := op.Retain(maxLen, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := op.Insert("x", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := fd.Edit(NewTextEdit(op)); err == nil {
		t.Fatal("expected TooLongError")
	} else if !isErrType[*TooLongError](err) {
		t.Fatalf("expected *TooLongError, got %T (%v)", err, err)
	}
	if s := fd.GetStringLength(); s == nil || *s != int64(maxLen) {
		t.Fatalf("stringLength should be unchanged, = %v", s)
	}

	op2 := NewTextOperation()
	if err := op2.Retain(maxLen-1, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := op2.Remove(1); err != nil {
		t.Fatal(err)
	}
	if err := fd.Edit(NewTextEdit(op2)); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if s := fd.GetStringLength(); s == nil || *s != int64(maxLen-1) {
		t.Fatalf("stringLength = %v, want %d", s, maxLen-1)
	}
}

func TestCreateHollow(t *testing.T) {
	// byteLength > 0, no stringLength -> hollowBinary
	h := CreateHollow(5, nil)
	if _, ok := h.(*HollowBinaryFileData); !ok {
		t.Fatalf("CreateHollow(5,nil) = %T, want *HollowBinaryFileData", h)
	}

	// with stringLength -> hollowString
	sl := int64(4)
	h2 := CreateHollow(8, &sl)
	hs, ok := h2.(*HollowStringFileData)
	if !ok {
		t.Fatalf("CreateHollow(8,sl) = %T, want *HollowStringFileData", h2)
	}
	if s := hs.GetStringLength(); s == nil || *s != 4 {
		t.Fatalf("GetStringLength = %v", s)
	}

	// byteLength 0, no stringLength -> hollowBinary(0) (Node: byteLength ? string : binary)
	h3 := CreateHollow(0, nil)
	if _, ok := h3.(*HollowBinaryFileData); !ok {
		t.Fatalf("CreateHollow(0,nil) = %T, want *HollowBinaryFileData", h3)
	}
}

func TestCreateLazyFromBlobs(t *testing.T) {
	sl := int64(19)
	blob := &Blob{Hash: strings.Repeat("a", 40), ByteLength: 19, StringLength: &sl}
	fd, err := CreateLazyFromBlobs(blob, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fd.(*LazyStringFileData); !ok {
		t.Fatalf("CreateLazyFromBlobs(string blob) = %T, want *LazyStringFileData", fd)
	}

	blob2 := &Blob{Hash: strings.Repeat("b", 40), ByteLength: 5, StringLength: nil}
	fd2, err := CreateLazyFromBlobs(blob2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fd2.(*BinaryFileData); !ok {
		t.Fatalf("CreateLazyFromBlobs(binary blob) = %T, want *BinaryFileData", fd2)
	}
}
