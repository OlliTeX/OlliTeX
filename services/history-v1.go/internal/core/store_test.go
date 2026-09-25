package core

import (
	"encoding/json"
	"testing"
	"time"
)

const rfcISO = "2006-01-02T15:04:05.000Z"

func parseTs(t *testing.T, iso string) time.Time {
	ts, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		t.Fatalf("parseTs: %v", err)
	}
	return ts
}

// assertP: got parse-equivalent to want (wire fixture).
func assertP(t *testing.T, got, want []byte) {
	if !parseEq(t, got, want) {
		t.Fatalf("wire = %s, want %s (parse-eq oracle)", got, want)
	}
}

func repeat40(c byte) string {
	out := make([]byte, 40)
	for i := range out {
		out[i] = c
	}
	return string(out)
}

// countedBlobStore — deterministic fake mirroring the Node oracle fake:
// puts -> "hS<n>" (string) / "hO<n>" (object).
type countedBlobStore struct {
	strings map[string]string
	objects map[string]string
	nextStr int
	nextObj int
}

func newCountedBlobStore() *countedBlobStore {
	return &countedBlobStore{
		strings: map[string]string{},
		objects: map[string]string{},
	}
}

func (c *countedBlobStore) PutString(content string) (string, error) {
	h := "hS" + itoa(c.nextStr)
	c.nextStr++
	c.strings[h] = content
	return h, nil
}

func (c *countedBlobStore) PutObject(data []byte) (string, error) {
	h := "hO" + itoa(c.nextObj)
	c.nextObj++
	c.objects[h] = string(data)
	return h, nil
}

func (c *countedBlobStore) GetHashBlob(hash string) (string, error) {
	if s, ok := c.strings[hash]; ok {
		return s, nil
	}
	return "", blobNF{hash: hash}
}

// StringBlob — metadata for the counted fake: string/object blobs have a
// stringLength; no binary blobs are modelled here (Node binary case is
// pinned in the blobstore package tests via ProjectBlobStore).
func (c *countedBlobStore) StringBlob(hash string) (byteLength, stringLength int, found bool) {
	if s, ok := c.strings[hash]; ok {
		return len(s), utf16Length(s), true
	}
	if o, ok := c.objects[hash]; ok {
		return len(o), utf16Length(o), true
	}
	return 0, 0, false
}

func (c *countedBlobStore) GetRangesBlob(hash string) (json.RawMessage, error) {
	if s, ok := c.objects[hash]; ok {
		return json.RawMessage(s), nil
	}
	return nil, blobNF{hash: hash}
}

type blobNF struct{ hash string }

func (e blobNF) Error() string { return "blob not found: " + e.hash }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for x := n; x > 0; x /= 10 {
		b = append([]byte{byte('0' + x%10)}, b...)
	}
	return string(b)
}

// --- File.Store (Node File.store): attaches metadata when present ---

func TestFileStoreAttachesMetadata(t *testing.T) {
	// string kind: content into blob, hash back, metadata attached.
	bs := newCountedBlobStore()
	f := &File{Kind: "string", Content: "hello world", Metadata: json.RawMessage(`{"m":[1,2]}`)}
	got, err := f.Store(bs)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	assertP(t, got, []byte(`{"hash":"hS0","metadata":{"m":[1,2]}}`))
	if got := bs.strings["hS0"]; got != "hello world" {
		t.Fatalf("string blob = %q, want \"hello world\"", got)
	}

	// no metadata -> hash only.
	bs2 := newCountedBlobStore()
	got2, err := (&File{Kind: "string", Content: "x"}).Store(bs2)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	assertP(t, got2, []byte(`{"hash":"hS0"}`))

	// hash kind: no blob write, metadata attached.
	fh := &File{Kind: "hash", Hash: repeat40('a'), RangesHash: repeat40('b'), Metadata: json.RawMessage(`{"mm":1}`)}
	got3, err := fh.Store(bs2)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	assertP(t, got3, []byte(`{"hash":"`+repeat40('a')+`","rangesHash":"`+repeat40('b')+`","metadata":{"mm":1}}`))
	if len(bs2.strings) != 1 || len(bs2.objects) != 0 {
		t.Fatalf("hash store must not write blobs: strings=%d objects=%d", len(bs2.strings), len(bs2.objects))
	}
	// hash kind without metadata.
	fh2 := &File{Kind: "hash", Hash: repeat40('c')}
	got4, err := fh2.Store(bs2)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	assertP(t, got4, []byte(`{"hash":"`+repeat40('c')+`"}`))
}

// --- Snapshot.Store (Node Snapshot.store) ---

func TestSnapshotStore(t *testing.T) {
	bs := newCountedBlobStore()
	s := SnapshotFromRaw(json.RawMessage(`{"files":{}}`))
	s.SetProjectVersion("1.2")
	s.SetTimestamp(parseTs(t, "2020-05-06T07:08:09.010Z"))
	fa := &File{Kind: "string", Content: "content-A", Metadata: json.RawMessage(`{"z":9}`)}
	if err := s.AddFile("a.txt", fa); err != nil {
		t.Fatalf("AddFile: %v", err)
	}
	if err := s.AddFile("b.txt", &File{Kind: "hash", Hash: repeat40('c'), RangesHash: repeat40('d')}); err != nil {
		t.Fatalf("AddFile: %v", err)
	}
	got, err := s.Store(bs)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	assertP(t, got, []byte(`{"files":{"a.txt":{"hash":"hS0","metadata":{"z":9}},"b.txt":{"hash":"`+
		repeat40('c')+`","rangesHash":"`+repeat40('d')+`"}},"projectVersion":"1.2","timestamp":"2020-05-06T07:08:09.010Z"}`))
	if got := bs.strings["hS0"]; got != "content-A" {
		t.Fatalf("content blob = %q", got)
	}
}

func TestSnapshotStoreEmptyOmitsOptionalKeys(t *testing.T) {
	bs := newCountedBlobStore()
	got, err := SnapshotFromRaw(json.RawMessage(`{"files":{}}`)).Store(bs)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	assertP(t, got, []byte(`{"files":{}}`))
}

// --- Operation.Store (Node Operation.store) ---

func TestOperationStoreAddFile(t *testing.T) {
	bs := newCountedBlobStore()
	op, err := OperationFromRaw(json.RawMessage(`{"pathname":"new.txt","file":{"content":"n-c"}}`))
	if err != nil {
		t.Fatalf("OperationFromRaw: %v", err)
	}
	if op.Kind != "addFile" {
		t.Fatalf("kind = %q, want addFile", op.Kind)
	}
	got, err := op.Store(bs)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	assertP(t, got, []byte(`{"pathname":"new.txt","file":{"hash":"hS0"}}`))
	if got := bs.strings["hS0"]; got != "n-c" {
		t.Fatalf("blob = %q", got)
	}
	// noOp store -> {} (toRaw).
	gotNoOp, err := NoOp().Store(bs)
	if err != nil {
		t.Fatalf("Store noOp: %v", err)
	}
	assertP(t, gotNoOp, []byte(`{}`))
	// non-File ops (moveFile) store to their toRaw.
	mv, _ := OperationFromRaw(json.RawMessage(`{"pathname":"a.txt","newPathname":"b.txt"}`))
	gotMv, err := mv.Store(bs)
	if err != nil {
		t.Fatalf("Store moveFile: %v", err)
	}
	assertP(t, gotMv, mv.ToRaw())
}

// --- History / Chunk (Node oracle D_* / E_*) ---

func TestHistoryAndChunkStore(t *testing.T) {
	bs := newCountedBlobStore()

	ch1 := NewChange(
		[]*Operation{AddFile("h.txt", &File{Kind: "string", Content: "hc1"})},
		parseTs(t, "2020-01-01T00:00:00.000Z"),
		[]any{float64(1), float64(2), float64(1)},
		nil,
		[]any{},
		"",
		nil,
	)
	ch2 := NewChange(
		[]*Operation{},
		parseTs(t, "2020-01-02T00:00:00.000Z"),
		[]any{float64(3)},
		nil,
		[]any{},
		"2.0",
		nil,
	)
	h := NewHistory(SnapshotFromRaw(json.RawMessage(`{"files":{}}`)), []*Change{ch1, ch2})
	ck := NewChunk(h, 4)

	// toRaw (Node D_hist_toRaw / D_chunk_toRaw).
	assertP(t, h.ToRaw(), []byte(`{"snapshot":{"files":{}},"changes":[`+
		`{"operations":[{"pathname":"h.txt","file":{"content":"hc1"}}],"timestamp":"2020-01-01T00:00:00.000Z","authors":[1,2,1],"v2Authors":[]}`+
		`,`+
		`{"operations":[],"timestamp":"2020-01-02T00:00:00.000Z","authors":[3],"v2Authors":[],"projectVersion":"2.0"}`+
		`]}`))
	assertP(t, ck.ToRaw(), []byte(`{"history":`+string(h.ToRaw())+`,"startVersion":4}`))

	// Chunk getters.
	if ck.GetEndVersion() != 6 || ck.GetStartVersion() != 4 {
		t.Fatalf("end=%d start=%d, want 6/4", ck.GetEndVersion(), ck.GetStartVersion())
	}
	if got := ck.GetEndTimestamp().Format(rfcISO); got != "2020-01-02T00:00:00.000Z" {
		t.Fatalf("endTimestamp = %q, want 2020-01-02T00:00:00.000Z", got)
	}
	empty := NewChunk(NewHistory(SnapshotFromRaw(json.RawMessage(`{"files":{}}`)), nil), 0)
	if !empty.GetEndTimestamp().IsZero() {
		t.Fatalf("empty chunk endTimestamp must be zero (models Node null)")
	}

	// store (Node D_hist_store): content into blob, authors deduped.
	got, err := h.Store(bs)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	assertP(t, got, []byte(`{"snapshot":{"files":{}},"changes":[`+
		`{"operations":[{"pathname":"h.txt","file":{"hash":"hS0"}}],"timestamp":"2020-01-01T00:00:00.000Z","authors":[1,2],"v2Authors":[]}`+
		`,`+
		`{"operations":[],"timestamp":"2020-01-02T00:00:00.000Z","authors":[3],"v2Authors":[],"projectVersion":"2.0"}`+
		`]}`))
	if got := bs.strings["hS0"]; got != "hc1" {
		t.Fatalf("blob hS0 = %q", got)
	}
}

// --- Chunk error taxonomy (Node oracle E_*) ---

func TestChunkErrorMessages(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&ChunkNotFoundError{ProjectID: "p9"}, "no chunks for project p9"},
		{&ChunkVersionNotFoundError{ProjectID: "p9", Version: 5}, "chunk for p9 v 5 not found"},
		{&ChunkBeforeTimestampNotFoundError{ProjectID: "p9", Timestamp: "2010-01-01T00:00:00.000Z"}, "chunk for p9 timestamp 2010-01-01T00:00:00.000Z not found"},
		{&ChunkNotPersistedError{ProjectID: "p9"}, "chunk for p9 not persisted yet"},
		{&ConflictingEndVersion{ClientEndVersion: 3, LatestEndVersion: 7}, "client sent updates with end_version 3 but latest chunk has end_version 7"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Fatalf("err = %q, want %q", got, c.want)
		}
	}
}
