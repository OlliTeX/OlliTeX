// C13 oracle — FlushManager F1–F3 + LocalFileWriter F4–F6.
package flushmanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var cctx = context.Background()

func TestC13_FlushIfOldBranches(t *testing.T) {
	processed := 0
	d := &Deps{
		GetFirstOpTimestamp: func(ctx context.Context, p string) (int64, error) { return 1000, nil },
		ProcessUpdates:      func(ctx context.Context, p string) error { processed++; return nil },
	}
	// ts (1000) < cutoff (5000) → flush
	if err := d.FlushIfOld(cctx, "p", 5000); err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed: %d", processed)
	}
	// ts >= cutoff → skipped (no process)
	processed = 0
	if err := d.FlushIfOld(cctx, "p", 500); err != nil {
		t.Fatal(err)
	}
	if processed != 0 {
		t.Fatal("should skip when ts >= cutoff")
	}
	// short-queue branch
	d.ShortHistoryQueues = []string{"p"}
	if err := d.FlushIfOld(cctx, "p", 500); err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatal("short queue must process")
	}
	// missing timestamp (0) → flush anyway (vendor: safety)
	processed = 0
	d2 := &Deps{
		GetFirstOpTimestamp: func(ctx context.Context, p string) (int64, error) { return 0, nil },
		ProcessUpdates:      func(ctx context.Context, p string) error { processed++; return nil },
	}
	if err := d2.FlushIfOld(cctx, "p", 500); err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatal("absent timestamp → flush for safety")
	}
	// process error propagates
	d3 := &Deps{
		GetFirstOpTimestamp: func(ctx context.Context, p string) (int64, error) { return 0, nil },
		ProcessUpdates:      func(ctx context.Context, p string) error { return errors.New("busy") },
	}
	if err := d3.FlushIfOld(cctx, "p", 500); err == nil {
		t.Fatal("want process error")
	}
	// redis error propagates
	d4 := &Deps{GetFirstOpTimestamp: func(ctx context.Context, p string) (int64, error) { return 0, errors.New("redis down") }}
	if err := d4.FlushIfOld(cctx, "p", 500); err == nil {
		t.Fatal("want redis error")
	}
}

func TestC13_FlushOldOpsFullFlow(t *testing.T) {
	var order []string
	d := &Deps{
		GetProjectIdsWithHistoryOps: func(ctx context.Context) ([]string, error) {
			return []string{"a", "b", "c", "failed1"}, nil
		},
		GetFailedProjects: func(ctx context.Context) ([]map[string]any, error) {
			return []map[string]any{{"project_id": "failed1"}}, nil
		},
		GetFirstOpTimestamp: func(ctx context.Context, p string) (int64, error) { return 0, nil },
		ProcessUpdates: func(ctx context.Context, p string) error {
			order = append(order, p)
			if p == "b" {
				return errors.New("flush boom")
			}
			return nil
		},
		ShortHistoryQueues: nil,
		Slew:               func(d2 time.Duration) {},
	}
	res, err := d.FlushOldOps(cctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Success) != 3 || len(res.Failure) != 1 {
		t.Fatalf("success=%v failure=%v", res.Success, res.Failure)
	}
	if res.Failure[0] != "b" {
		t.Fatalf("failure: %v", res.Failure)
	}
	if len(res.FailedProjects) != 1 || res.FailedProjects[0] != "failed1" {
		t.Fatalf("failedProjects: %v", res.FailedProjects)
	}
	// 'b' failed → not in order of processed ones? vendor: flushIfOld IS called for it
	processed := map[string]bool{}
	for _, p := range order {
		processed[p] = true
	}
	if !processed["b"] {
		t.Fatal("b must still be attempted")
	}
	if processed["failed1"] {
		t.Fatal("failed1 must be SKIPPED (vendor)")
	}
}

func TestC13_FlushOldOpsTimeoutBail(t *testing.T) {
	now := time.Now()
	ms := int64(0)
	d := &Deps{
		GetProjectIdsWithHistoryOps: func(ctx context.Context) ([]string, error) {
			return []string{"p1", "p2", "p3"}, nil
		},
		GetFailedProjects: func(ctx context.Context) ([]map[string]any, error) { return nil, nil },
		GetFirstOpTimestamp: func(ctx context.Context, p string) (int64, error) {
			ms += 100
			return 0, nil
		},
		ProcessUpdates: func(ctx context.Context, p string) error { return nil },
		Slew:           func(d2 time.Duration) {},
		Now:            func() time.Time { return now.Add(time.Duration(ms) * time.Millisecond) },
	}
	// timeout 150ms → p1 (0) ok, p2 (100→200 > 150 at check) bails
	res, err := d.FlushOldOps(cctx, Options{Timeout: 150})
	if err == nil || err.Error() != "retries timed out" {
		t.Fatalf("bail err: %v", err)
	}
	// the bailing job counts as success (F3)
	foundBail := false
	for _, p := range res.Success {
		if p == "p2" {
			foundBail = true
		}
	}
	if !foundBail {
		t.Fatalf("bail job must be in success: %v", res.Success)
	}
}

func TestC13_FlushOldOpsLimitBail(t *testing.T) {
	d := &Deps{
		GetProjectIdsWithHistoryOps: func(ctx context.Context) ([]string, error) {
			return []string{"p1", "p2", "p3"}, nil
		},
		GetFailedProjects:   func(ctx context.Context) ([]map[string]any, error) { return nil, nil },
		GetFirstOpTimestamp: func(ctx context.Context, p string) (int64, error) { return 0, nil },
		ProcessUpdates:      func(ctx context.Context, p string) error { return nil },
		Slew:                func(d2 time.Duration) {},
	}
	res, err := d.FlushOldOps(cctx, Options{Limit: 2})
	if err == nil || err.Error() != "hit limit" {
		t.Fatalf("bail err: %v", err)
	}
	// vendor: the BAILING job (count 3 > limit 2) is still counted → 3 in success
	if len(res.Success) != 3 {
		t.Fatalf("success: %v", res.Success)
	}
}

func TestC13_StoreErrors(t *testing.T) {
	boom := errors.New("down")
	d := &Deps{GetProjectIdsWithHistoryOps: func(ctx context.Context) ([]string, error) { return nil, boom }}
	if _, err := d.FlushOldOps(cctx, Options{}); err != boom {
		t.Fatal("want ids error")
	}
	d2 := &Deps{
		GetProjectIdsWithHistoryOps: func(ctx context.Context) ([]string, error) { return []string{"a"}, nil },
		GetFailedProjects:           func(ctx context.Context) ([]map[string]any, error) { return nil, boom },
	}
	if _, err := d2.FlushOldOps(cctx, Options{}); err != boom {
		t.Fatal("want failures error")
	}
}

func TestC13_DeleteFileFaithful(t *testing.T) {
	// F4: empty → no-op
	if err := deleteFileFaithful(""); err != nil {
		t.Fatal("empty path")
	}
	// F4: missing → ENOENT ignored
	if err := deleteFileFaithful("/nonexistent/definitely/missing-" + time.Now().Format("20060102150405.000")); err != nil {
		t.Fatalf("ENOENT must be ignored: %v", err)
	}
	// F4: existing → removed
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	if err := os.WriteFile(p, []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := deleteFileFaithful(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file must be gone")
	}
	_ = os.IsExist
}

func TestC13_BufferOnDiskHappyAndConsumers(t *testing.T) {
	dir := t.TempDir()
	var gotPath string
	d := &Deps{
		UploadFolder: dir,
		WriteLocal: func(p string, data []byte) (int64, error) {
			if err := os.WriteFile(p, data, 0644); err != nil {
				return 0, err
			}
			return int64(len(data)), nil
		},
		ReplaceWithStubIfNeeded: func(fsPath, fileId string, fileSize int64) (string, error) {
			if fileSize != int64(len("payload")) {
				t.Fatalf("fileSize: %d", fileSize)
			}
			return fsPath + "-lfg", nil
		},
	}
	err := d.BufferOnDisk(cctx, []byte("payload"), "http://u", "fid", func(newFsPath string, cleanup Cleaner) error {
		gotPath = newFsPath
		if cleanup == nil {
			t.Error("cleanup must be handed to the consumer")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath == "" {
		t.Fatal("consumer path")
	}
}

func TestC13_BufferOnDiskWriteError(t *testing.T) {
	boom := errors.New("disk full")
	d := &Deps{
		UploadFolder: t.TempDir(),
		WriteLocal:   func(p string, data []byte) (int64, error) { return 0, boom },
	}
	err := d.BufferOnDisk(cctx, []byte("x"), "u", "f", func(string, Cleaner) error { t.Fatal("consumer must not run"); return nil })
	el, ok := err.(*ErrLocal)
	if !ok || el.Msg != "problem writing file locally" {
		t.Fatalf("write error shape: %v", err)
	}
	if el.Info["fsPath"] == "" || el.Info["url"] != "u" {
		t.Fatalf("info: %#v", el.Info)
	}
	if el.Unwrap() != boom {
		t.Fatal("cause")
	}
}

func TestC13_BufferOnDiskStubErrorAndCleanupBoth(t *testing.T) {
	boom := errors.New("lfg down")
	var logged []error
	d := &Deps{
		UploadFolder: t.TempDir(),
		WriteLocal: func(p string, data []byte) (int64, error) {
			if err := os.WriteFile(p, data, 0644); err != nil {
				return 0, err
			}
			return int64(len(data)), nil
		},
		ReplaceWithStubIfNeeded: func(fsPath, fileId string, fileSize int64) (string, error) { return "", boom },
		Log:                     func(e error) { logged = append(logged, e) },
	}
	err := d.BufferOnDisk(cctx, []byte("x"), "u", "f", func(string, Cleaner) error { t.Fatal("n/a"); return nil })
	el, ok := err.(*ErrLocal)
	if !ok || el.Msg != "problem in large file manager" {
		t.Fatalf("stub error shape: %v", err)
	}
	// F6: cleanup removed the temp file via the faithful deleteFile
	// (both-error log path needs a failing cleanup — force one below)
	d2 := &Deps{
		UploadFolder: t.TempDir(),
		WriteLocal:   func(p string, data []byte) (int64, error) { return 0, errors.New("w") },
		DeleteFile:   func(p string) error { return errors.New("cleanup boom") },
		Log:          func(e error) { logged = append(logged, e) },
	}
	if err := d2.BufferOnDisk(cctx, []byte("x"), "u", "f", func(string, Cleaner) error { return nil }); err == nil {
		t.Fatal("want write error")
	}
	found := false
	for _, e := range logged {
		if e.Error() == "cleanup boom" {
			found = true
		}
	}
	if !found {
		t.Fatal("F6: both-error path must log the cleanup error")
	}
}
