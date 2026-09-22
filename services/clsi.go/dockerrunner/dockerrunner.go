// Package dockerrunner ports services/clsi/app/js/DockerRunner.mjs (634L),
// the mandatory sandboxed Docker compile runner (dockerode-based).
//
// Implements the commandrunner.Runner contract and drives: the Docker
// engine via the Engine SPI (unixengine.go in production, fakes in tests),
// dockerlockmanager (container-name locks), lastprojectaccess and logger.
//
// The DockerRunner is a struct (not the Node object-literal) so it can be
// constructed with an injected Engine for tests.
package dockerrunner

import (
	"sort"
	"strings"
)

// DockerRunner mirrors the Node DockerRunner object-literal. New() builds one
// with a given engine (fakes in tests, the unix engine in production).
type DockerRunner struct {
	// Cfg is the slice of settings this runner reads (see doc divergence 7).
	Cfg *RunnerConfig
	// Engine is the Docker surface (fakes in tests, unix engine in prod).
	Engine Engine
}

// RunnerConfig is the narrow view of settings the Node DockerRunner reads at
// call time (Settings.*). It is a snapshot so tests are deterministic.
type RunnerConfig struct {
	// Docker = settings.clsi.docker.*
	DockerRuntime            string
	DockerUser               string
	DockerEnv                map[string]string
	SeccompProfile           string
	ApparmorProfile          string
	DockerImage              string
	AllowedImages            []string
	MaxContainerAgeMS        int
	// Override = settings.texliveImageNameOveride.
	Override string
	// Path.sandboxedCompilesHostDirCompiles / Output (settings.path.*).
	HostDirCompiles string
	HostDirOutput   string
	// CompileGroupConfig = settings.clsi.docker.compileGroupConfig (the
	// Node deep-merge overrides; see divergence 6).
	CompileGroupConfig map[string]map[string]interface{}
}

// New builds a DockerRunner from the narrow config + engine.
func New(cfg RunnerConfig, eng Engine) *DockerRunner {
	return &DockerRunner{Cfg: &cfg, Engine: eng}
}

// canRunSyncTeXInOutputDir mirrors canRunSyncTeXInOutputDir():
//   Boolean(Settings.path.sandboxedCompilesHostDirOutput)
func (d *DockerRunner) canRunSyncTeXInOutputDir() bool {
	return d.Cfg.HostDirOutput != ""
}

// CanRunSyncTeXInOutputDir is the exported Runner-contract method.
func (d *DockerRunner) CanRunSyncTeXInOutputDir() bool { return d.canRunSyncTeXInOutputDir() }

// imageAllowed mirrors the Settings.clsi.docker.allowedImages guard.
func (d *DockerRunner) imageAllowed(image string) bool {
	if len(d.Cfg.AllowedImages) == 0 {
		return true
	}
	for _, a := range d.Cfg.AllowedImages {
		if a == image {
			return true
		}
	}
	return false
}

// buildOpts ports the _getContainerOptions option build (the part that does
// NOT depend on the per-run volumes map ordering, so it is factored out for
// the fake-driven tests). It returns (CreateOpts, err).
func (d *DockerRunner) buildOpts(projectID string, command []string, directory, image string,
	timeout int64, environment map[string]string, compileGroup string, cwd string) (CreateOpts, error) {

	// Node: command.map(arg => arg.replace('$COMPILE_DIR','/compile')).
	cmd := make([]string, len(command))
	for i, a := range command {
		cmd[i] = strings.Replace(a, "$COMPILE_DIR", "/compile", 1)
	}

	// image == null || image === '' || image === 'undefined' -> default image.
	if image == "" || image == "undefined" {
		image = d.Cfg.DockerImage
	}
	if !d.imageAllowed(image) {
		return CreateOpts{}, errorNotAllowed(image)
	}

	// settings.texliveImageNameOveride != null -> <override>/<basename>.
	if d.Cfg.Override != "" {
		image = d.Cfg.Override + "/" + basenamePosix(image)
	}

	// directory remap (see node comment blocks in run()).
	if compileGroup == "synctex-output" {
		directory = joinPosix(d.Cfg.HostDirOutput, last3segments(directory)...)
	} else {
		directory = joinPosix(d.Cfg.HostDirCompiles, basenamePosix(directory))
	}

	// volumes map (a single entry, keyed by directory, value
	// "/compile:rw" default, "/compile:ro" for the read-only groups).
	// Mirrors Node: volumes = {[directory]: '/compile'} then [:ro] for
	// synctex/synctex-output/wordcount, then _getContainerOptions adds ':rw'
	// when not already suffixed with ':r'.
	vol := "/compile:rw"
	if compileGroup == "synctex" || compileGroup == "synctex-output" || compileGroup == "wordcount" {
		vol = "/compile:ro"
	}

	// Node merge: env = settings.docker.env then caller env (caller wins),
	// in a deterministic order.
	env := make(map[string]string, len(d.Cfg.DockerEnv)+len(environment)+1)
	for k, v := range d.Cfg.DockerEnv {
		env[k] = v
	}
	for k, v := range environment {
		env[k] = v
	}

	// year from the image tag (rolling when no numeric year matched).
	year := "rolling"
	if m := imageYearRE.FindStringSubmatch(image); m != nil {
		y := m[1]
		if y == "" {
			y = m[2]
		}
		if y != "" {
			year = y
		}
	}
	env["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/texlive/" + year + "/bin/x86_64-linux/"

	// Node: Memory = 1024^4.
	memory := int64(1) * 1024 * 1024 * 1024 * 1024
	// Node: timeoutInSeconds = timeout/1000 (float).
	timeoutInSeconds := int64(timeout) / 1000

	opts := CreateOpts{
		Cmd:    cmd,
		Image: image,
		WorkingDir: func() string {
			if cwd != "" {
				return "/compile/" + cwd
			}
			return "/compile"
		}(),
		NetworkDisabled: true,
		Memory:          memory,
		User:            d.Cfg.DockerUser,
		HostConfig: HostConfig{
			Binds: []string{directory + ":" + vol},
			LogConfig: LogConfig{
				Type:     "none",
				Config: map[string]string{},
			},
			Ulimits: []Ulimit{
				{Name: "cpu", Soft: int(timeoutInSeconds + 5), Hard: int(timeoutInSeconds + 10)},
			},
			CapDrop:     []string{"ALL"},
			SecurityOpt: []string{"no-new-privileges"},
		},
	}
	if d.Cfg.SeccompProfile != "" {
		opts.HostConfig.SecurityOpt = append(opts.HostConfig.SecurityOpt, "seccomp="+d.Cfg.SeccompProfile)
	}
	if d.Cfg.ApparmorProfile != "" {
		opts.HostConfig.SecurityOpt = append(opts.HostConfig.SecurityOpt, "apparmor="+d.Cfg.ApparmorProfile)
	}
	if d.Cfg.DockerRuntime != "" {
		opts.HostConfig.Runtime = d.Cfg.DockerRuntime
	}

	// Env: sorted keys (divergence 2).
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	opts.Env = make([]string, 0, len(keys))
	for _, k := range keys {
		opts.Env = append(opts.Env, k+"="+env[k])
	}

	// compileGroupConfig: _.set options <key> with <value> (documented
	// divergence 6 — only dotted paths and scalar values are supported).
	if group, ok := d.Cfg.CompileGroupConfig[compileGroup]; ok {
		applyGroupOverrides(&opts, group)
	}

	fprint := fingerprint(opts)
	opts.Name = "project-" + projectID + "-" + fprint
	return opts, nil
}

// errorNotAllowed mirrors `new Error('image not allowed')`.
func errorNotAllowed(string) *imageNotAllowed { return &imageNotAllowed{msg: "image not allowed"} }

type imageNotAllowed struct{ msg string }

func (e *imageNotAllowed) Error() string { return e.msg }

// applyGroupOverrides ports the _.set deep-merge of the per-group overrides.
// Only the dotted paths used by settings.defaults.cjs are handled (divergence
// 6, documented). Unknown paths are ignored (Go cannot _.set arbitrary paths).
func applyGroupOverrides(opts *CreateOpts, group map[string]interface{}) {
	for key, val := range group {
		switch key {
		case "User":
			if s, ok := val.(string); ok {
				opts.User = s
			}
		case "HostConfig.AutoRemove":
			if b, ok := val.(bool); ok {
				opts.HostConfig.AutoRemove = b
			}
		// dotted paths that would require mutating nested HostConfig fields
		// are out of scope for CE config; see divergence 6.
		}
	}
}

// basenamePosix / last3segments / joinPosix mirror the node:Path calls used in
// run() (posix, always — Node runs linux).
func basenamePosix(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

func last3segments(p string) []string {
	// split on '/' and take the last 3 (drop empties? Node keeps the split
	// result; trailing empties are dropped by node path.join on join, so we
	// drop empty trailing segment).
	parts := strings.Split(p, "/")
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	return parts
}

func joinPosix(base string, segs ...string) string {
	sep := "/"
	out := base
	for _, s := range segs {
		if s == "" {
			continue
		}
		if strings.HasSuffix(out, sep) {
			out += s
		} else {
			out += sep + s
		}
	}
	return out
}
