package otc

import (
	"context"
	"strings"
	"testing"
)

import "ollitex/go/libraries/oerror"

func setupLazyBlobStore(t *testing.T) (*fakeBlobStore, string, string) {
	t.Helper()
	fileHash := "a5675307b61ec2517330622a6e649b4ca1ee5612"
	rangesHash := "380de212d09bf8498065833dbf242aaf11184316"
	bs := newFakeBlobStore()
	bs.stringMap[fileHash] = "the quick brown fox"
	bs.objectMap[rangesHash] = map[string]any{
		"comments": []map[string]any{
			{"id": "foo", "ranges": []any{map[string]any{"pos": 0, "length": 3}}},
		},
		"trackedChanges": []map[string]any{
			{
				"range":    map[string]any{"pos": 4, "length": 5},
				"tracking": map[string]any{"type": "delete", "userId": "user1", "ts": "2024-01-01T00:00:00.000Z"},
			},
		},
	}
	return bs, fileHash, rangesHash
}

func TestLazyToRawFromRaw(t *testing.T) {
	ld, err := NewLazyStringFileData(FileEmptyHash, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "empty toRaw", ld.ToRaw(), map[string]any{"hash": FileEmptyHash, "stringLength": 0})

	rt, err := lazyStringFileDataFromRaw(ld.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if h := rt.GetHash(); h == nil || *h != FileEmptyHash {
		t.Fatalf("roundTrip GetHash = %v", h)
	}
	if s := rt.GetStringLength(); s == nil || *s != 0 {
		t.Fatalf("roundTrip GetStringLength = %v", s)
	}
	if len(rt.(*LazyStringFileData).GetOperations()) != 0 {
		t.Fatalf("roundTrip operations = %d, want 0", len(rt.(*LazyStringFileData).GetOperations()))
	}

	// edit insert 'a'
	op := NewTextOperation()
	if err := op.Insert("a", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := ld.Edit(NewTextEdit(op)); err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "toRaw after insert", ld.ToRaw(), map[string]any{
		"hash":         FileEmptyHash,
		"stringLength": 1,
		"operations":   []map[string]any{{"textOperation": []any{"a"}}},
	})

	rt2, err := lazyStringFileDataFromRaw(ld.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if h := rt2.GetHash(); h != nil {
		t.Fatalf("roundTrip GetHash = %v, want nil (has operations)", *h)
	}
	ops2 := rt2.(*LazyStringFileData).GetOperations()
	if len(ops2) != 1 {
		t.Fatalf("roundTrip operations = %d, want 1", len(ops2))
	}

	// rangesHash round-trip
	rh := "380de212d09bf8498065833dbf242aaf11184316"
	ld2, _ := NewLazyStringFileData(FileEmptyHash, &rh, 19, nil)
	sameRaw(t, "toRaw with ranges", ld2.ToRaw(), map[string]any{
		"hash": FileEmptyHash, "rangesHash": rh, "stringLength": 19,
	})
	rt3, _ := lazyStringFileDataFromRaw(ld2.ToRaw())
	if r := rt3.GetRangesHash(); r == nil || *r != rh {
		t.Fatalf("roundTrip GetRangesHash = %v", r)
	}
}

func TestLazyToEagerWithRanges(t *testing.T) {
	c := context.Background()
	bs, fileHash, rangesHash := setupLazyBlobStore(t)
	ld, err := NewLazyStringFileData(fileHash, &rangesHash, 19, nil)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := ld.ToEager(c, bs)
	if err != nil {
		t.Fatal(err)
	}
	sfd, ok := fd.(*StringFileData)
	if !ok {
		t.Fatalf("ToEager = %T, want *StringFileData", fd)
	}
	if gp := sfd.GetContent(false); gp == nil || *gp != "the quick brown fox" {
		t.Fatalf("content = %v", gp)
	}
	sameRaw(t, "eager comments", sfd.GetComments().ToRaw(), []map[string]any{
		{"id": "foo", "ranges": []any{map[string]any{"pos": 0, "length": 3}}},
	})
	sameRaw(t, "eager trackedChanges", sfd.GetTrackedChanges().ToRaw(), []map[string]any{
		{
			"range":    map[string]any{"pos": 4, "length": 5},
			"tracking": map[string]any{"type": "delete", "userId": "user1", "ts": "2024-01-01T00:00:00.000Z"},
		},
	})
}

func TestLazyToEagerNoRanges(t *testing.T) {
	c := context.Background()
	bs, fileHash, _ := setupLazyBlobStore(t)
	ld, err := NewLazyStringFileData(fileHash, nil, 19, nil)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := ld.ToEager(c, bs)
	if err != nil {
		t.Fatal(err)
	}
	sfd, ok := fd.(*StringFileData)
	if !ok {
		t.Fatalf("ToEager = %T, want *StringFileData", fd)
	}
	if gp := sfd.GetContent(false); gp == nil || *gp != "the quick brown fox" {
		t.Fatalf("content = %v", gp)
	}
	if cc := sfd.GetComments(); cc == nil || cc.Len() != 0 {
		t.Fatalf("comments should be empty, got %v", cc)
	}
}

func TestLazyEditValidates(t *testing.T) {
	ld, err := NewLazyStringFileData(FileEmptyHash, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h := ld.GetHash(); h == nil || *h != FileEmptyHash {
		t.Fatalf("GetHash = %v", h)
	}
	if b := ld.GetByteLength(); b == nil || *b != 0 {
		t.Fatalf("GetByteLength = %v", b)
	}
	if s := ld.GetStringLength(); s == nil || *s != 0 {
		t.Fatalf("GetStringLength = %v", s)
	}

	op := NewTextOperation()
	if err := op.Insert("a", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := ld.Edit(NewTextEdit(op)); err != nil {
		t.Fatal(err)
	}
	if h := ld.GetHash(); h != nil {
		t.Fatalf("GetHash = %v, want nil after edit", *h)
	}
	if s := ld.GetStringLength(); s == nil || *s != 1 {
		t.Fatalf("GetStringLength = %v, want 1", s)
	}
	if len(ld.GetOperations()) != 1 {
		t.Fatalf("operations = %d, want 1", len(ld.GetOperations()))
	}

	// retain(10) with length 1 -> ApplyError (type preserved)
	bad := NewTextOperation()
	if err := bad.Retain(10, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	err = ld.Edit(NewTextEdit(bad))
	if err == nil {
		t.Fatal("expected ApplyError")
	} else if !isErrType[*ApplyError](err) {
		t.Fatalf("expected *ApplyError, got %T (%v)", err, err)
	}
	if s := ld.GetStringLength(); s == nil || *s != 1 {
		t.Fatalf("GetStringLength = %v, want 1 (unchanged)", s)
	}
	if len(ld.GetOperations()) != 1 {
		t.Fatalf("operations = %d, want 1 (unchanged)", len(ld.GetOperations()))
	}
}

func TestLazyEditTooLong(t *testing.T) {
	ld, _ := NewLazyStringFileData(FileEmptyHash, nil, 0, nil)
	long := strings.Repeat("a", MaxStringLength)
	op := NewTextOperation()
	if err := op.Insert(long, InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := ld.Edit(NewTextEdit(op)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s := ld.GetStringLength(); s == nil || *s != int64(len(long)) {
		t.Fatalf("GetStringLength = %v, want %d", s, len(long))
	}

	bad := NewTextOperation()
	if err := bad.Retain(len(long), RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := bad.Insert("x", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	err := ld.Edit(NewTextEdit(bad))
	if err == nil {
		t.Fatal("expected TooLongError")
	} else if !isErrType[*TooLongError](err) {
		t.Fatalf("expected *TooLongError, got %T (%v)", err, err)
	}
}

func TestLazyStoreTruncates(t *testing.T) {
	c := context.Background()
	bs := newFakeBlobStore()
	bs.stringMap[FileEmptyHash] = ""
	ld, err := NewLazyStringFileData(FileEmptyHash, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	op := NewTextOperation()
	if err := op.Insert("abc", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := ld.Edit(NewTextEdit(op)); err != nil {
		t.Fatal(err)
	}
	stored, err := ld.Store(c, bs)
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := stored["hash"].(string); !ok || h == "" {
		t.Fatalf("stored hash = %v", stored["hash"])
	}
	// after store, operations are truncated and hash is updated
	if got := ld.Hash; got != stored["hash"].(string) {
		t.Fatalf("ld.Hash = %q, stored = %q", got, stored["hash"])
	}
	if len(ld.GetOperations()) != 0 {
		t.Fatalf("operations after store = %d, want 0", len(ld.GetOperations()))
	}
}

func TestLazyErrorAnnotationToEager(t *testing.T) {
	c := context.Background()
	bs, fileHash, _ := setupLazyBlobStore(t)
	// one op with base length 999 (won't match the 19-char blob); stringLength forced to 999
	badOp := NewTextOperation()
	if err := badOp.Retain(999, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	ld, _ := NewLazyStringFileData(fileHash, nil, 999, []EditOperation{NewTextEdit(badOp)})
	_, err := ld.ToEager(c, bs)
	if err == nil {
		t.Fatal("expected an error")
	}
	info := oerror.GetFullInfo(err)
	expectInfo := map[string]any{
		"blobHash":               fileHash,
		"blobContentLength":      19,
		"metadataStringLength":   999,
		"totalOperations":        1,
		"operationIndex":         0,
		"currentContentLength":   19,
		"firstOpBaseLength":      999,
		"contentMatchesMetadata": false,
		"contentMatchesFirstOp":  false,
	}
	for k, v := range expectInfo {
		if info[k] != v {
			t.Fatalf("info[%q] = %v, want %v (full: %v)", k, info[k], v, info)
		}
	}
}

func TestLazyErrorAnnotationEdit(t *testing.T) {
	bs, fileHash, _ := setupLazyBlobStore(t)
	_ = bs
	ld, _ := NewLazyStringFileData(fileHash, nil, 19, nil)
	good := NewTextOperation()
	if err := good.Retain(19, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := good.Insert("!", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := ld.Edit(NewTextEdit(good)); err != nil {
		t.Fatal(err)
	}
	badOp := NewTextOperation()
	if err := badOp.Retain(999, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	err := ld.Edit(NewTextEdit(badOp))
	if err == nil {
		t.Fatal("expected an error")
	}
	info := oerror.GetFullInfo(err)
	expectInfo := map[string]any{
		"blobHash":                fileHash,
		"metadataStringLength":    20,
		"operationBaseLength":     999,
		"totalExistingOperations": 1,
	}
	for k, v := range expectInfo {
		if info[k] != v {
			t.Fatalf("info[%q] = %v, want %v (full: %v)", k, info[k], v, info)
		}
	}
}

func TestLazyErrorAnnotationApplyOps(t *testing.T) {
	c := context.Background()
	bs, fileHash, _ := setupLazyBlobStore(t)
	good := NewTextOperation()
	if err := good.Retain(19, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := good.Insert("!", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	badOp := NewTextOperation()
	if err := badOp.Retain(999, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	ld, _ := NewLazyStringFileData(fileHash, nil, 999, []EditOperation{NewTextEdit(good), NewTextEdit(badOp)})
	_, err := ld.ToEager(c, bs)
	if err == nil {
		t.Fatal("expected an error")
	}
	info := oerror.GetFullInfo(err)
	expectInfo := map[string]any{
		"operationIndex":       1,
		"totalOperations":      2,
		"currentContentLength": 20,
		"blobHash":             fileHash,
		"blobContentLength":    19,
		"metadataStringLength": 999,
	}
	for k, v := range expectInfo {
		if info[k] != v {
			t.Fatalf("info[%q] = %v, want %v (full: %v)", k, info[k], v, info)
		}
	}
}
