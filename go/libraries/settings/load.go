package settings

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Result is the outcome of Load, mirroring the plain-object module export of
// Settings.js plus the paths Node logs ("Using default settings from X" /
// "Using settings from Y").
type Result struct {
	Value         map[string]any // the exported settings object
	DefaultsPath  string
	OverridesPath string
	FlyingBlind   bool // true: neither defaults nor overrides found
}

// EnvLookup abstracts process.env so Load is testable (inject a map or stub).
type EnvLookup func(name string) string

// Filesystem abstracts fs.existsSync for the config-File-discovery walk.
type Filesystem interface {
	PathExists(path string) bool
}

// Load mirrors Node Settings.js as it runs once per process at require():
//
//  1. env: OVERLEAF_CONFIG = env OVERLEAF_CONFIG || env SHARELATEX_CONFIG; if
//     both set and unequal -> error "found mismatching SHARELATEX_CONFIG,
//     rename to OVERLEAF_CONFIG" (Node throws at module top level, before any
//     file is touched).
//  2. NODE_ENV = env NODE_ENV || "development", lowercased (only used to
//     build the config/settings.<NODE_ENV>.{cjs,js} fallback path).
//  3. defaultsPath = first existing among
//     <CWD>/config/settings.defaults.cjs, <CWD>/config/settings.defaults.js,
//     <EntryDir>/config/settings.defaults.cjs, <EntryDir>/config/settings.defaults.js
//     (EntryDir omitted when empty, mirroring `process.argv[1]` absent).
//  4. overridesPath = first existing among
//     OVERLEAF_CONFIG, <CWD>/config/settings.<NODE_ENV>.cjs,
//     <CWD>/config/settings.<NODE_ENV>.js
//     (OVERLEAF_CONFIG only when env OVERLEAF_CONFIG is non-empty —
//     `pathIfExists("")` is falsy in Node).
//  5. neither -> "No settings or defaults found. I'm flying blind." -> {}.
//  6. dispatch (R11): typeof settings.mergeWith === 'function' ?
//     settings.mergeWith(overrides) : merge(overrides, settings).
func Load(opts *LoadOpts) (*Result, error) {
	env := opts.Env
	if env == nil {
		env = func(string) string { return "" }
	}
	overleaf := env("OVERLEAF_CONFIG")
	sharelatex := env("SHARELATEX_CONFIG")
	if sharelatex != "" && sharelatex != overleaf {
		return nil, fmt.Errorf("found mismatching SHARELATEX_CONFIG, rename to OVERLEAF_CONFIG")
	}

	nodeEnv := env("NODE_ENV")
	if nodeEnv == "" {
		nodeEnv = "development"
	}
	nodeEnv = strings.ToLower(nodeEnv)

	pathExists := func(path string) bool {
		return path != "" && opts.FS.PathExists(path)
	}

	// defaults resolution (Settings.js lines 31-35)
	var defaultsPath string
	for _, cand := range []string{
		filepath.Join(opts.CWD, "config", "settings.defaults.cjs"),
		filepath.Join(opts.CWD, "config", "settings.defaults.js"),
		filepath.Join(opts.EntryPointDir, "config", "settings.defaults.cjs"),
		filepath.Join(opts.EntryPointDir, "config", "settings.defaults.js"),
	} {
		if opts.EntryPointDir == "" && (cand == filepath.Join(opts.EntryPointDir, "config", "settings.defaults.cjs") || cand == filepath.Join(opts.EntryPointDir, "config", "settings.defaults.js")) {
			continue
		}
		if pathExists(cand) {
			defaultsPath = cand
			break
		}
	}

	// overrides resolution (Settings.js lines 37-41)
	var overridesPath string
	if overleaf != "" {
		if pathExists(overleaf) {
			overridesPath = overleaf
		}
	}
	if overridesPath == "" {
		for _, cand := range []string{
			filepath.Join(opts.CWD, "config", "settings."+nodeEnv+".cjs"),
			filepath.Join(opts.CWD, "config", "settings."+nodeEnv+".js"),
		} {
			if pathExists(cand) {
				overridesPath = cand
				break
			}
		}
	}

	res := &Result{}
	var defaultsModule any

	if defaultsPath != "" {
		mod, err := opts.ReadModule(defaultsPath)
		if err != nil {
			return nil, err
		}
		defaultsModule = mod
		res.Value = toMap(mod)
		res.DefaultsPath = defaultsPath
	} else {
		res.Value = map[string]any{}
	}

	if overridesPath != "" {
		ov, err := opts.ReadModule(overridesPath)
		if err != nil {
			return nil, err
		}
		res.OverridesPath = overridesPath

		// R11 dispatch: the check is on the DEFAULTS module (`settings` in
		// Settings.js; web's lazy/stamp settings.defaults.js:1665 exports
		// `.mergeWith`). Plain-object modules take the plain
		// `merge(overridesModule, settings)` arm.
		if mw, ok := defaultsModule.(moduleWithMerge); ok {
			out, ok := mw.MergeWith(ov).(map[string]any)
			if !ok {
				return nil, fmt.Errorf("settings: mergeWith returned %T, want map[string]any", defaultsModule)
			}
			res.Value = out
		} else {
			out, err := Merge(toMap(ov), res.Value)
			if err != nil {
				return nil, err
			}
			res.Value = out
		}
	}

	res.FlyingBlind = defaultsPath == "" && overridesPath == ""
	return res, nil
}

// LoadOpts carries the injected seams so Load is the pure part of Settings.js.
type LoadOpts struct {
	Env EnvLookup
	FS  Filesystem
	// ReadModule maps an absolute config path to its exported module value. In
	// Node this is requireFromFile(absPath) (PnP-anchored require of a JS /
	// CJS module); in Go a config file is produced by the consuming Go module,
	// so the seam is a Go function or a deserialized module value.
	ReadModule func(path string) (any, error)
	// CWD mirrors process.cwd().
	CWD string
	// EntryPointDir mirrors Path.dirname(process.argv[1]) ("" when Node was
	// run without argv[1], e.g. REPL).
	EntryPointDir string
}

// moduleWithMerge mirrors the R11 dispatch in Settings.js:
//
//	typeof settings.mergeWith === 'function' ? settings.mergeWith(overrides) : merge(overrides, settings)
//
// A Go module value that implements MergeWith(any) any is folded through
// that method instead of the plain Merge (web's lazy/stamp-based defaults module, settings.defaults.js:1665).
type moduleWithMerge interface {
	MergeWith(overrides any) any
}

// toMap converts a ReadModule result to map[string]any for the plain (not
// mergeWith) dispatch arm, matching the module being a plain object
// export in Go. Non-map module values are not settings-shaped.
func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
