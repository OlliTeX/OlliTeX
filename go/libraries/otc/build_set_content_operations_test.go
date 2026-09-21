package otc

// build_set_content_operations_test.go — oracle-pinned port of
// `test/unit/build_set_content_operations.test.js`. The Go assertions mirror
// the chai expectations (status, operation type, metadata, blob contents).

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	oerror "ollitex/go/libraries/oerror"
)

// --- in-memory blob store (Node: InMemoryBlobStore) -----------------------

type bssBlob struct {
	content      string
	stringLength *int64
}

type bssBlobStore struct {
	blobs map[string]bssBlob
}

func newBSSBlobStore() *bssBlobStore { return &bssBlobStore{blobs: map[string]bssBlob{}} }

func (s *bssBlobStore) put(content string, stringLength *int64) *Blob {
	hash := BlobHashFromString(content)
	b := &Blob{Hash: hash, ByteLength: int64(len(content)), StringLength: stringLength}
	s.blobs[hash] = bssBlob{content: content, stringLength: stringLength}
	return b
}

// PutString stores an editable (string) blob. Oracle inputs are ASCII, so byte
// length == code units.
func (s *bssBlobStore) PutString(_ context.Context, content string) (*Blob, error) {
	sl := int64(len(content))
	return s.put(content, &sl), nil
}

// putBinary stores a blob with no string length (binary content).
func (s *bssBlobStore) putBinary(content string) *Blob { return s.put(content, nil) }

func (s *bssBlobStore) PutObject(_ context.Context, obj map[string]any) (*Blob, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return s.PutString(context.Background(), string(b))
}

func (s *bssBlobStore) GetBlob(_ context.Context, hash string) (*Blob, error) {
	bl, ok := s.blobs[hash]
	if !ok {
		return nil, nil
	}
	return &Blob{Hash: hash, ByteLength: int64(len(bl.content)), StringLength: bl.stringLength}, nil
}

func (s *bssBlobStore) GetString(_ context.Context, hash string) (string, error) {
	bl, ok := s.blobs[hash]
	if !ok {
		return "", &BlobNotFoundError{Hash: hash}
	}
	return bl.content, nil
}

func (s *bssBlobStore) GetObject(_ context.Context, hash string) (map[string]any, error) {
	str, err := s.GetString(context.Background(), hash)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(str), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// --- shared fixtures -------------------------------------------------------

var (
	bssTS       = time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	bssTrack    = &BuildSetContentTracking{UserID: "user-1", TS: bssTS}
	bssPathname = "main.tex"
	bssImported = "2026-01-02T03:04:05.678Z"
)

func bssRun(t *testing.T, args BuildSetContentArgs) (*BuildSetContentResult, error) {
	t.Helper()
	if args.BlobStore == nil {
		t.Fatal("BlobStore required")
	}
	return BuildSetContentOperations(context.Background(), args)
}

func bssAs[T any](val any, name string) T {
	v, ok := val.(T)
	if !ok {
		panic("expected " + name)
	}
	return v
}

func bssApplyToFresh(t *testing.T, op Operation, fresh *File) *File {
	t.Helper()
	fm, err := NewFileMap(map[string]*File{bssPathname: fresh})
	if err != nil {
		t.Fatalf("newFileMap: %v", err)
	}
	snap := NewSnapshot(fm, nil, nil, nil)
	if err := op.ApplyTo(snap); err != nil {
		t.Fatalf("applyTo: %v", err)
	}
	return snap.GetFile(bssPathname)
}

func TestBSS_RequiresExactlyOneOfContentAndBlob(t *testing.T) {
	bs := newBSSBlobStore()
	cases := []BuildSetContentArgs{
		{File: nil, Pathname: bssPathname, BlobStore: bs},
		{File: nil, Pathname: bssPathname, Content: strPtr("a"), Blob: &Blob{Hash: BlobHashFromString("a"), ByteLength: 1, StringLength: int64Ptr(1)}, BlobStore: bs},
	}
	for i, args := range cases {
		_, err := bssRun(t, args)
		if err == nil {
			t.Fatalf("case %d: expected an error", i)
		}
		if !strings.Contains(err.Error(), "exactly one of content and blob") {
			t.Fatalf("case %d: unexpected message: %v", i, err)
		}
		if !isOError(err) {
			t.Fatalf("case %d: expected an OError, got %T", i, err)
		}
	}
}

func TestBSS_EditsExistingEditableWithMinimalDiff(t *testing.T) {
	bs := newBSSBlobStore()
	file, _ := FileFromString("hello cruel world", nil)
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr("hello brave world"), BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "applied" {
		t.Fatalf("status = %q", res.Status)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	op := bssAs[*EditFileOperation](res.Operations[0], "EditFileOperation")
	if op.GetPathname() != bssPathname {
		t.Fatalf("pathname = %q", op.GetPathname())
	}
	fresh, _ := FileFromString("hello cruel world", nil)
	updated := bssApplyToFresh(t, op, fresh)
	if updated.GetContent(false) == nil || *updated.GetContent(false) != "hello brave world" {
		t.Fatalf("content after edit = %v", updated.GetContent(false))
	}
}

func TestBSS_EagerlyLoadsLazyEditableBeforeDiffing(t *testing.T) {
	bs := newBSSBlobStore()
	blob, _ := bs.PutString(context.Background(), "one\ntwo\nthree")
	file, _ := FileCreateLazyFromBlobs(blob, nil, nil)
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr("one\ntwo and a half\nthree"), BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	bssAs[*EditFileOperation](res.Operations[0], "EditFileOperation")
}

func TestBSS_IdenticalContentIsNoop(t *testing.T) {
	bs := newBSSBlobStore()
	file, _ := FileFromString("hello world", nil)
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr("hello world"), BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "noop" {
		t.Fatalf("status = %q", res.Status)
	}
	if len(res.Operations) != 0 {
		t.Fatalf("operations = %v", res.Operations)
	}
}

func TestBSS_ClearsMetadataWhenNotProvided(t *testing.T) {
	bs := newBSSBlobStore()
	file, _ := FileFromString("hello world", map[string]any{"main": true})
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr("hello world"), BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "applied" {
		t.Fatalf("status = %q", res.Status)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	op := bssAs[*SetFileMetadataOperation](res.Operations[0], "SetFileMetadataOperation")
	if !reflect.DeepEqual(op.GetMetadata(), map[string]any{}) {
		t.Fatalf("metadata = %v", op.GetMetadata())
	}
}

func TestBSS_UpdatesMetadataWhenProvided(t *testing.T) {
	bs := newBSSBlobStore()
	file, _ := FileFromString("hello world", nil)
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr("hello brave world"), Metadata: map[string]any{"main": true}, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Operations) != 2 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	bssAs[*EditFileOperation](res.Operations[0], "EditFileOperation")
	op := bssAs[*SetFileMetadataOperation](res.Operations[1], "SetFileMetadataOperation")
	if !reflect.DeepEqual(op.GetMetadata(), map[string]any{"main": true}) {
		t.Fatalf("metadata = %v", op.GetMetadata())
	}
}

func TestBSS_UpdatesMetadataWhenContentUnchanged(t *testing.T) {
	bs := newBSSBlobStore()
	file, _ := FileFromString("hello world", nil)
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr("hello world"), Metadata: map[string]any{"main": true}, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "applied" {
		t.Fatalf("status = %q", res.Status)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	bssAs[*SetFileMetadataOperation](res.Operations[0], "SetFileMetadataOperation")
}

func TestBSS_IdenticalContentAndMetadataIsNoop(t *testing.T) {
	bs := newBSSBlobStore()
	file, _ := FileFromString("hello world", map[string]any{"main": true})
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr("hello world"), Metadata: map[string]any{"main": true}, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "noop" {
		t.Fatalf("status = %q", res.Status)
	}
}

func TestBSS_TrackedWholeDocDelete(t *testing.T) {
	bs := newBSSBlobStore()
	file, _ := FileFromString("abc", nil)
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr(""), Tracking: bssTrack, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "applied" {
		t.Fatalf("status = %q", res.Status)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
}

func TestBSS_ReplacesBinaryWithDoc(t *testing.T) {
	bs := newBSSBlobStore()
	existing := bs.putBinary("%PDF-1.5")
	file, _ := FileCreateLazyFromBlobs(existing, nil, nil)
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr("hello world"), Metadata: map[string]any{"main": true}, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "applied" {
		t.Fatalf("status = %q", res.Status)
	}
	if len(res.Operations) != 2 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	remove := bssAs[*MoveFileOperation](res.Operations[0], "MoveFileOperation")
	if !remove.IsRemoveFile() {
		t.Fatalf("expected a remove-file operation")
	}
	add := bssAs[*AddFileOperation](res.Operations[1], "AddFileOperation")
	if h := add.GetFile().GetHash(); h == nil || *h != BlobHashFromString("hello world") {
		t.Fatalf("hash = %v", h)
	}
	if sl := add.GetFile().GetStringLength(); sl == nil || *sl != int64(len("hello world")) {
		t.Fatalf("stringLength = %v", sl)
	}
	if !reflect.DeepEqual(add.GetFile().GetMetadata(), map[string]any{"main": true}) {
		t.Fatalf("metadata = %v", add.GetFile().GetMetadata())
	}
	got, err := bs.GetString(context.Background(), BlobHashFromString("hello world"))
	if err != nil || got != "hello world" {
		t.Fatalf("getString = %q, %v", got, err)
	}
}

func TestBSS_BinaryIdenticalContentIsNoop(t *testing.T) {
	bs := newBSSBlobStore()
	existing := bs.putBinary("hello world")
	file, _ := FileCreateLazyFromBlobs(existing, nil, map[string]any{"main": true})
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Content: strPtr("hello world"), Metadata: map[string]any{"main": true}, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "noop" {
		t.Fatalf("status = %q", res.Status)
	}
}

func TestBSS_CreatesNewDocWhenNoFile(t *testing.T) {
	bs := newBSSBlobStore()
	res, err := bssRun(t, BuildSetContentArgs{File: nil, Pathname: bssPathname, Content: strPtr("hello world"), BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	bssAs[*AddFileOperation](res.Operations[0], "AddFileOperation")
}

func TestBSS_TrackedNewDocInsert(t *testing.T) {
	bs := newBSSBlobStore()
	res, err := bssRun(t, BuildSetContentArgs{File: nil, Pathname: bssPathname, Content: strPtr("hello world"), Tracking: bssTrack, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	add := bssAs[*AddFileOperation](res.Operations[0], "AddFileOperation")
	rangesHash := add.GetFile().GetRangesHash()
	if rangesHash == nil {
		t.Fatal("expected a rangesHash")
	}
	ranges, err := bs.GetObject(context.Background(), *rangesHash)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{
		"comments": []any{},
		"trackedChanges": []any{
			map[string]any{
				"range":    map[string]any{"pos": float64(0), "length": float64(len("hello world"))},
				"tracking": map[string]any{"type": "insert", "userId": "user-1", "ts": "2026-07-10T00:00:00.000Z"},
			},
		},
	}
	if !reflect.DeepEqual(ranges, expected) {
		t.Fatalf("ranges = %#v, want %#v", ranges, expected)
	}
}

func TestBSS_NoRangesBlobForEmptyTrackedContent(t *testing.T) {
	bs := newBSSBlobStore()
	res, err := bssRun(t, BuildSetContentArgs{File: nil, Pathname: bssPathname, Content: strPtr(""), Tracking: bssTrack, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	add := bssAs[*AddFileOperation](res.Operations[0], "AddFileOperation")
	if add.GetFile().GetRangesHash() != nil {
		t.Fatalf("expected no rangesHash, got %v", add.GetFile().GetRangesHash())
	}
}

func TestBSS_BlobReplacesExistingBinary(t *testing.T) {
	bs := newBSSBlobStore()
	existing := bs.putBinary("old bytes")
	file, _ := FileCreateLazyFromBlobs(existing, nil, nil)
	newBlob := bs.putBinary("new bytes")
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Blob: newBlob, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "applied" {
		t.Fatalf("status = %q", res.Status)
	}
	if len(res.Operations) != 2 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	add := bssAs[*AddFileOperation](res.Operations[1], "AddFileOperation")
	if h := add.GetFile().GetHash(); h == nil || *h != newBlob.GetHash() {
		t.Fatalf("hash = %v", h)
	}
}

func TestBSS_BlobConvertsDocToBinary(t *testing.T) {
	bs := newBSSBlobStore()
	docBlob, _ := bs.PutString(context.Background(), "hello world")
	file, _ := FileCreateLazyFromBlobs(docBlob, nil, nil)
	newBlob := bs.putBinary("%PDF-1.5")
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Blob: newBlob, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Operations) != 2 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	remove := bssAs[*MoveFileOperation](res.Operations[0], "MoveFileOperation")
	if !remove.IsRemoveFile() {
		t.Fatalf("expected a remove-file operation")
	}
	add := bssAs[*AddFileOperation](res.Operations[1], "AddFileOperation")
	if add.GetFile().GetStringLength() != nil {
		t.Fatalf("expected null stringLength, got %v", add.GetFile().GetStringLength())
	}
}

func TestBSS_BlobSameHashAndMetadataIsNoop(t *testing.T) {
	bs := newBSSBlobStore()
	blob := bs.putBinary("same bytes")
	file, _ := FileCreateLazyFromBlobs(blob, nil, map[string]any{"main": true})
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Blob: blob, Metadata: map[string]any{"main": true}, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "noop" {
		t.Fatalf("status = %q", res.Status)
	}
	if len(res.Operations) != 0 {
		t.Fatalf("operations = %v", res.Operations)
	}
}

func TestBSS_BlobOnlySetsMetadataWhenHashMatches(t *testing.T) {
	bs := newBSSBlobStore()
	blob := bs.putBinary("same bytes")
	file, _ := FileCreateLazyFromBlobs(blob, nil, map[string]any{"main": false})
	res, err := bssRun(t, BuildSetContentArgs{File: file, Pathname: bssPathname, Blob: blob, Metadata: map[string]any{"main": true}, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "applied" {
		t.Fatalf("status = %q", res.Status)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	op := bssAs[*SetFileMetadataOperation](res.Operations[0], "SetFileMetadataOperation")
	if !reflect.DeepEqual(op.GetMetadata(), map[string]any{"main": true}) {
		t.Fatalf("metadata = %v", op.GetMetadata())
	}
}

func TestBSS_BlobAddsNewFileWhenNoFile(t *testing.T) {
	bs := newBSSBlobStore()
	blob := bs.putBinary("new bytes")
	res, err := bssRun(t, BuildSetContentArgs{File: nil, Pathname: bssPathname, Blob: blob, Metadata: map[string]any{"importedAt": bssImported}, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Operations) != 1 {
		t.Fatalf("operations length = %d", len(res.Operations))
	}
	op := bssAs[*AddFileOperation](res.Operations[0], "AddFileOperation")
	if !reflect.DeepEqual(op.GetFile().GetMetadata(), map[string]any{"importedAt": bssImported}) {
		t.Fatalf("metadata = %v", op.GetFile().GetMetadata())
	}
}

func TestBSS_BlobIgnoresTrackingOption(t *testing.T) {
	bs := newBSSBlobStore()
	blob := bs.putBinary("new bytes")
	res, err := bssRun(t, BuildSetContentArgs{File: nil, Pathname: bssPathname, Blob: blob, Tracking: bssTrack, BlobStore: bs})
	if err != nil {
		t.Fatal(err)
	}
	op := bssAs[*AddFileOperation](res.Operations[0], "AddFileOperation")
	if op.GetFile().GetRangesHash() != nil {
		t.Fatalf("expected no rangesHash, got %v", op.GetFile().GetRangesHash())
	}
}

// --- tiny helpers ----------------------------------------------------------

func isOError(err error) bool {
	_, ok := err.(*oerror.OError)
	return ok
}
