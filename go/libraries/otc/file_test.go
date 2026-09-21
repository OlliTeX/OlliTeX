package otc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// leBlobStore is a loadEager-oriented blob store (Node: file.test.js
// `blobStoreOf`): getBlob returns a Blob whose stringLength is set only for
// "editable" blobs.
type leBlob struct {
	content      string
	stringLength *int64
}

type leBlobStore struct {
	blobs map[string]leBlob
}

func (b *leBlobStore) GetBlob(_ context.Context, hash string) (*Blob, error) {
	bl, ok := b.blobs[hash]
	if !ok {
		return nil, nil
	}
	return &Blob{Hash: hash, ByteLength: int64(len(bl.content)), StringLength: bl.stringLength}, nil
}
func (b *leBlobStore) GetString(_ context.Context, hash string) (string, error) {
	bl, ok := b.blobs[hash]
	if !ok {
		return "", &BlobNotFoundError{Hash: hash}
	}
	return bl.content, nil
}
func (b *leBlobStore) GetObject(_ context.Context, hash string) (map[string]any, error) {
	s, err := b.GetString(context.Background(), hash)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}
func (b *leBlobStore) PutString(context.Context, string) (*Blob, error) {
	return nil, nil
}
func (b *leBlobStore) PutObject(context.Context, map[string]any) (*Blob, error) {
	return nil, nil
}

func TestFileMetadata(t *testing.T) {
	file, err := FileFromString("foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := file.GetMetadata(); len(got) != 0 {
		t.Fatalf("GetMetadata = %v, want {}", got)
	}

	file2, err := FileFromString("foo", map[string]any{"main": true})
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "constructor metadata", file2.GetMetadata(), map[string]any{"main": true})

	linked := map[string]any{
		"provider":           "project_file",
		"source_project_id":  "507f1f77bcf86cd799439011",
		"source_entity_path": "/foo.bib",
		"importedAt":         "2024-08-05T11:53:34.532Z",
	}
	file2.SetMetadata(linked)
	sameRaw(t, "setMetadata", file2.GetMetadata(), linked)
}

func TestFileToRaw(t *testing.T) {
	meta := map[string]any{"main": true}
	file, err := FileFromHash(FileEmptyHash, nil, meta)
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "toRaw with metadata", file.ToRaw(), map[string]any{
		"hash": FileEmptyHash, "metadata": map[string]any{"main": true},
	})

	file.SetMetadata(nil)
	sameRaw(t, "toRaw empty metadata omitted", file.ToRaw(), map[string]any{"hash": FileEmptyHash})

	// deep clone of metadata
	meta2 := map[string]any{
		"provider":   "url",
		"url":        "https://example.com/foo.bib",
		"importedAt": "2024-08-05T11:53:34.532Z",
	}
	file2, _ := FileFromHash(FileEmptyHash, nil, meta2)
	raw := file2.ToRaw()
	rawMeta, ok := raw["metadata"].(map[string]any)
	if !ok {
		t.Fatal("raw.metadata missing")
	}
	if &rawMeta == &file2.Metadata {
		t.Fatal("raw.metadata must be a deep clone, not the same map")
	}
	sameRaw(t, "raw metadata deep-equal", rawMeta, file2.GetMetadata())
}

func TestFileStore(t *testing.T) {
	c := context.Background()
	file, err := FileFromHash(FileEmptyHash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := file.Store(c, newFakeBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "store no metadata", raw, map[string]any{"hash": FileEmptyHash})

	meta := map[string]any{"main": true}
	file2, _ := FileFromHash(FileEmptyHash, nil, meta)
	raw2, err := file2.Store(c, newFakeBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "store with metadata", raw2, map[string]any{
		"hash": FileEmptyHash, "metadata": map[string]any{"main": true},
	})

	// deep clone: mutating raw metadata must not affect the file's metadata
	meta3 := map[string]any{"externalFile": map[string]any{"id": 123}}
	file3, _ := FileFromHash(FileEmptyHash, nil, meta3)
	raw3, _ := file3.Store(c, newFakeBlobStore())
	raw3["metadata"].(map[string]any)["externalFile"].(map[string]any)["id"] = 456
	if got := file3.GetMetadata()["externalFile"].(map[string]any)["id"]; got != 123 {
		t.Fatalf("file metadata should be unaffected, got id=%v", got)
	}
}

func TestFileFromContent(t *testing.T) {
	file, err := FileFromString("foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if gp := file.GetContent(false); gp == nil || *gp != "foo" {
		t.Fatalf("GetContent = %v", gp)
	}
}

func TestFileLoadEager(t *testing.T) {
	c := context.Background()
	hash := strings.Repeat("a", 40)

	// editable: blob has a stringLength
	bs := &leBlobStore{blobs: map[string]leBlob{hash: {content: "hello", stringLength: int64Ptr(5)}}}
	file, err := FileFromHash(hash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := file.LoadEager(c, bs)
	if err != nil {
		t.Fatal(err)
	}
	if gp := data.GetContent(false); gp == nil || *gp != "hello" {
		t.Fatalf("data.GetContent = %v", gp)
	}
	if gp := file.GetContent(false); gp == nil || *gp != "hello" {
		t.Fatalf("file.GetContent = %v", gp)
	}

	// already-eager content
	file2, _ := FileFromString("foo", nil)
	data2, err := file2.LoadEager(c, newFakeBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	if gp := data2.GetContent(false); gp == nil || *gp != "foo" {
		t.Fatalf("data2.GetContent = %v", gp)
	}

	// non-editable blob (no stringLength) -> NotEditableError
	bs3 := &leBlobStore{blobs: map[string]leBlob{hash: {content: "hello"}}}
	file3, _ := FileFromHash(hash, nil, nil)
	if _, err := file3.LoadEager(c, bs3); err == nil {
		t.Fatal("expected NotEditableError")
	} else if !isErrType[*NotEditableError](err) {
		t.Fatalf("expected *NotEditableError, got %T (%v)", err, err)
	}
}

func TestFileHollowClone(t *testing.T) {
	z := int64(0)
	file := FileCreateHollow(0, &z, nil)
	if s := file.GetStringLength(); s == nil || *s != 0 {
		t.Fatalf("GetStringLength = %v", s)
	}
	clone := file.Clone()
	if s := clone.GetStringLength(); s == nil || *s != 0 {
		t.Fatalf("clone GetStringLength = %v", s)
	}
}

func TestFileGetComments(t *testing.T) {
	file, err := FileFromString("foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw := file.GetComments().ToRaw(); len(raw) != 0 {
		t.Fatalf("GetComments().ToRaw() = %v, want []", raw)
	}
}
