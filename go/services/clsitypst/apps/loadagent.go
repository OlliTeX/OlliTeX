// loadagent.go ports the clsi_typst load-balancer agent (Node app.js L150-220).
//
// Two listeners (separate from the main apps handler, D13):
//   - TCP (CLSI_TYPST_LOAD_PORT, default 3046): per connection push ONE line
//     and close the socket (Node: socket.write(line); socket.end() — NO
//     command loop: the 3-line command loop in the clsi.go HANDOFF is a
//     TEX-fork artifact and does not apply to clsi_typst).
//   - HTTP (CLSI_TYPST_LOCAL_PORT, default 3047): POST /state/{up,down,maint}
//     -> 204 (the orchestrator flips the state).
//
// The readyLine formula is Node app.js L162-199 verbatim (the free-load
// percentage), with the disk adjustments from the REDUCED PPM:
// disk-critical -> 0%; disk-low -> pct/2 (float division, Node parity).
// The maintained-metric is the typst-specific
// clsi_typst-prevented-maint (Node: Metrics.inc('clsi_typst-prevented-maint')).
package apps

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"ollitex/go/services/clsitypst/metrics"
	projectpersistence "ollitex/go/services/clsitypst/projectpersistence"
)

// LoadState is the agent state pushed to / read by the LB (Node `STATE`):
// 'up' | 'down' | 'maint'.
type LoadState string

// loadStates mirrors `const states = ['up', 'down', 'maint']`.
var loadStates = []LoadState{"up", "down", "maint"}

// LoadAgent ports the LB agent (Node app.js 244-315). Node reads os.cpus()
// and os.loadavg() inline; both (and the PPM disk flags) are injectable
// seams so the formula is unit-testable.
type LoadAgent struct {
	state atomic.Value // LoadState, initial 'up'

	CPUCount   func() int
	LoadAvg    func() float64
	ReportLoad bool // Settings.internal.load_balancer_agent.report_load
	// AllowMaintenance mirrors allow_maintenance (drain -> maint flip).
	AllowMaintenance bool
	// MaintenanceInc (Node: Metrics.inc('clsi_typst-prevented-maint'))
	// fires when the agent would have gone maint but allow_maintenance is
	// off (or the state is non-up at a zero load).
	MaintenanceInc func()

	isDiskCritical func() bool // node ProjectPersistenceManager.isAnyDiskCriticalLow
	isDiskLow      func() bool // node ProjectPersistenceManager.isAnyDiskLow
}

// loadAvg1 reads the 1-minute load average (Node os.loadavg()[0]) from
// /proc/loadavg (Linux); 0 outside Linux / on error (the formula then
// reports full capacity — a documented Go divergence: Node uses os.loadavg
// on every OS).
func loadAvg1() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return v
}

// NewLoadAgent builds the production agent, initialised to state 'up'
// (Node: `let STATE = 'up'`), with the real seams.
func NewLoadAgent(allowMaintenance, reportLoad bool) *LoadAgent {
	a := &LoadAgent{
		ReportLoad:       reportLoad,
		AllowMaintenance: allowMaintenance,
		MaintenanceInc:   func() { metrics.GenericInc("clsi_typst-prevented-maint", 1) },
		isDiskCritical:   projectpersistence.IsAnyDiskCriticalLow,
		isDiskLow:        projectpersistence.IsAnyDiskLow,
	}
	a.state.Store(LoadState("up"))
	a.CPUCount = func() int { return runtime.NumCPU() }
	a.LoadAvg = loadAvg1
	return a
}

// State returns the current state (initial 'up').
func (a *LoadAgent) State() LoadState {
	if v := a.state.Load(); v != nil {
		return v.(LoadState)
	}
	return LoadState("up")
}

// SetState mirrors the Node `POST /state/{up,down,maint}` handler: flips the
// state (Node: logger.debug + res.sendStatus(204)); unknown states error so
// the caller renders 400.
func (a *LoadAgent) SetState(s LoadState) error {
	for _, ok := range loadStates {
		if s == ok {
			a.state.Store(s)
			return nil
		}
	}
	return fmt.Errorf("unknown load state: %q", s)
}

// readyLine computes the pushed line for a single LB connection
// (Node app.js 162-199, verbatim formula; disk adjustments per the LOCKED
// T10 spec: disk-critical -> 0, disk-low -> pct/2 (float, Node parity)).
func (a *LoadAgent) readyLine() string {
	state := a.State()
	if state != "up" || !a.ReportLoad {
		// Node: socket.write(`${STATE}\n`); socket.end() — 'down\n' or
		// 'maint\n' (a maint state means the orchestrator flipped it).
		return string(state) + "\n"
	}

	cpus := a.CPUCount()
	avail := cpus
	// Node: staging clsi's have 1 cpu core only.
	if cpus <= 1 {
		avail = 1
	} else {
		avail = cpus - 1
	}
	load := a.LoadAvg()
	freeLoad := float64(avail) - load
	pct := math.Round((freeLoad / float64(avail)) * 100)
	if a.isDiskCritical() {
		pct = 0
	}
	if a.isDiskLow() {
		pct = pct / 2
	}

	if a.AllowMaintenance && pct <= 0 {
		// When it's 0 the server is set to drain implicitly.
		return "maint, 0%\n"
	}

	// Ready will cancel the maint state.
	max := pct
	if max < 1 {
		max = 1
	}
	if pct <= 0 {
		// This metric records how often we would have gone into
		// maintenance mode.
		a.MaintenanceInc()
	}
	return fmt.Sprintf("up, ready, %v%%\n", max)
}

// ServeTCP runs the TCP listener: for each connection push one line and
// close the socket (Node: socket.write + socket.end). Returns when the
// listener stops accepting (tests Close() it) or the context cancels.
func (a *LoadAgent) ServeTCP(ctx context.Context, lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer func() { _ = c.Close() }()
			// Node: socket.on('error') — ECONNRESET is silent, anything
			// else is a logged drop; the Go form swallows the write error
			// (no consumer) and just closes.
			_, _ = io.WriteString(c, a.readyLine())
		}(conn)
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// StateHandler is the load-HTTP route `POST /state/{up,down,maint}` (Node
// app.js 204-220): flips the state and acknowledges with 204 (Node
// res.sendStatus(204)); non-POST -> 405 with the Allow header (Go mux
// does not add it, the Node express 405 does); unknown state -> 400.
func (a *LoadAgent) StateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := a.SetState(LoadState(r.PathValue("state"))); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
