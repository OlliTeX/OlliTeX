package swap

import "testing"

// ---- ports InMemorySwapStoreTest ----

func TestDownloadingNonExistentFileThrows(t *testing.T) {
	store := NewInMemorySwapStore()
	_, err := store.Download("x")
	if err == nil {
		t.Fatalf("Download of nonexistent blob must error")
	}
}

func TestCanDownloadUploadedFiles(t *testing.T) {
	store := NewInMemorySwapStore()
	data := []byte("hello world")
	if err := store.Upload("x", data); err != nil {
		t.Fatalf("upload: %v", err)
	}
	got, err := store.Download("x")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(got) != len(data) {
		t.Errorf("len = %d, want %d", len(got), len(data))
	}
}

func TestUploadOverwrites(t *testing.T) {
	store := NewInMemorySwapStore()
	if err := store.Upload("x", []byte("one")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := store.Upload("x", []byte("two")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	got, err := store.Download("x")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(got) != "two" {
		t.Errorf("blob = %q, want two", got)
	}
}

func TestRemoveDeletesFiles(t *testing.T) {
	store := NewInMemorySwapStore()
	if err := store.Upload("x", []byte("delete me")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := store.Remove("x"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := store.Download("x"); err == nil {
		t.Errorf("download after remove must error")
	}
}

func TestNoopStoreIsNotSafe(t *testing.T) {
	noop := NoopSwapStore{}
	if noop.IsSafe() {
		t.Errorf("NoopSwapStore.IsSafe must be false")
	}
}

func TestInMemoryStoreIsSafe(t *testing.T) {
	if !NewInMemorySwapStore().IsSafe() {
		t.Errorf("InMemorySwapStore.IsSafe must be true")
	}
}
