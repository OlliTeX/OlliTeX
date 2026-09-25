package core

import (
	"encoding/json"
	"testing"

	ch "history-v1/internal/contenthash"
	"history-v1/service/blobstore"
)

func TestLazyLoadEagerAppliesBufferedOps(t *testing.T) {
	bs := blobstore.NewFakeBlobStore()
	h0, err := bs.PutString("abc")
	if err != nil {
		t.Fatal(err)
	}

	// Build a lazy file: {hash, stringLength:3}, then buffer an edit op.
	lazy, err := FileFromRaw(json.RawMessage(`{"hash":"` + h0 + `","stringLength":3}`))
	if err != nil {
		t.Fatalf("FileFromRaw(lazy): %v", err)
	}
	op, err := EditOpFromRaw(json.RawMessage(`{"textOperation":[1,{"i":"x"},2]}`))
	if err != nil {
		t.Fatalf("EditOpFromRaw: %v", err)
	}
	if err := lazy.Edit(op.TextOpForEdit()); err != nil {
		t.Fatalf("lazy Edit: %v", err)
	}

	// Load eager: content = "axbc".
	eager, err := lazy.Load("eager", bs)
	if err != nil {
		t.Fatalf("Load[eager]: %v", err)
	}
	if eager.Kind != "string" {
		t.Fatalf("kind = %q, want string", eager.Kind)
	}
	if got := eager.Content; got != "axbc" {
		t.Fatalf("content = %q, want \"axbc\"", got)
	}
	if got := eager.GetStringLength(); got != 4 {
		t.Fatalf("stringLength = %d, want 4", got)
	}
	if got := eager.GetByteLength(); got != 4 {
		t.Fatalf("byteLength = %d, want 4", got)
	}
}

// Lazy + buffered ops, store -> new hash {hash}, content "azbc" persisted.
func TestLazyWithOpsStoreRoundTrip(t *testing.T) {
	bs := blobstore.NewFakeBlobStore()
	h0, _ := bs.PutString("abc")

	lazy, _ := FileFromRaw(json.RawMessage(`{"hash":"` + h0 + `","stringLength":3}`))
	op, _ := EditOpFromRaw(json.RawMessage(`{"textOperation":[{"i":"z"},3]}`))
	if err := lazy.Edit(op.TextOpForEdit()); err != nil {
		t.Fatal(err)
	}
	if len(lazy.LazyOps) != 1 {
		t.Fatalf("lazy ops = %d, want 1", len(lazy.LazyOps))
	}
	if got := lazy.GetStringLength(); got != 4 {
		t.Fatalf("lazy stringLength = %d, want 4", got)
	}

	stored, err := lazy.Store(bs)
	if err != nil {
		t.Fatalf("lazy Store: %v", err)
	}
	var probe struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(stored, &probe); err != nil {
		t.Fatalf("stored not object: %s", stored)
	}
	if got := probe.Hash; got != ch.BlobHash("zabc") {
		t.Fatalf("stored hash = %q, want blob(\"zabc\")", got)
	}
	got, err := bs.GetString(probe.Hash)
	if err != nil || got != "zabc" {
		t.Fatalf("blob not persisted: %q, %v", got, err)
	}
}

// GetContent(filterTrackedDeletes) drops tracked-deleted ranges (Node).
func TestGetContentFilterTrackedDeletes(t *testing.T) {
	f := &File{
		Kind:    "string",
		Content: "abcdef",
		TrackedChanges: TrackedChangeList{
			{
				Range:    Range{Pos: 1, Length: 2},
				Tracking: &TrackingProps{Type: "delete", UserID: "u1"},
			},
		},
	}
	got, err := f.GetContent(true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "adef" {
		t.Fatalf("GetContent(filter) = %q, want \"adef\"", got)
	}
	plain, _ := f.GetContent(false)
	if plain != "abcdef" {
		t.Fatalf("GetContent(nofilter) = %q, want \"abcdef\"", plain)
	}
}

// GetContent on non-string kind returns "" (Node base returns null).
func TestGetContentNonStringNil(t *testing.T) {
	f := &File{Kind: "lazy", Hash: "1", StringLength: 5}
	got, _ := f.GetContent(true)
	if got != "" {
		t.Fatalf("GetContent(lazy) = %q, want \"\"", got)
	}
}
