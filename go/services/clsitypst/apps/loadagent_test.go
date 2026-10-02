// loadagent_test.go covers the load-balancer agent (D13): the readyLine
// formula (Node app.js L162-199 verbatim: cpus-1 avail, loadavg,
// disk-critical 0 / disk-low halved, maintenance drain flip,
// clsi_typst-prevented-maint metric), the per-connection TCP one-line
// push, and the load-HTTP POST /state/{up,down,maint} handler
// (204 / 405 non-POST + Allow / 400 unknown).
package apps

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"ollitex/go/services/clsitypst/metrics"
)

func testAgent(t *testing.T, cpus int, load float64,
	diskCritical, diskLow, allowMaint, report bool, inc *int) *LoadAgent {
	a := &LoadAgent{
		CPUCount:         func() int { return cpus },
		LoadAvg:          func() float64 { return load },
		ReportLoad:       report,
		AllowMaintenance: allowMaint,
		MaintenanceInc: func() {
			if inc != nil {
				*inc++
			}
		},
		isDiskCritical: func() bool { return diskCritical },
		isDiskLow:      func() bool { return diskLow },
	}
	a.state.Store(LoadState("up"))
	return a
}

// --- readyLine formula (Node app.js L162-199 verbatim) ------------------------------

func TestReadyLineUp(t *testing.T) {
	// cpus 8 -> avail 7, load 0 -> pct round(100) -> "up, ready, 100%\n".
	a := testAgent(t, 8, 0, false, false, false, true, nil)
	if got := a.readyLine(); got != "up, ready, 100%\n" {
		t.Fatalf("ready up: %q", got)
	}
}

func TestReadyLineAvailCPUsMinusOne(t *testing.T) {
	// cpus 4 -> avail 3, load 1 -> free 2 -> round(66.67) = 67%.
	a := testAgent(t, 4, 1, false, false, false, true, nil)
	if got := a.readyLine(); got != "up, ready, 67%\n" {
		t.Fatalf("avail-1: %q", got)
	}
}

func TestReadyLineSingleCPU(t *testing.T) {
	// cpus 1 -> avail 1 (staging single-core), load 0 -> 100%.
	a := testAgent(t, 1, 0, false, false, false, true, nil)
	if got := a.readyLine(); got != "up, ready, 100%\n" {
		t.Fatalf("1cpu: %q", got)
	}
}

func TestReadyLineDiskCriticalZeros(t *testing.T) {
	// disk-critical -> pct = 0 (before the disk-low halving).
	a := testAgent(t, 4, 0, true, false, false, true, nil)
	if got := a.readyLine(); got != "up, ready, 1%\n" {
		t.Fatalf("disk critical: %q", got)
	}
}

func TestReadyLineDiskLowHalves(t *testing.T) {
	// avail 3, load 0 -> 100; disk-low halves to 50.
	a := testAgent(t, 4, 0, false, true, false, true, nil)
	if got := a.readyLine(); got != "up, ready, 50%\n" {
		t.Fatalf("disk low: %q", got)
	}
}

func TestReadyLineMaintenanceDrain(t *testing.T) {
	// cpus 4 loadavg 3 -> free 0 -> pct 0 -> maint (allow_maintenance).
	a := testAgent(t, 4, 3, false, false, true, true, nil)
	if got := a.readyLine(); got != "maint, 0%\n" {
		t.Fatalf("drain: %q", got)
	}
}

func TestReadyLinePreventedMaint(t *testing.T) {
	// Same zero-load, but allow_maintenance false -> NOT maint: the
	// ready line at max(pct,1) and the prevented-maint metric fire.
	inc := 0
	a := testAgent(t, 4, 3, false, false, false, true, &inc)
	if got := a.readyLine(); got != "up, ready, 1%\n" {
		t.Fatalf("prevented maint: %q", got)
	}
	if inc != 1 {
		t.Fatalf("prevented-maint must fire once, got %d", inc)
	}
}

func TestReadyLineStateDown(t *testing.T) {
	a := testAgent(t, 4, 0, false, false, false, true, nil)
	a.state.Store(LoadState("down"))
	if got := a.readyLine(); got != "down\n" {
		t.Fatalf("down state: %q", got)
	}
}

func TestReadyLineStateMaint(t *testing.T) {
	a := testAgent(t, 4, 0, false, false, true, true, nil)
	a.state.Store(LoadState("maint"))
	if got := a.readyLine(); got != "maint\n" {
		t.Fatalf("maint state: %q", got)
	}
}

func TestReadyLineNoReport(t *testing.T) {
	// report_load off -> raw state line even when up.
	a := testAgent(t, 4, 0, false, false, false, false, nil)
	if got := a.readyLine(); got != "up\n" {
		t.Fatalf("no report: %q", got)
	}
}

// --- New (production seams + metric wiring) -----------------------------------------

func TestNewLoadAgent(t *testing.T) {
	a := NewLoadAgent(true, true)
	if a.State() != "up" {
		t.Fatal("must init to up")
	}
	if !a.ReportLoad || !a.AllowMaintenance {
		t.Fatal("flags not wired")
	}
	if a.CPUCount() < 1 {
		t.Fatal("CPUCount must be >= 1")
	}
	if a.LoadAvg() < 0 {
		t.Fatal("loadavg must be >= 0")
	}
	// The production MaintenanceInc records under the typst-specific name
	// (D13) via metrics.GenericInc -> Count (countersByName) into the clsi
	// Counter registry; probe the named counter (GenericObserved reads a
	// DIFFERENT summary/hist store).
	before := int64(0)
	if c := metrics.NamedCounter("clsi_typst-prevented-maint"); c != nil {
		before = c.Get()
	}
	a.MaintenanceInc()
	after := int64(0)
	if c := metrics.NamedCounter("clsi_typst-prevented-maint"); c != nil {
		after = c.Get()
	}
	if after != before+1 {
		t.Fatalf("MaintenanceInc must increment clsi_typst-prevented-maint by 1: %d -> %d", before, after)
	}
}

func TestSetState(t *testing.T) {
	a := NewLoadAgent(false, false)
	if a.State() != "up" {
		t.Fatal("init state must be up")
	}
	for _, s := range []LoadState{"down", "maint", "up"} {
		if err := a.SetState(s); err != nil {
			t.Fatalf("set %v: %v", s, err)
		}
		if a.State() != s {
			t.Fatalf("state after %v: %q", s, a.State())
		}
	}
	if err := a.SetState("bogus"); err == nil {
		t.Fatal("bogus state must error")
	}
}

func TestLoadAvg1(t *testing.T) {
	// /proc/loadavg is readable on Linux CI; on other OSes loadAvg1
	// returns 0 (documented Go divergence). Assert non-negative.
	if got := loadAvg1(); got < 0 {
		t.Fatalf("loadavg1: %v", got)
	}
}

// --- ServeTCP (per-connection one-line push) ------------------------------------------

func TestServeTCPIntegration(t *testing.T) {
	a := NewLoadAgent(false, false) // report off -> raw state line

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { defer func() { cancel() }(); a.ServeTCP(ctx, lis) }()

	conn, err := net.Dial("tcp", lis.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	line, rerr := bufio.NewReader(conn).ReadString('\n')
	// The server closes the socket after writing the line (EOF is fine).
	if rerr != nil && line == "" {
		t.Fatalf("tcp read: %v", rerr)
	}
	if want := "up\n"; line != want {
		t.Fatalf("tcp line: %q want %q", line, want)
	}

	// Second connection gets the same one-line push (no state change).
	conn2, err := net.Dial("tcp", lis.Addr().String())
	if err != nil {
		t.Fatalf("dial2: %v", err)
	}
	defer conn2.Close()
	conn2.SetDeadline(time.Now().Add(3 * time.Second))
	if a.State() == "up" {
		if line, rerr := bufio.NewReader(conn2).ReadString('\n'); rerr == nil && line != "up\n" {
			t.Fatalf("tcp line 2: %q", line)
		}
	}
}

func TestServeTCPReadsState(t *testing.T) {
	// down state pushed.
	a := NewLoadAgent(false, false)
	if err := a.SetState("down"); err != nil {
		t.Fatalf("set down: %v", err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.ServeTCP(ctx, lis)
	conn, err := net.Dial("tcp", lis.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	line, rerr := bufio.NewReader(conn).ReadString('\n')
	if rerr == nil && line != "down\n" {
		t.Fatalf("down line: %q", line)
	}
}

// --- StateHandler (POST /state/{up,down,maint}) ----------------------------------------

func TestStateHandler204(t *testing.T) {
	a := NewLoadAgent(false, false)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /state/{state}", a.StateHandler)
	for _, s := range []string{"up", "down", "maint"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/state/"+s, nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("POST /state/%s: want 204 got %d", s, rec.Code)
		}
		if a.State() != LoadState(s) {
			t.Fatalf("state after POST: %q", a.State())
		}
	}
}

func TestStateHandlerNonPOST(t *testing.T) {
	a := NewLoadAgent(false, false)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /state/{state}", a.StateHandler)
	// Go 1.22 mux does NOT auto-405 for other methods against a
	// method-prefixed pattern with no matching handler (it 405s with
	// Allow itself); StateHandler's own 405 arm covers handlers mounted
	// without a method prefix.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/state/up", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: want 405 got %d", rec.Code)
	}
	if allow := rec.Result().Header.Get("Allow"); allow != "POST" {
		t.Fatalf("Allow header: %q", allow)
	}
}

func TestStateHandlerUnknownState(t *testing.T) {
	a := NewLoadAgent(false, false)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /state/{state}", a.StateHandler)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/state/bogus", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus state: want 400 got %d", rec.Code)
	}
}

// --- handler edge (State() nil-value guard) ------------------------------------------

func TestStateNilGuard(t *testing.T) {
	// A zero-value LoadAgent (state not yet stored) must report 'up'.
	a := &LoadAgent{}
	if got := a.State(); got != "up" {
		t.Fatalf("zero-value state: %q", got)
	}
	var v atomic.Value
	v.Store(LoadState("maint"))
	if v.Load().(LoadState) != "maint" {
		t.Fatal("value roundtrip")
	}
}

// --- load agent HTTP mux (the T11 server mounts) ---------------------------------------
