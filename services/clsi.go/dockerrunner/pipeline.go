package dockerrunner

// pipeline.go ports the async core of DockerRunner.mjs:
//   _runAndWaitForContainer  -> runOnce (gate: streamEnded && containerReturned)
//   _startContainer          -> startOnce (lock + inspect + create-if-404 + attach + start)
//   waitForContainer         -> waitForContainer (timer + kill + wait + check order)
//   attachToContainer/stream  -> drainStream (8-byte demux) + markStream (gate)
//
// Node is event-driven (callback + _.once). Go blocks and uses a runCtx gate
// with a first-winner "fired" flag so the user callback fires exactly once.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"clsi/commandrunner"
	"clsi/dockerlockmanager"
	clsl "clsi/logger"
)

// truncMarker mirrors Node `(...truncated at ${MAX_OUTPUT} chars...)`.
var truncMarker = fmt.Sprintf("(...truncated at %d chars...)", maxOutput)

// runCtx is the per-run state for the fire-once gate.
//
//   - fired: the user callback has been delivered (mirrors Node's `_.once`).
//   - streamEnded / containerReturned: the two Node flags (the callback fires
//     the success path only when BOTH are true).
//   - stdout / stderr / exitCode: the RunOutput payload for the success path.
//
// Every terminal action (an error delivery, or the success fire) checks
// fired under mu and sets it; the first to flip it wins. This is the Go
// equivalent of Node's `_.once(callback)`.
type runCtx struct {
	mu              sync.Mutex
	fired           bool
	streamEnded     bool
	containerReturned bool
	stdout         string
	stderr         string
	exitCode       int
	cb             func(err error, out *commandrunner.RunOutput)
}

// deliverError fires the user callback immediately with an error (first-wins).
// Mirrors Node `callback(error)` / `callbackIfFinished` never reached.
func (rc *runCtx) deliverError(err error) {
	rc.mu.Lock()
	fired := rc.fired
	rc.fired = true
	cb := rc.cb
	rc.mu.Unlock()
	if fired {
		return
	}
	cb(err, nil)
}

// markStream mirrors attachStreamHandler(null, {stdout, stderr}) on stream end:
// it records the captured output and sets streamEnded, then fires if both.
func (rc *runCtx) markStream(stdout, stderr string) {
	rc.mu.Lock()
	rc.stdout = stdout
	rc.stderr = stderr
	rc.streamEnded = true
	rc.mu.Unlock()
	rc.tryFireSuccess()
}

// markContainer mirrors `output.exitCode = code; containerReturned = true;
// callbackIfFinished()` for the non-137/non-1 exit path.
func (rc *runCtx) markContainer(code int) {
	rc.mu.Lock()
	rc.exitCode = code
	rc.containerReturned = true
	rc.mu.Unlock()
	rc.tryFireSuccess()
}

// tryFireSuccess is callbackIfFinished(): fires the success callback only when
// BOTH flags are set (and it hasn't fired already).
func (rc *runCtx) tryFireSuccess() {
	rc.mu.Lock()
	streamEnded := rc.streamEnded
	containerReturned := rc.containerReturned
	exitCode := rc.exitCode
	stdout := rc.stdout
	stderr := rc.stderr
	fired := rc.fired
	if !streamEnded || !containerReturned || fired {
		rc.mu.Unlock()
		return
	}
	rc.fired = true
	cb := rc.cb
	rc.mu.Unlock()
	cb(nil, &commandrunner.RunOutput{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	})
}

// runOnce ports _runAndWaitForContainer(options, volumes, timeout, _callback).
// It blocks until the run is done (the caller runs it in a goroutine).
//
// Phases:
//  1. startOnce: lock + inspect + create-if-404 + attach (+ spawn drain) + start.
//     On error -> deliver the error.
//  2. waitForContainer: timer + kill-on-timeout + blocking wait.
//     On error -> deliver; 137 -> TerminatedError; 1 -> ExitedError;
//     otherwise markContainer (success gate).
func (d *DockerRunner) runOnce(opts CreateOpts, timeout int64, cb func(err error, out *commandrunner.RunOutput)) {
	rc := &runCtx{cb: cb}

	if err := d.startOnce(rc, opts, cb); err != nil {
		clsl.Debug(map[string]any{"projectId": opts.Name}, "error starting container")
		rc.deliverError(err)
		return
	}

	code, werr := d.waitForContainer(opts, timeout)
	if werr != nil {
		rc.deliverError(werr)
		return
	}

	switch code {
	case 137:
		// exit status from kill -9
		rc.deliverError(&TerminatedError{})
		return
	case 1:
		// exit status from chktex
		rc.deliverError(&ExitedError{Code: code})
		return
	default:
		clsl.Debug(map[string]any{"exitCode": code, "containerId": opts.Name},
			"docker container has exited")
		rc.markContainer(code)
	}
}

// startOnce ports _startContainer:
//
//   LockManager.runWithLock(name, release -> _startContainer(options, volumes,
//   attachStreamHandler, release), callback)
//
// The lock window covers inspect + create(only on 404) + attach + start. The
// drain goroutine is spawned AFTER a successful attach and outlives the lock
// release (the attach stream stays open until the container stops).
//
// Node returns the error via the release-wrapped callback; here startOnce
// returns it (nil on success), which runOnce then delivers.
func (d *DockerRunner) startOnce(rc *runCtx, opts CreateOpts, cb func(err error, out *commandrunner.RunOutput)) error {
	err := dockerlockmanager.RunWithLock(opts.Name, func(release func()) error {
		defer release()

		// inspect: 404 -> create; other error -> return it; ok -> start existing.
		_, ierr := d.Engine.Inspect(opts.Name)
		if ierr != nil {
			var api *APIError
			if errors.As(ierr, &api) && api.StatusCode == 404 {
				// create and start (createAndStartContainer()).
				if cerr := d.Engine.Create(opts.Name, opts); cerr != nil {
					return cerr
				}
			} else {
				clsl.Err(map[string]any{"containerName": opts.Name, "err": ierr},
					"unable to inspect container to start")
				return ierr
			}
		}

		// attachToContainer(options.name, attachStreamHandler, attachStartCallback).
		clsl.Debug(map[string]any{"containerName": opts.Name}, "starting container")
		stream, aerr := d.Engine.Attach(opts.Name)
		if aerr != nil {
			clsl.Error(map[string]any{"err": aerr, "containerId": opts.Name},
				"error attaching to container")
			return aerr
		}
		clsl.Debug(map[string]any{"containerId": opts.Name}, "attached to container")
		go d.drainStream(rc, stream, opts.Name)

		// start (304 = already running, treated as success).
		serr := d.Engine.Start(opts.Name)
		if serr != nil {
			var api *APIError
			if errors.As(serr, &api) && api.StatusCode == 304 {
				// already running
				clsl.Debug(map[string]any{"containerId": opts.Name},
					"container already started")
				return nil
			}
			return serr
		}
		return nil
	})
	if err != nil {
		clsl.Err(map[string]any{"containerId": opts.Name, "err": err},
			"error starting container (lock/phase failed)")
	}
	return err
}

// waitForContainer ports waitForContainer(containerId, timeout, options, cb).
//
// It blocks for `timeout` ms (a goroutine flips timedOut and kills on fire),
// then a blocking Wait returns the exit code. Check order (faithful):
//  1. wait error 404 + AutoRemove -> (0, nil)
//  2. any wait error -> (0, error)
//  3. timedOut -> (0, *TimedOutError)
//  4. exit code -> (code, nil)
func (d *DockerRunner) waitForContainer(opts CreateOpts, timeout int64) (int, error) {
	// settled: mirrors clearTimeout — the first of (timer, wait-return) to
	// store-true wins; the loser aborts. The timer must still set timedOut
	// before killing (Node: `timedOut = true` then `container.kill`), and the
	// check ordering after Wait is faithful (404+AutoRemove, error, timedOut,
	// code). A kill-on-timeout yields code 137 which the timedOut check maps
	// to TimedOutError (Node: the flag is checked before res.StatusCode).
	var settled atomic.Bool
	var timedOut atomic.Bool
	go func() {
		time.Sleep(time.Duration(timeout) * time.Millisecond)
		if !settled.CompareAndSwap(false, true) {
			return
		}
		timedOut.Store(true)
		clsl.Debug(map[string]any{"containerId": opts.Name}, "timeout reached, killing container")
		if kerr := d.Engine.Kill(opts.Name); kerr != nil {
			clsl.Warn(map[string]any{"err": kerr, "containerId": opts.Name},
				"failed to kill container")
		}
	}()

	code, werr := d.Engine.Wait(opts.Name)
	settled.Store(true)
	if werr != nil {
		var api *APIError
		if errors.As(werr, &api) && api.StatusCode == 404 && opts.HostConfig.AutoRemove {
			clsl.Debug(map[string]any{"containerId": opts.Name},
				"auto-destroy container destroyed before starting to wait")
			return 0, nil
		}
		clsl.Warn(map[string]any{"err": werr, "containerId": opts.Name},
			"error waiting for container")
		return 0, werr
	}
	if timedOut.Load() {
		clsl.Debug(map[string]any{"containerId": opts.Name}, "docker container timed out")
		return 0, &TimedOutError{}
	}
	clsl.Debug(map[string]any{"containerId": opts.Name, "exitCode": code},
		"docker container returned")
	return code, nil
}

// --- stream draining + demux (attachToContainer's demuxStream) ---

// cappedSink mirrors createStringOutputStream(name): it appends until its
// length reaches >= maxOutput, appends the truncation marker once, and then
// stops appending (overflowed). Mirrors Node UTF-16 char counting; Go counts
// bytes (identical for ASCII; see package divergence 4).
type cappedSink struct {
	name       string
	data       []byte
	overflowed bool
}

func (s *cappedSink) write(p []byte) {
	if s.overflowed {
		return
	}
	if len(s.data) < maxOutput {
		s.data = append(s.data, p...)
		return
	}
	clsl.Info(map[string]any{"containerId": s.name, "length": len(s.data), "maxLen": maxOutput},
		fmt.Sprintf("%s exceeds max size", s.name))
	s.data = append(s.data, truncMarker...)
	s.overflowed = true
}

// demuxer is a port of docker-modem's demuxStream: 8-byte frames
// [type(1)][pad(3)][u32BE len][content]. type 1 -> stdout, type 0/2 -> stderr.
// An invalid type (3+) flips to raw passthrough of every following byte to
// stdout (mirrors the "misaligned stream" fallback).
type demuxer struct {
	buf         []byte
	pendingType uint8
	pendingLen  uint32
	rawMode     bool
	stdout      *cappedSink
	stderr      *cappedSink
}

const noPendingType uint8 = 0xFF

func (dm *demuxer) Write(p []byte) {
	if dm.rawMode {
		dm.stdout.write(p)
		return
	}
	dm.buf = append(dm.buf, p...)
	for {
		if dm.pendingType == noPendingType {
			if len(dm.buf) < 8 {
				return
			}
			typ := dm.buf[0]
			ln := binary.BigEndian.Uint32(dm.buf[4:8])
			dm.buf = dm.buf[8:]
			if typ != 0 && typ != 1 && typ != 2 {
				dm.rawMode = true
				dm.stdout.write(dm.buf)
				dm.buf = nil
				return
			}
			dm.pendingType = typ
			dm.pendingLen = ln
		}
		if len(dm.buf) < int(dm.pendingLen) {
			return
		}
		content := dm.buf[:dm.pendingLen]
		if dm.pendingType == 1 {
			dm.stdout.write(content)
		} else {
			dm.stderr.write(content)
		}
		dm.buf = dm.buf[dm.pendingLen:]
		dm.pendingType = noPendingType
		dm.pendingLen = 0
	}
}

// drainStream is a port of attachToContainer's stream handling: it demuxes the
// attach stream until EOF, then calls markStream (mirrors stream 'end').
//
// Divergence (documented): Node calls attachStreamHandler only on stream
// 'end' and logs (without calling the handler) on 'error'. Go finalizes the
// gate when the read loop completes — on normal EOF this is identical; on a
// read error Go logs then finalizes (Node would hang the run). The error
// path here only matters if the attach stream breaks before the container
// stops, which does not happen for a healthy run.
func (d *DockerRunner) drainStream(rc *runCtx, stream io.ReadCloser, containerID string) {
	defer stream.Close()
	stdout := &cappedSink{name: "stdout"}
	stderr := &cappedSink{name: "stderr"}
	dm := &demuxer{
		pendingType: noPendingType,
		stdout:      stdout,
		stderr:      stderr,
	}

	buf := make([]byte, 64*1024)
	for {
		n, rerr := stream.Read(buf)
		if n > 0 {
			dm.Write(buf[:n])
		}
		if rerr != nil {
			clsl.Error(map[string]any{"err": rerr, "containerId": containerID},
				"error reading from container stream")
			break
		}
	}
	// stream 'end' -> attachStreamHandler(null, {stdout, stderr})
	rc.markStream(string(stdout.data), string(stderr.data))
}
