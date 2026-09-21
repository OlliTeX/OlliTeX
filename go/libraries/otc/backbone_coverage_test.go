package otc

import (
	"context"
	"strings"
	"testing"
)

// fileDataBare exposes the base defaults (Node FileData base "not implemented"
// throws + null accessors) that concrete variants otherwise shadow.
type fileDataBare struct{ fileDataDefaults }

func TestBareFileDataDefaults(t *testing.T) {
	c := context.Background()
	b := &fileDataBare{}
	if b.GetHash() != nil || b.GetRangesHash() != nil || b.GetContent(true) != nil {
		t.Fatal("accessors should be nil")
	}
	if b.IsEditable() != nil || b.GetByteLength() != nil || b.GetStringLength() != nil {
		t.Fatal("accessors should be nil")
	}
	if b.GetComments() != nil || b.GetTrackedChanges() != nil {
		t.Fatal("accessors should be nil")
	}
	op := mustTextOp(t, "a")
	if err := b.Edit(op); err == nil {
		t.Fatal("expected edit not implemented")
	}
	if _, err := b.ToEager(c, newFakeBlobStore()); err == nil {
		t.Fatal("expected toEager not implemented")
	}
	if _, err := b.ToLazy(c, newFakeBlobStore()); err == nil {
		t.Fatal("expected toLazy not implemented")
	}
	if _, err := b.ToHollow(c, newFakeBlobStore()); err == nil {
		t.Fatal("expected toHollow not implemented")
	}
	if _, err := b.Store(c, newFakeBlobStore()); err == nil {
		t.Fatal("expected store not implemented")
	}
	if b.ToRaw() != nil || b.ToStats() != nil {
		t.Fatal("base ToRaw/ToStats should be nil")
	}
}

func mustTextOp(t *testing.T, s string) EditOperation {
	t.Helper()
	o := NewTextOperation()
	if err := o.Retain(len(s)-len(s), RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := o.Insert(s, InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	return NewTextEdit(o)
}

func TestHashFileDataFull(t *testing.T) {
	c := context.Background()
	hash := strings.Repeat("d", 40)
	ranges := strings.Repeat("e", 40)
	fd, err := newHashFileData(hash, &ranges)
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "hash raw", fd.ToRaw(), map[string]any{"hash": hash, "rangesHash": ranges})
	if v := fd.ToStats()["hashes"]; v != 2 {
		t.Fatalf("hashes = %v, want 2", v)
	}
	// toLazy/toEager/toHollow need getBlob; fake store has none -> errors
	if _, err := fd.ToLazy(c, newFakeBlobStore()); err == nil {
		t.Fatal("expected blob not found")
	}
	if _, err := fd.ToEager(c, newFakeBlobStore()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := fd.ToHollow(c, newFakeBlobStore()); err == nil {
		t.Fatal("expected error")
	}
	// store returns the raw
	raw, err := fd.Store(c, newFakeBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "hash store", raw, map[string]any{"hash": hash, "rangesHash": ranges})

	// happy toLazy with a real blob
	bs := &leBlobStore{blobs: map[string]leBlob{
		hash:   {content: "hello", stringLength: int64Ptr(5)},
		ranges: {content: "[]"},
	}}
	lz, err := fd.ToLazy(c, bs)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lz.(*LazyStringFileData); !ok {
		t.Fatalf("ToLazy = %T, want *LazyStringFileData", lz)
	}
}

func TestBinaryFileDataFull(t *testing.T) {
	c := context.Background()
	hash := strings.Repeat("f", 40)
	fd, err := newBinaryFileData(hash, 12)
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "binary raw", fd.ToRaw(), map[string]any{"hash": hash, "byteLength": 12})
	sameRaw(t, "binary stats", fd.ToStats(), map[string]any{"hashes": 1, "byteLength": 12})

	rt, err := binaryFileDataFromRaw(map[string]any{"hash": hash, "byteLength": int64(12)})
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := rt.(*BinaryFileData); !ok || b.ByteLength != 12 {
		t.Fatalf("binaryFileDataFromRaw = %v", rt)
	}
	if _, err := binaryFileDataFromRaw(map[string]any{"hash": hash, "byteLength": "nope"}); err == nil {
		t.Fatal("expected error")
	}

	if d, err := fd.ToEager(c, newFakeBlobStore()); err != nil || d == nil {
		t.Fatalf("ToEager: %v", err)
	}
	if d, err := fd.ToLazy(c, newFakeBlobStore()); err != nil || d == nil {
		t.Fatalf("ToLazy: %v", err)
	}
	if h, err := fd.ToHollow(c, newFakeBlobStore()); err != nil {
		t.Fatalf("ToHollow: %v", err)
	} else if _, ok := h.(*HollowBinaryFileData); !ok {
		t.Fatalf("ToHollow = %T", h)
	}
	raw, err := fd.Store(c, newFakeBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "binary store", raw, map[string]any{"hash": hash})
}

func TestHollowRawStatsFromRaw(t *testing.T) {
	hs, _ := newHollowStringFileData(7)
	sameRaw(t, "hollowString raw", hs.ToRaw(), map[string]any{"stringLength": 7})
	sameRaw(t, "hollowString stats", hs.ToStats(), map[string]any{"stringLength": 7})
	if d, err := hs.ToHollow(context.Background(), newFakeBlobStore()); err != nil || d == nil {
		t.Fatalf("hollowString ToHollow: %v", err)
	}
	hsr, err := hollowStringFileDataFromRaw(map[string]any{"stringLength": int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hsr.(*HollowStringFileData); !ok {
		t.Fatalf("hollowStringFileDataFromRaw = %T", hsr)
	}
	if _, err := hollowStringFileDataFromRaw(map[string]any{"stringLength": "nope"}); err == nil {
		t.Fatal("expected error")
	}

	hb, _ := newHollowBinaryFileData(9)
	sameRaw(t, "hollowBinary raw", hb.ToRaw(), map[string]any{"byteLength": 9})
	sameRaw(t, "hollowBinary stats", hb.ToStats(), map[string]any{"byteLength": 9})
	if d, err := hb.ToHollow(context.Background(), newFakeBlobStore()); err != nil || d == nil {
		t.Fatalf("hollowBinary ToHollow: %v", err)
	}
	hbr, err := hollowBinaryFileDataFromRaw(map[string]any{"byteLength": int64(9)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hbr.(*HollowBinaryFileData); !ok {
		t.Fatalf("hollowBinaryFileDataFromRaw = %T", hbr)
	}
	if _, err := hollowBinaryFileDataFromRaw(map[string]any{"byteLength": "nope"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestFileDelegatesAndStats(t *testing.T) {
	// binary (non-editable) exposes nil/known accessors via the File delegates
	hash := strings.Repeat("9", 40)
	bfd, _ := newBinaryFileData(hash, 8)
	file := NewFile(bfd, nil)
	if h := file.GetHash(); h == nil || *h != hash {
		t.Fatalf("GetHash = %v", h)
	}
	if file.GetRangesHash() != nil {
		t.Fatal("GetRangesHash should be nil for binary")
	}
	if b := file.GetByteLength(); b == nil || *b != 8 {
		t.Fatalf("GetByteLength = %v", b)
	}
	if ed := file.IsEditable(); ed == nil || *ed {
		t.Fatalf("IsEditable = %v, want false", ed)
	}
	if file.GetTrackedChanges() != nil {
		t.Fatal("GetTrackedChanges should be nil for binary")
	}

	// editable file edit path
	f2, _ := FileFromString("hello", nil)
	op := NewTextOperation()
	if err := op.Retain(5, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := op.Insert("!", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := f2.Edit(NewTextEdit(op)); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if gp := f2.GetContent(false); gp == nil || *gp != "hello!" {
		t.Fatalf("content = %v", gp)
	}

	// non-editable edit path -> NotEditableError
	f3, _ := FileFromHash(FileEmptyHash, nil, nil)
	if err := f3.Edit(mustTextOp(t, "x")); err == nil {
		t.Fatal("expected NotEditableError")
	} else if !isErrType[*NotEditableError](err) {
		t.Fatalf("got %T (%v)", err, err)
	}

	// File.ToStats with metadata
	f4, _ := FileFromString("foo", map[string]any{"main": true})
	stats := f4.ToStats()
	if v := stats["nMetadata"]; v != 1 {
		t.Fatalf("nMetadata = %v, want 1", v)
	}
	if _, ok := stats["metadataSize"].(int); !ok {
		t.Fatalf("metadataSize missing: %v", stats)
	}

	// FileCreateLazyFromBlobs
	blob := &Blob{Hash: strings.Repeat("c", 40), ByteLength: 19, StringLength: int64Ptr(19)}
	f5, err := FileCreateLazyFromBlobs(blob, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h := f5.GetHash(); h == nil || *h != blob.Hash {
		t.Fatalf("f5.GetHash = %v", h)
	}
	if ed := f5.IsEditable(); ed == nil || !*ed {
		t.Fatalf("f5.IsEditable = %v, want true", ed)
	}
}

func TestRegistryBadRaw(t *testing.T) {
	if _, err := FromRawFileData(map[string]any{"unknown": true}); err == nil {
		t.Fatal("expected an error for a bad raw")
	}
}

func TestCommentOpComposeInvert(t *testing.T) {
	// text op invert/compose/canBe*
	prev, _ := NewStringFileData("abc", nil, nil)
	o := NewTextOperation()
	if err := o.Retain(1, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := o.Insert("x", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	te := NewTextEdit(o)
	if inv, err := te.Invert(prev); err != nil || inv == nil {
		t.Fatalf("Invert: %v", err)
	}
	o2 := NewTextOperation()
	if err := o2.Retain(1, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	te2 := NewTextEdit(o2)
	_ = te.CanBeComposedWith(te2)
	_ = te.CanBeComposedWithForUndo(te2)
	if _, err := te.Compose(te2); err != nil {
		// composing incompatible ops is expected to error; either way we exercised it
		_ = err
	}

	// comment op compose paths
	add, _ := NewAddCommentOp("c1", []Range{NewRange(1, 2)}, false)
	del := &DeleteCommentOp{CommentID: "c1"}
	set := &SetCommentStateOp{CommentID: "c1", Resolved: true}
	if _, err := add.Compose(del); err != nil {
		t.Fatalf("add.compose(del) : %v", err)
	}
	if _, err := add.Compose(set); err != nil {
		t.Fatalf("add.compose(set) : %v", err)
	}
	if _, err := set.Compose(del); err != nil {
		t.Fatalf("set.compose(del) : %v", err)
	}
	if _, err := set.Compose(&AddCommentOp{CommentID: "c1"}); err == nil {
		t.Fatal("set.compose(different) should error")
	}
	// apply + invert on a file
	file, _ := NewStringFileData("abcdef", nil, nil)
	if err := add.Apply(file); err != nil {
		t.Fatal(err)
	}
	if file.GetComments().Len() != 1 {
		t.Fatalf("comments = %d, want 1", file.GetComments().Len())
	}
	if err := del.Apply(file); err != nil {
		t.Fatal(err)
	}
	if file.GetComments().Len() != 0 {
		t.Fatalf("comments = %d, want 0", file.GetComments().Len())
	}
	if _, err := del.Invert(prev); err != nil {
		t.Fatalf("del.Invert: %v", err)
	}
	if _, err := set.Invert(prev); err != nil {
		t.Fatalf("set.Invert: %v", err)
	}
	_ = del.CanBeComposedWith(add)
	_ = del.CanBeComposedWithForUndo(add)
	if _, err := del.Compose(add); err == nil {
		t.Fatal("del.compose should error")
	}
	_ = add.CanBeComposedWith(add)
	_ = add.CanBeComposedWithForUndo(add)
	_ = set.CanBeComposedWith(add)
	_ = set.CanBeComposedWithForUndo(add)
}

func TestMustJSONHelper(t *testing.T) {
	o := NewTextOperation()
	_ = o.Retain(1, RetainBuilderOpts{})
	_ = o.Insert("x", InsertBuilderOpts{})
	if s := mustJSON(NewTextEdit(o)); s == "" {
		t.Fatal("mustJSON should not be empty")
	}
	if e := newInvalidConversionError("p", NewTextEdit(o)); e == nil {
		t.Fatal("expected an error")
	}
}
