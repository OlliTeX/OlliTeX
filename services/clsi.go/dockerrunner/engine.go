// Package dockerrunner ports services/clsi/app/js/DockerRunner.mjs (634L),
// the mandatory sandboxed Docker compile runner (dockerode-based).
//
// Implements the commandrunner.Runner contract and drives: the Docker
// engine via the Engine SPI (unixengine.go in production, fakes in tests),
// dockerlockmanager (container-name locks), lastprojectaccess and logger.
//
// # Error contract (Node error flags -> Go typed errors)
//
//   | Node error flags / message                  | Go error                          |
//   | ------------------------------------------- | --------------------------------- |
//   | err.terminated (exit 137, kill -9)           | *TerminatedError  msg "terminated"|
//   | `new Error('exited'); err.code = exitCode=1` | *ExitedError{1}   msg "exited"    |
//   | error.timedout ("container timed out")       | *TimedOutError    msg "container timed out" |
//   | dockerode HTTP error (statusCode N)          | *APIError{StatusCode: N, ...}     |
//   | plain `new Error('image not allowed')`       | errors.New(...)                   |
//   | DockerLockManager "Lock timeout"             | *dockerlockmanager.LockTimeoutError |
//
// callback contract: err != nil implies out == nil (mirrors the consumer:
// CompileManager/LatexRunner only check flags/message of the error object).
//
// # Documented divergences from Node (everything else is 1:1)
//
//  1. fingerprint: Node computes MD5 over JS-insertion-ordered
//     JSON.stringify(options) (no name key yet, includes per-group
//     overrides). Go computes MD5 over a Go struct marshalled with sorted
//     keys. Go map iteration order is randomized (documented Go behavior,
//     NOT sorted), but every field we set is a slice/struct, so no map
//     ordering enters the hash for the keys CLSI actually uses. Names are
//     only ever compared within this Go deployment => container reuse works.
//  2. Env order: Node emits Env in JS object iteration order; Go sorts
//     keys alphabetically (Docker: order-irrelevant).
//  3. kill "not running" 409: dockerode's engine error messages contain
//     the lowercase "cannot kill container: X...is not running"; the Node
//     regex /Cannot kill container .* is not running/ is CASE-SENSITIVE and
//     does NOT match, so the error PROPAGATES (confirmed against the live
//     engine). Go mirrors: the 409 propagates.
//  4. Output cap: Node caps UTF-16 code units; Go caps bytes (identical
//     for ASCII output).
//  5. Start 304: Node checks `error.statusCode !== 304`; Go checks
//     errors.As(&APIError) && StatusCode==304 (dockerode models 304 as an
//     error object with that status).
//  6. Monitor side effect: Node startContainerMonitor() runs at module
//     import; Go requires an explicit StartContainerMonitor() call
//     (documented; cmd/clsi.go calls it exactly once).
//  7. Settings.clsi.docker.Readonly: the Node branch is dead in CE
//     (default undefined); Go omits that branch (documented).
//  8. Lock release timing: Node _startContainer wraps start + attach in
//     the lock and releases after the start callback; Go takes the lock
//     for inspect+create+attach+start and releases when the start phase
//     finishes (same window).
package dockerrunner

import (
	"fmt"
	"io"
	"regexp"
)

// oneHourMS mirrors Node ONE_HOUR_IN_MS.
const oneHourMS = int64(60 * 60 * 1000)

// maxOutput mirrors Node MAX_OUTPUT = 1024*1024*2 (2 MiB per stream).
const maxOutput = 1024 * 1024 * 2

var (
	// /Cannot kill container .* is not running/ (case-sensitive; see
	// divergence 3 — the live engine message does NOT match).
	notRunningKillRE = regexp.MustCompile(`Cannot kill container .* is not running`)
	// /\/project-([a-f0-9]{24})-/
	projectIDNameRE  = regexp.MustCompile(`/project-([a-f0-9]{24})-`)
	// /:([0-9]+)\.[0-9]+|:TL([0-9]+)/
	imageYearRE      = regexp.MustCompile(`:([0-9]+)\.[0-9]+|:TL([0-9]+)`)
)

// --- Dockerode-style API error (wire parity; see APIError). ---
type APIError struct {
	StatusCode int
	Reason     string
	Message    string
}

// Error returns the dockerode message shape:
//
//	"(HTTP code " + statusCode + ") " + reason + " - " + message + " "
func (e *APIError) Error() string {
	return fmt.Sprintf("(HTTP code %d) %s - %s ", e.StatusCode, e.Reason, e.Message)
}

// reason: per-operation statusCodes[N] || "unexpected" (dockerode).
func reason(code int) string {
	switch code {
	case 304:
		return "container already started"
	case 400:
		return "bad parameter"
	case 404:
		return "no such container"
	case 406:
		return "impossible to attach"
	case 500:
		return "server error"
	default:
		return "unexpected"
	}
}

// --- Termination / exit / timeout errors (Node flag errors). ---

// TerminatedError: exit 137 (kill -9). Node: `err.terminated = true`.
type TerminatedError struct{}

func (*TerminatedError) Error() string { return "terminated" }

// ExitedError: exit 1 (chktex). Node: `err.code = exitCode; Error('exited')`.
type ExitedError struct{ Code int }

func (*ExitedError) Error() string { return "exited" }

// TimedOutError: wait timed out. Node: `error.timedout = true; 'container timed out'`.
type TimedOutError struct{}

func (*TimedOutError) Error() string { return "container timed out" }

// --- Engine SPI + wire types ---

// Engine is the Docker surface this port uses (dockerode equivalents).
// Implementations: unixengine.go (HTTP over unix socket), fakes in tests.
// All methods block until the engine responds; runner goroutines manage
// concurrency.
type Engine interface {
	// Inspect: GET /containers/{id}/json (the engine's old route; /inspect
	// is 404 "page not found"). 404 => unknown container.
	Inspect(id string) (*ContainerInfo, error)
	// Create: POST /containers/create?name=<name> body = opts (probe-verified:
	// name from query, config from body).
	Create(id string, opts CreateOpts) error
	// Attach: POST /containers/{id}/attach?stdout=1&stderr=1&stream=1 => raw
	// 8-byte-header frames: [type][pad3][size u32be][content].
	Attach(id string) (io.ReadCloser, error)
	// Start: POST /containers/{id}/start. 304 = already running.
	Start(id string) error
	// Wait: POST /containers/{id}/wait => exit code (404 if autoRemoved).
	Wait(id string) (int, error)
	// Kill: POST /containers/{id}/kill. 409 = not running.
	Kill(id string) error
	// Remove: DELETE /containers/{id}?force=<bool>&v=true.
	Remove(id string, force bool) error
	// List: GET /containers/json?all=true.
	List() ([]ListedContainer, error)
}

// ContainerInfo: inspect result (used for existence check only).
type ContainerInfo struct {
	ID      string `json:"Id"`
	Name    string `json:"Name"`
	Running bool   `json:"State"`
}

// CreateOpts: container creation document (dockerode wire keys).
type CreateOpts struct {
	Name            string     `json:"name,omitempty"`
	Cmd             []string   `json:"Cmd"`
	Image           string     `json:"Image"`
	WorkingDir      string     `json:"WorkingDir"`
	NetworkDisabled bool       `json:"NetworkDisabled"`
	Memory          int64      `json:"Memory"`
	User            string     `json:"User,omitempty"`
	Env             []string   `json:"Env,omitempty"`
	HostConfig      HostConfig `json:"HostConfig"`
}

// HostConfig: creation HostConfig wire keys (dockerode parity subset).
type HostConfig struct {
	AutoRemove  bool     `json:"AutoRemove,omitempty"`
	Binds       []string `json:"Binds,omitempty"`
	LogConfig   LogConfig `json:"LogConfig"`
	Ulimits     []Ulimit `json:"Ulimits,omitempty"`
	CapDrop     []string  `json:"CapDrop,omitempty"`
	SecurityOpt []string  `json:"SecurityOpt,omitempty"`
	Runtime     string    `json:"Runtime,omitempty"`
}

// LogConfig: creation HostConfig.LogConfig wire keys.
// Config must be a MAP (Node emits LogConfig: { Type: 'none', Config: {} });
// a non-nil map marshals as {}.
type LogConfig struct {
	Type   string            `json:"Type,omitempty"`
	Config map[string]string `json:"Config,omitempty"`
}

// Ulimit: a creation Ulimits entry.
type Ulimit struct {
	Name string `json:"Name"`
	Soft int    `json:"Soft,omitempty"`
	Hard int    `json:"Hard,omitempty"`
}

// ListedContainer: dockerode list entry surface (list + expiry).
type ListedContainer struct {
	Id      string   `json:"Id"`
	Name    string   `json:"Name,omitempty"`
	Names   []string `json:"Names,omitempty"`
	Created int64    `json:"Created"`
}
