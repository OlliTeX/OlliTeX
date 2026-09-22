// run.go: the Runner-contract entry points (Run, Kill) and the 500-retry
// wrapper. Mirrors DockerRunner.run + DockerRunner.kill.
package dockerrunner

import (
	"errors"

	"clsi/commandrunner"
	clsl "clsi/logger"
)

// Run ports DockerRunner.run(projectId, command, directory, image, timeout,
// environment, compileGroup, cwd, callback) -> name.
//
//   - Pre-validates image (allowed-images guard) SYNCHRONOUSLY: on "image not
//     allowed" the callback fires inline and "" is returned (Node returns
//     undefined in that case; the consumer's fired flag is handled via the
//     synchronous callback).
//   - Otherwise the name is returned and the run drives in a goroutine.
//   - A single 500-retry harness wraps the run: on a dockerode 500 it
//     force-destroys the container and retries ONCE with the user callback.
func (d *DockerRunner) Run(projectID string, command []string, directory string, image string,
	timeout int64, environment map[string]string, compileGroup string, cwd string,
	callback func(err error, out *commandrunner.RunOutput)) string {

	opts, err := d.buildOpts(projectID, command, directory, image, timeout, environment, compileGroup, cwd)
	if err != nil {
		// Node: `return callback(new Error('image not allowed'))`.
		callback(err, nil)
		return ""
	}

	// 500-retry harness (the outer _callback in Node). runOnce is the inner
	// _runAndWaitForContainer; it calls this finalCB exactly once.
	finalCB := func(err error, out *commandrunner.RunOutput) {
		var api *APIError
		if err != nil && errors.As(err, &api) && api.StatusCode == 500 {
			clsl.Debug(map[string]any{"projectId": projectID, "err": err},
				"error running container so destroying and retrying")
			// destroyContainer(name, null, true, cb) -> containerId null -> name.
			if derr := d.destroy(opts.Name, opts.Name, true); derr != nil {
				callback(derr, nil)
				return
			}
			// Retry ONCE with the user's callback (no retry harness).
			d.runOnce(opts, timeout, callback)
			return
		}
		callback(err, out)
	}

	clsl.Debug(map[string]any{"projectId": projectID}, "running docker container")
	go d.runOnce(opts, timeout, finalCB)
	return opts.Name
}

// Kill ports DockerRunner.kill(containerId, callback). The case-sensitive
// "Cannot kill container .* is not running" regex does NOT match the live
// engine (whose message is lowercase "cannot kill container"), so the 409
// PROPAGATES (documented divergence 3). Kept for parity with a
// capital-C-emitting proxy.
func (d *DockerRunner) Kill(containerID string, callback func(err error)) {
	clsl.Debug(map[string]any{"containerId": containerID}, "sending kill signal to container")
	go func() {
		err := d.Engine.Kill(containerID)
		if err != nil && notRunningKillRE.MatchString(err.Error()) {
			clsl.Warn(map[string]any{"containerId": containerID, "err": err},
				"container not running, continuing")
			err = nil
		}
		if err != nil {
			clsl.Error(map[string]any{"containerId": containerID, "err": err}, "error killing container")
			callback(err)
			return
		}
		callback(nil)
	}()
}
