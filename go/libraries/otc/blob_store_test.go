package otc

import (
	"context"
	"testing"
)

func ctx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func TestBaseBlobStoreDefaults(t *testing.T) {
	c := ctx(t)
	bs := &BaseBlobStore{
		FetchString: func(context.Context, string) (string, error) { return "", nil },
	}
	// getBlob -> nil (Node BlobStoreBase default).
	if b, err := bs.GetBlob(c, "abc"); err != nil || b != nil {
		t.Fatalf("GetBlob = %v, %v", b, err)
	}
	// empty hash -> ''
	if s, err := bs.GetString(c, ""); err != nil || s != "" {
		t.Fatalf("GetString('') = %q, %v", s, err)
	}
	// non-empty hash -> fetch
	bs.FetchString = func(context.Context, string) (string, error) { return "hello", nil }
	if s, err := bs.GetString(c, "h"); err != nil || s != "hello" {
		t.Fatalf("GetString('h') = %q, %v", s, err)
	}
	// getObject: empty -> {}
	bs2 := &BaseBlobStore{FetchString: func(context.Context, string) (string, error) { return "", nil }}
	obj, err := bs2.GetObject(c, "h")
	if err != nil || len(obj) != 0 {
		t.Fatalf("GetObject(empty) = %v, %v", obj, err)
	}
	// getObject: JSON
	bs3 := &BaseBlobStore{FetchString: func(context.Context, string) (string, error) {
		return `{"a":1}`,
			nil
	}}
	obj3, err := bs3.GetObject(c, "h")
	if err != nil {
		t.Fatalf("GetObject(json): %v", err)
	}
	if obj3["a"] != any(1.0) {
		t.Fatalf("GetObject(json) = %v", obj3)
	}
}

func TestBaseBlobStoreDelegation(t *testing.T) {
	c := ctx(t)
	var put string
	var putObj map[string]any
	bs := &BaseBlobStore{
		PutStringFn: func(context.Context, string) (*Blob, error) {
			put = "s"
			return &Blob{Hash: "h1", ByteLength: 1, StringLength: int64Ptr(1)}, nil
		},
		PutObjectFn: func(context.Context, map[string]any) (*Blob, error) {
			putObj = map[string]any{"x": 1}
			return &Blob{Hash: "h2", ByteLength: 2, StringLength: int64Ptr(2)}, nil
		},
	}
	b1, err := bs.PutString(c, "s")
	if err != nil || b1.Hash != "h1" {
		t.Fatalf("PutString = %v, %v", b1, err)
	}
	if put == "" {
		t.Fatal("PutStringFn was not called")
	}
	b2, err := bs.PutObject(c, map[string]any{"x": 1})
	if err != nil || b2.Hash != "h2" {
		t.Fatalf("PutObject = %v, %v", b2, err)
	}
	if putObj == nil {
		t.Fatal("PutObjectFn was not called")
	}
}

// fakeBlobStore is a minimal in-memory BlobStore for the File-model tests.
type fakeBlobStore struct {
	stringMap map[string]string
	objectMap map[string]map[string]any
	nextHash  int
}

func newFakeBlobStore() *fakeBlobStore {
	return &fakeBlobStore{
		stringMap: map[string]string{},
		objectMap: map[string]map[string]any{},
		nextHash:  0,
	}
}

func (f *fakeBlobStore) GetBlob(context.Context, string) (*Blob, error) {
	return nil, nil
}
func (f *fakeBlobStore) GetString(c context.Context, hash string) (string, error) {
	s, ok := f.stringMap[hash]
	if !ok {
		return "", &BlobNotFoundError{Hash: hash}
	}
	return s, nil
}
func (f *fakeBlobStore) GetObject(c context.Context, hash string) (map[string]any, error) {
	o, ok := f.objectMap[hash]
	if !ok {
		return nil, &BlobNotFoundError{Hash: hash}
	}
	return o, nil
}
func (f *fakeBlobStore) PutString(c context.Context, content string) (*Blob, error) {
	f.nextHash++
	h := string(rune('a'+f.nextHash%26)) + "b"
	f.stringMap[h] = content
	return &Blob{Hash: h, ByteLength: int64(len(content)), StringLength: int64Ptr(len(content))}, nil
}
func (f *fakeBlobStore) PutObject(c context.Context, obj map[string]any) (*Blob, error) {
	f.nextHash++
	h := string(rune('a'+f.nextHash%26)) + "o"
	f.objectMap[h] = obj
	return &Blob{Hash: h, ByteLength: int64(len(obj)), StringLength: int64Ptr(len(obj))}, nil
}
