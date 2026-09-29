package apps

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"ollitex/go/services/clsitex/metrics"
	"ollitex/go/services/clsitex/projectpersistence"
)

// LoadState is the agent state pushed to / read by the load balancer
// (Node app.js STATE: 'up' | 'down' | 'maint').
type LoadState string

// loadStates mirrors `const states = ['up', 'down', 'maint']`.
var loadStates = []LoadState{"up", "down", "maint"}

// LoadAgent ports the load-balancer agent: a TCP listener that pushes one
// status line to the LB per connection, plus an HTTP endpoint that lets the
// orchestrator flip the state (app.js 244-315).
//
// Go note: the Node agent reads os.cpus().length and os.loadavg() inline;
// both are injectable seams here so the formula is unit-testable, and the
// disk checks (ProjectPersistenceManager package-level functions) are
// injectable too.
type LoadAgent struct {
	state atomic.Value // LoadState

	CPUCount         func() int     // os.cpus().length
	LoadAvg          func() float64 // os.loadavg()[0]
	AllowMaintenance bool           // settings.allowed_maintenance
	ReportLoad       bool           // agent_settings.report_load
	MaintenanceInc   func()         // metrics.inc('clsi-prevented-maint')

	isDiskCritical func() bool // prod: ProjectPersistenceManager.IsAnyDiskCriticalLow
	isDiskLow      func() bool // prod: ProjectPersistenceManager.IsAnyDiskLow
}

// --- os helpers (staging CLSIs have 1 cpu core; os.loadavg is a Node API) ---

// runtimeNumCPU returns the logical CPU count (os.cpus().length).
func runtimeNumCPU() int { return runtime.NumCPU() }

// loadAvg reads /proc/loadavg (Linux, first field: the 1-minute average).
// Fallback 0 outside Linux / on read error (the formula then reports 100%)
// — a documented Go divergence, Node uses os.loadavg() everywhere.
func loadAvg() float64 {
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
// (Node: `let STATE = 'up'`) with the real seams.
func NewLoadAgent(allowMaintenance, reportLoad bool) *LoadAgent {
	a := &LoadAgent{
		AllowMaintenance: allowMaintenance,
		ReportLoad:       reportLoad,
		MaintenanceInc:   func() { metrics.GenericInc("clsi-prevented-maint", 1) },
		isDiskCritical:   projectpersistence.IsAnyDiskCriticalLow,
		isDiskLow:        projectpersistence.IsAnyDiskLow,
	}
	a.state.Store(LoadState("up"))
	a.CPUCount = func() int { return runtimeNumCPU() }
	a.LoadAvg = func() float64 { return loadAvg() }
	return a
}

// State returns the current state (initially 'up').
func (a *LoadAgent) State() LoadState {
	if v := a.state.Load(); v != nil {
		return v.(LoadState)
	}
	return LoadState("up")
}

// SetState mirrors the Node `POST /state/{up,down,maint}` handler: flips the
// state (logger.debug side-effect only, not a state change); unknown states
// are rejected by returning an error so the caller renders 400.
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
// (app.js 244-299: up+report branch, verbatim formula).
func (a *LoadAgent) readyLine() string {
	state := a.State()
	if state == "maint" {
		return "maint, 0%\n"
	}

	if state == "up" && a.ReportLoad {
		cpus := a.CPUCount()
		avail := cpus
		// staging clsi's have 1 cpu core only
		if cpus <= 1 {
			avail = 1
		} else {
			avail = cpus - 1
		}

		load := a.LoadAvg()
		freeLoad := float64(avail) - load
		pct := int(round100((freeLoad / float64(avail)) * 100))
		if a.isDiskCritical() {
			pct = 0
		}
		if a.isDiskLow() {
			pct = pct / 2
		}

		if a.AllowMaintenance && pct <= 0 {
			// When it's 0 the server is set to drain implicitly.
			// Drain will move new projects to different servers.
			// Drain will keep existing projects assigned to the same server.
			// Maint will move existing and new projects to different servers.
			return "maint, 0%\n"
		}

		// Ready will cancel the maint state.
		max := 1
		if pct > max {
			max = pct
		}
		if pct <= 0 {
			// This metric records how often we would have gone into
			// maintenance mode.
			a.MaintenanceInc()
		}
		return fmt.Sprintf("up, ready, %d%%\n", max)
	}

	return string(state) + "\n"
}

// ServeTCP runs the TCP listener: for each connection push one line and
// close the socket (app.js 244-297: socket.end()). Returns when the listener
// stops accepting (test servers close via Close()).
func (a *LoadAgent) ServeTCP(ctx context.Context, lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer func() { _ = c.Close() }()
			_, _ = fmt.Fprint(c, a.readyLine())
		}(conn)
	}
}

// StateHandler is the HTTP route `POST /state/{up,down,maint}` (app.js
// 300-315): flips the state and acknowledges with 204.
func (a *LoadAgent) StateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	state := ""
	if len(parts) > 0 {
		state = parts[len(parts)-1]
	}
	if err := a.SetState(LoadState(state)); err != nil {
		// 400 unknown state (Node express defaults: 404 for unknown route).
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// round100 ports Math.round (Node: Math.round((freeLoad/avail)*100) — the
// *100 is folded into the caller passing the ratio).
func round100(v float64) float64 { return math.Round(v) }
