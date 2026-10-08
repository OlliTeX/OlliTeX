package toolkit

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"ollitex/go/libraries/configstore"
)

// TestRepro_AttachFrame builds the real TUI app on a net.Pipe session screen
// (the same code path as an SSH attach: NewTUI(a, screen) → Init → Run) and
// checks that the first frame actually reaches the client at every size —
// the TK H3 owner report: every size except 80x24 died at boot (tcell v2.8.1
// double-Init cell-buffer shrink → drawCell spin).
func TestRepro_AttachFrame(t *testing.T) {
	cols, rows := 81, 24
	if v := os.Getenv("TK_REPRO_SIZE"); v != "" {
		fmt.Sscanf(v, "%dx%d", &cols, &rows)
	}
	start := os.Getenv("TK_REPRO_SCREEN")
	t.Setenv(configstore.SQLiteEnv, filepath.Join(t.TempDir(), "store.db"))
	os.Unsetenv("CONFIG_DB_DSN")
	os.Unsetenv("DATABASE_URL")
	tk, err := New(Options{DataDir: t.TempDir(), Project: "ollitex"})
	if err != nil {
		t.Fatalf("toolkit new: %v", err)
	}
	a := newApp(tk)
	if start != "" {
		a.gotoScreen(start)
	}

	cr, cs := net.Pipe()
	// client reader MUST drain before any server write (SetScreen inits and
	// writes the engage sequence) — like ssh relaying to the pty.
	var got atomic.Int64
	go func() {
		buf := make([]byte, 65536)
		for {
			r, e := cs.Read(buf)
			if r > 0 {
				got.Add(int64(r))
			}
			if e != nil {
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	screen, _, err := SessionScreen(cr, cols, rows)
	if err != nil {
		t.Fatalf("session screen: %v", err)
	}
	tv := NewTUI(a, screen)
	// server.go also Init's explicitly — with the onceInitScreen wrapper the
	// second Init is a no-op either way; mirror the live flow.
	_ = screen.Init()

	done := make(chan error, 1)
	go func() { done <- tv.Run() }()

	// Run must NOT return on its own (that only happens on quit)...
	deadline := time.After(6 * time.Second)
	early := false
	select {
	case e := <-done:
		t.Fatalf("Run returned early: %v (screen %dx%d)", e, cols, rows)
	case <-deadline:
		early = true
	}
	_ = early

	bytes := got.Load()
	// small terminals simply produce smaller first frames
	minBytes := int64(5000)
	if mb := int64(cols) * int64(rows) * 3 + 1000; mb < minBytes {
		minBytes = mb
	}
	if bytes < minBytes {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("FRAME NOT DELIVERED at %dx%d (client got %d bytes) — stacks:\n%s", cols, rows, bytes, string(buf[:n]))
	}
	t.Logf("FRAME OK at %dx%d: client received %d bytes", cols, rows, bytes)

	// clean quit via the verified T10 path (app.Stop's tcell Fini deadlocks
	// on the blocked tty read — tearDown writes teardown bytes + closes).
	tv.SetRIO(cr)
	tv.tearDown()
	select {
	case e := <-done:
		t.Logf("clean stop: %v", e)
	case <-time.After(8 * time.Second):
		t.Fatalf("STOP HUNG at %dx%d", cols, rows)
	}
}
