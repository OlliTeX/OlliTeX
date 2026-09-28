// C3 oracle — BlobManager vendor contracts: passthrough, retry, first-error
// semantics, lock extension on every attempt + success, concurrent cap.
package blobmanager

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

var cctx = context.Background()

func TestC3_PassthroughNonAddUpdates(t *testing.T) {
	created := 0
	d := &Deps{
		CreateBlob: func(ctx context.Context, p, h string, u map[string]any) (map[string]any, error) {
			created++
			return map[string]any{"file": "h1"}, nil
		},
	}
	ups := []map[string]any{
		{"pathname": "x.tex", "content": "a"},       // not an add
		{"doc": "d", "docLines": "x", "file": true}, // IS an add
		{"comment": "c"}, // not an add
	}
	out, err := CreateBlobsForUpdates(cctx, d, "p", "h", ups)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("len: %d", len(out))
	}
	// vendor element wrapper: {update: <update>, blobHashes?: ...}
	e0 := out[0]["update"].(map[string]any)
	if e0["pathname"] != "x.tex" || e0["content"] != "a" {
		t.Fatalf("passthrough update: %#v", out[0])
	}
	if _, has := out[0]["blobHashes"]; has {
		t.Fatal("non-add must not carry blobHashes")
	}
	bh := out[1]["blobHashes"].(map[string]any)
	if bh["file"] != "h1" {
		t.Fatalf("add element: %#v", out[1])
	}
	if out[1]["update"].(map[string]any)["doc"] != "d" {
		t.Fatalf("add update wrapper: %#v", out[1])
	}
	if out[2]["update"].(map[string]any)["comment"] != "c" {
		t.Fatalf("element 3: %#v", out[2])
	}
	if created != 1 {
		t.Fatalf("createblob calls: %d (only add updates)", created)
	}
}

func TestC3_RetryThenSuccess(t *testing.T) {
	calls := 0
	slews := 0
	d := &Deps{
		Slew: func(time.Duration) { slews++ },
		CreateBlob: func(ctx context.Context, p, h string, u map[string]any) (map[string]any, error) {
			calls++
			if calls < 3 {
				return nil, context.DeadlineExceeded
			}
			return map[string]any{"file": "ok"}, nil
		},
	}
	out, err := CreateBlobsForUpdates(cctx, d, "p", "h", []map[string]any{{"file": true, "url": "http://x/project/abcdefabcdefabcdef/file/abcdefabcdefabcdef"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("attempts: %d (vendor: fails twice then succeeds)", calls)
	}
	if slews != 2 {
		t.Fatalf("slews: %d (delay only BETWEEN attempts)", slews)
	}
	bh := out[0]["blobHashes"].(map[string]any)
	if bh["file"] != "ok" {
		t.Fatalf("blobHashes: %#v", out[0])
	}
}

func TestC3_ExhaustedRetriesFirstError(t *testing.T) {
	calls := 0
	d := &Deps{
		Slew: func(time.Duration) {},
		CreateBlob: func(ctx context.Context, p, h string, u map[string]any) (map[string]any, error) {
			calls++
			return nil, context.Canceled
		},
	}
	_, err := CreateBlobsForUpdates(cctx, d, "p", "h", []map[string]any{{"file": true, "url": "http://x/project/abcdefabcdefabcdef/file/abcdefabcdefabcdef"}})
	if err == nil {
		t.Fatal("want first-error")
	}
	if calls != 3 {
		t.Fatalf("attempts: %d (want 3)", calls)
	}
	eb, ok := err.(*errBlob)
	if !ok {
		t.Fatalf("want errBlob: %T", err)
	}
	if eb.Error() != "retry: error creating blob" {
		t.Fatalf("msg: %q", eb.Error())
	}
	if eb.Info()["projectId"] != "p" {
		t.Fatalf("info: %#v", eb.Info())
	}
}

func TestC3_ExtendLockEachAttemptAndSuccess(t *testing.T) {
	var ext int32
	d := &Deps{
		Slew: func(time.Duration) {},
		ExtendLock: func(ctx context.Context) error {
			atomic.AddInt32(&ext, 1)
			return nil
		},
		CreateBlob: func(ctx context.Context, p, h string, u map[string]any) (map[string]any, error) {
			return map[string]any{"file": "ok"}, nil
		},
	}
	if _, err := CreateBlobsForUpdates(cctx, d, "p", "h", []map[string]any{{"doc": "d", "docLines": "x"}}); err != nil {
		t.Fatal(err)
	}
	// success: lock extended once per attempt (1) + once on success path = 2
	if got := atomic.LoadInt32(&ext); got != 2 {
		t.Fatalf("extendLock calls: %d (want 2)", got)
	}
}

func TestC3_ExtendLockFailureAborts(t *testing.T) {
	calls := 0
	d := &Deps{
		Slew: func(time.Duration) {},
		ExtendLock: func(ctx context.Context) error {
			return context.DeadlineExceeded
		},
		CreateBlob: func(ctx context.Context, p, h string, u map[string]any) (map[string]any, error) {
			calls++
			return map[string]any{"file": "ok"}, nil
		},
	}
	_, err := CreateBlobsForUpdates(cctx, d, "p", "h", []map[string]any{{"doc": "d", "docLines": "x"}})
	if err == nil || err.Error() != "context deadline exceeded" {
		t.Fatalf("err: %v", err)
	}
	if calls != 0 {
		t.Fatalf("createBlob must not run after extendLock failure: %d", calls)
	}
}

func TestC3_FirstErrorCollectedAcrossUpdates(t *testing.T) {
	d := &Deps{
		Slew: func(time.Duration) {},
		CreateBlob: func(ctx context.Context, p, h string, u map[string]any) (map[string]any, error) {
			if u["ok"] == true {
				return map[string]any{"file": "ok"}, nil
			}
			return nil, context.Canceled
		},
	}
	ups := []map[string]any{
		{"doc": "a", "docLines": "x", "ok": true},
		{"doc": "b", "docLines": "y"},
	}
	_, err := CreateBlobsForUpdates(cctx, d, "p", "h", ups)
	if err == nil {
		t.Fatal("want the FIRST blob creation error surfaced")
	}
	if err.Error() != "retry: error creating blob" {
		t.Fatalf("msg: %q", err.Error())
	}
}
