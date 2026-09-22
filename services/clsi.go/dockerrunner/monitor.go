package dockerrunner

// monitor.go ports the container-expiry monitor of DockerRunner.mjs:
//
//   destroyContainer / _destroyContainer -> destroy (lock + Remove, 404 ok)
//   examineOldContainer                   -> containerAge (inline)
//   destroyOldContainers                  -> destroyOldContainers (List + per-destroy)
//   startContainerMonitor / stop...       -> StartContainerMonitor / StopContainerMonitor
//
// Node runs the monitor from module import; Go requires an explicit
// StartContainerMonitor() call (see package divergence 6).

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"clsi/dockerlockmanager"
	"clsi/lastprojectaccess"
	clsl "clsi/logger"
)

// destroy ports destroyContainer(containerName, containerId, shouldForce, cb).
// The lock key is the PLAIN container name; the delete target is
// containerID (or the name when the id is absent). A 404 on remove is logged
// (warn) and treated as success; all other delete errors propagate.
func (d *DockerRunner) destroy(containerName, containerID string, force bool) error {
	if containerID == "" {
		containerID = containerName
	}
	err := dockerlockmanager.RunWithLock(containerName, func(release func()) error {
		defer release()
		clsl.Debug(map[string]any{"containerId": containerID}, "destroying docker container")
		rerr := d.Engine.Remove(containerID, force)
		var api *APIError
		if rerr != nil && errors.As(rerr, &api) && api.StatusCode == 404 {
			clsl.Warn(map[string]any{"containerId": containerID},
				"container not found, continuing")
			return nil
		}
		if rerr != nil {
			clsl.Error(map[string]any{"err": rerr, "containerId": containerID},
				"error destroying container")
		} else {
			clsl.Debug(map[string]any{"containerId": containerID}, "destroyed container")
		}
		return rerr
	})
	if err != nil {
		clsl.Error(map[string]any{"err": err, "containerName": containerName},
			"error destroying container")
	}
	return err
}

// destroyOldContainers ports destroyOldContainers(cb). It lists all
// containers, destroys the ones matching /project-<id>- older than
// MAX_CONTAINER_AGE (subject to the per-project last-access grace), and
// returns only the LIST error (per-destroy errors are logged and swallowed,
// mirroring the Node comment: "some containers get stuck but will be
// destroyed next time").
func (d *DockerRunner) destroyOldContainers() error {
	containers, err := d.Engine.List()
	if err != nil {
		return err
	}
	for _, c := range containers {
		name := c.Name
		if name == "" && len(c.Names) > 0 {
			name = c.Names[0]
		}
		// creation time is returned in seconds
		created := c.Created * 1000
		now := time.Now().UnixMilli()
		age := now - created
		maxAge := int64(d.Cfg.MaxContainerAgeMS)
		if maxAge == 0 {
			maxAge = oneHourMS
		}
		ttl := maxAge - age
		clsl.Debug(map[string]any{"containerName": name, "created": created, "now": now,
			"age": age, "maxAge": maxAge, "ttl": ttl},
			"checking whether to destroy container")
		if !strings.HasPrefix(name, "/project-") || ttl > 0 {
			continue
		}
		// extract projectId: /project-<projectId>-...
		projectId := ""
		if m := projectIDNameRE.FindStringSubmatch(name); m != nil {
			projectId = m[1]
			lastAccess := lastprojectaccess.GetLastProjectAccessTime(projectId)
			// if last access time is missing, lastAccess == 0, container will be
			// destroyed provided that MAX_CONTAINER_AGE is less than 56 years.
			if lastAccess > 0 && lastAccess > now-maxAge {
				clsl.Debug(map[string]any{"projectId": projectId, "lastAccess": lastAccess},
					"container project recently accessed, skipping destroy")
				continue
			}
		}
		// strip the / prefix — the lock manager uses the plain container name.
		plainName := name[1:]
		if derr := d.destroy(plainName, c.Id, false); derr != nil {
			clsl.Error(map[string]any{"err": derr, "containerId": c.Id},
				"error destroying old container")
		}
	}
	return nil
}

// --- singleton monitor (Node module-level, Go package-level) ---

var (
	monitorMu    sync.Mutex
	monitorCancel context.CancelFunc
)

// StartContainerMonitor ports startContainerMonitor(): stop any running
// monitor, then after the given random delay run hourly destroyOldContainers
// ticks until StopContainerMonitor is called. delay is injected (Node
// computes Math.floor(Math.random()*5*60*1000) ms); production uses a 0–5
// minute range.
func (d *DockerRunner) StartContainerMonitor(delay time.Duration) {
	monitorMu.Lock()
	defer monitorMu.Unlock()
	maxAge := int64(d.Cfg.MaxContainerAgeMS)
	if maxAge == 0 {
		maxAge = oneHourMS
	}
	clsl.Debug(map[string]any{"maxAge": maxAge}, "starting container expiry")
	if monitorCancel != nil {
		monitorCancel()
		monitorCancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	monitorCancel = cancel
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if ctx.Err() != nil {
			return
		}
		// Node: the (delayed) setTimeout fires destroyOldContainers once,
		// then setInterval ticks hourly.
		runDestroyTick := func() {
			if err := d.destroyOldContainers(); err != nil {
				clsl.Error(map[string]any{"err": err},
					"failed to destroy old containers")
			}
		}
		runDestroyTick()
		tick := time.NewTicker(time.Duration(oneHourMS) * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				runDestroyTick()
			}
		}
	}()
}

// StopContainerMonitor ports stopContainerMonitor().
func StopContainerMonitor() {
	monitorMu.Lock()
	defer monitorMu.Unlock()
	if monitorCancel != nil {
		monitorCancel()
		monitorCancel = nil
	}
}
