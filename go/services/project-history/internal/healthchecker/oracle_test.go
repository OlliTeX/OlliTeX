// C15 oracle — HealthChecker H1–H4.
package healthchecker

import (
	"context"
	"errors"
	"testing"
	"time"
)

var cctx = context.Background()

func TestC15_FullPass(t *testing.T) {
	urls := []string{}
	d := &Deps{Port: "3054", ProjectID: "0123456789abcdef01234567", FetchNothing: func(ctx context.Context, url string, timeout time.Duration) error {
		urls = append(urls, url)
		_ = timeout
		return nil
	}}
	if err := d.Check(cctx); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"http://127.0.0.1:3054/check_lock",
		"http://127.0.0.1:3054/project/0123456789abcdef01234567/flush",
		"http://127.0.0.1:3054/project/0123456789abcdef01234567/updates",
	}
	if len(urls) != 3 || urls[0] != want[0] || urls[1] != want[1] || urls[2] != want[2] {
		t.Fatalf("urls: %v", urls)
	}
}

func TestC15_CheckLockErrorTag(t *testing.T) {
	boom := errors.New("socket down")
	d := &Deps{Port: "3054", ProjectID: "pid", FetchNothing: func(ctx context.Context, url string, timeout time.Duration) error { return boom }}
	err := d.Check(cctx)
	tg, ok := err.(*tagged)
	if !ok || tg.msg != "error checking lock for health check" {
		t.Fatalf("tag: %v", err)
	}
	if tg.Info()["project_id"] != "pid" || tg.Unwrap() != boom {
		t.Fatalf("info/cause: %#v", tg)
	}
}

func TestC15_FlushErrorTag(t *testing.T) {
	d := &Deps{Port: "3054", ProjectID: "pid", FetchNothing: func(ctx context.Context, url string, timeout time.Duration) error {
		if url == "http://127.0.0.1:3054/check_lock" {
			return nil
		}
		return errors.New("flush 500")
	}}
	err := d.Check(cctx)
	tg, _ := err.(*tagged)
	if tg == nil || tg.msg != "error flushing for health check" {
		t.Fatalf("tag: %v", err)
	}
}

func TestC15_UpdatesErrorTag(t *testing.T) {
	d := &Deps{Port: "3054", ProjectID: "pid", FetchNothing: func(ctx context.Context, url string, timeout time.Duration) error {
		if url == "http://127.0.0.1:3054/project/pid/updates" {
			return errors.New("updates 500")
		}
		return nil
	}}
	err := d.Check(cctx)
	tg, _ := err.(*tagged)
	if tg == nil || tg.msg != "error getting updates for health check" {
		t.Fatalf("tag: %v", err)
	}
}

func TestC15_CheckLockSeam(t *testing.T) {
	d := &Deps{CheckLockFn: func(ctx context.Context) (bool, error) { return true, nil }}
	ok, err := d.CheckLock(cctx)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	d2 := &Deps{}
	if _, err := d2.CheckLock(cctx); err == nil {
		t.Fatal("want seam error")
	}
}
