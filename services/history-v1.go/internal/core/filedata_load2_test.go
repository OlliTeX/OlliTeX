package core

import (
	"encoding/json"
	"testing"

	ch "history-v1/internal/contenthash"
	"history-v1/service/blobstore"
)

// Lazy load with ranges: getString + getObject → materialized comments.
func TestLazyLoadEagerWithRanges(t *testing.T) {
	bs := blobstore.NewFakeBlobStore()
	h, _ := bs.PutString("abc")
	// Canonical ranges JSON: {comments, trackedChanges}.
	rangesJSON := `{"comments":[{"id":"c9","ranges":[{"pos":0,"length":1}],"resolved":false}],"trackedChanges":[{"range":{"pos":2,"length":1},"tracking":{"type":"insert","userId":"u1"}}]}`
	rh, err := bs.PutObject([]byte(rangesJSON))
	if err != nil {
		t.Fatal(err)
	}
	lazy, err := FileFromRaw(json.RawMessage(`{"hash":"` + h + `","stringLength":3,"rangesHash":"` + rh + `","operations":[{"textOperation":[3]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	eager, err := lazy.Load("eager", bs)
	if err != nil {
		t.Fatalf("Load[eager]: %v", err)
	}
	if eager.Kind != "string" || eager.Content != "abc" {
		t.Fatalf("eager = %+v", eager)
	}
	if got := len(eager.Comments); got != 1 {
		t.Fatalf("comments = %d, want 1", got)
	}
	if got := len(eager.TrackedChanges); got != 1 {
		t.Fatalf("trackedChanges = %d, want 1", got)
	}
}

// Load "hollow" on string kind: wire is {stringLength}.
func TestLoadHollowFromEager(t *testing.T) {
	utf16 := len("héllo ") // 6 runes, 6 units (h é l l o + space; é is 1 unit)
	_ = utf16
	f := &File{Kind: "string", Content: "héllo"}
	out, err := f.Load("hollow", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "hollowStr" {
		t.Fatalf("kind = %q, want hollowStr", out.Kind)
	}
	if got := out.GetStringLength(); got != 5 {
		t.Fatalf("stringLength = %d, want 5", got)
	}
	raw := out.ToRaw()
	if got := string(raw); got != `{"stringLength":5}` {
		t.Fatalf("wire = %s, want {\"stringLength\":5}", got)
	}
}

// Load "lazy" on hash kind: fetch blob, produce lazy {hash, stringLength}.
func TestLoadLazyFromHash(t *testing.T) {
	bs := blobstore.NewFakeBlobStore()
	h, _ := bs.PutString("hello")
	f, _ := FileFromRaw(json.RawMessage(`{"hash":"` + h + `","rangesHash":""}`))
	// Hash kind: FileFromRaw maps {hash} -> "hash".
	if f.Kind != "hash" {
		t.Fatalf("kind = %q, want hash", f.Kind)
	}
	out, err := f.Load("lazy", bs)
	if err != nil {
		t.Fatalf("Load[lazy]: %v", err)
	}
	if out.Kind != "lazy" {
		t.Fatalf("kind = %q, want lazy", out.Kind)
	}
	if got := out.GetStringLength(); got != 5 {
		t.Fatalf("stringLength = %d, want 5", got)
	}
	if got := out.GetHash(); got != h {
		t.Fatalf("hash = %q, want %q", got, h)
	}
	// Wire: {hash, stringLength} — no rangesHash (nil/empty hash blob).
	raw := out.ToRaw()
	var probe map[string]interface{}
	_ = json.Unmarshal(raw, &probe)
	if _, ok := probe["rangesHash"]; ok {
		t.Fatalf("wire has rangesHash: %s", raw)
	}
}

// Load "eager" on hash kind: toLazy -> toEager; content materialized.
func TestLoadEagerFromHash(t *testing.T) {
	bs := blobstore.NewFakeBlobStore()
	h, _ := bs.PutString("hello")
	f, _ := FileFromRaw(json.RawMessage(`{"hash":"` + h + `"}`))
	out, err := f.Load("eager", bs)
	if err != nil {
		t.Fatalf("Load[eager]: %v", err)
	}
	if out.Kind != "string" || out.Content != "hello" {
		t.Fatalf("eager = %+v", out)
	}
	// Node StringFileData.getHash() = null → Go "".
	if got := out.GetHash(); got != "" {
		t.Fatalf("GetHash = %q, want \"\"", got)
	}
}

// Store "string" kind (no comments/tracked): {hash} == blob hash of content.
func TestStoreStringPlain(t *testing.T) {
	bs := blobstore.NewFakeBlobStore()
	f := &File{Kind: "string", Content: "abc"}
	stored, err := f.Store(bs)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(stored); got != `{"hash":"`+ch.BlobHash("abc")+`"}` {
		t.Fatalf("wire = %s", got)
	}
}

// Store "hash" kind: {hash[, rangesHash]}, no blob write.
func TestStoreHashNoWrite(t *testing.T) {
	bs := blobstore.NewFakeBlobStore()
	f := &File{Kind: "hash", Hash: "a1"}
	stored, err := f.Store(bs)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(stored); got != `{"hash":"a1"}` {
		t.Fatalf("wire = %s", got)
	}
	f2 := &File{Kind: "hash", Hash: "b2", RangesHash: "c3"}
	stored2, _ := f2.Store(bs)
	if got := string(stored2); got != `{"hash":"b2","rangesHash":"c3"}` {
		t.Fatalf("wire = %s", got)
	}
}

// Store "binary" kind: {hash} no write.
func TestStoreBinaryPlain(t *testing.T) {
	bs := blobstore.NewFakeBlobStore()
	f := &File{Kind: "binary", Hash: "dd", ByteLength: 3}
	stored, err := f.Store(bs)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(stored); got != `{"hash":"dd"}` {
		t.Fatalf("wire = %s", got)
	}
}

// Load "eager" on binary kind: returns self (Node identity).
func TestLoadEagerBinarySelf(t *testing.T) {
	bs := blobstore.NewFakeBlobStore()
	f := &File{Kind: "binary", Hash: "dd", ByteLength: 3}
	out, err := f.Load("eager", bs)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "binary" {
		t.Fatalf("kind = %q, want binary", out.Kind)
	}
}

// Load "eager" on hollow kind: NotEditableError (toEager not implemented).
func TestLoadEagerHollowErr(t *testing.T) {
	f := &File{Kind: "hollowStr", StringLength: 5}
	if _, err := f.Load("eager", nil); err == nil {
		t.Fatal("Load[eager] on hollowStr: wanted error, got nil")
	}
}
