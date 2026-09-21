package settings

import (
	"fmt"
	"testing"
)

// fakeFS: the injected Filesystem seam (fs.existsSync stand-in).
type fakeFS map[string]bool

func (f fakeFS) PathExists(p string) bool { return f[p] }

// readModules: the injected ReadModule seam (requireFromFile stand-in).
// module values may be map[string]any (plain object module) or a type
// implementing moduleWithMerge (web's lazy/stamp settings.defaults analog).
func mkRead(mods map[string]map[string]any) func(path string) (any, error) {
	return func(path string) (any, error) {
		m, ok := mods[path]
		if !ok {
			return nil, fmt.Errorf("no module for %s", path)
		}
		return m, nil
	}
}

// mergeWithModule mirrors web settings.defaults.js:1665 (lazy/stamp-based
// module export): the defaults module itself carries MergeWith, so the R11
// dispatch routes through it instead of the plain merge.
type mergeWithModule struct{ base map[string]any }

func (m mergeWithModule) MergeWith(overrides any) any {
	out, err := Merge(overrides.(map[string]any), m.base)
	if err != nil {
		panic(err)
	}
	return out
}

func envLookup(kv map[string]string) EnvLookup {
	return func(name string) string { return kv[name] }
}

// TestLoadEnvMismatch: SHARELATEX_CONFIG set and != OVERLEAF_CONFIG (or unset)
// -> Node's top-level throw, before any fs touch. Oracle: Settings.js:17-20.
func TestLoadEnvMismatch(t *testing.T) {
	_, err := Load(&LoadOpts{
		Env:        envLookup(map[string]string{"SHARELATEX_CONFIG": "/sl/c.js"}),
		FS:         fakeFS{},
		ReadModule: mkRead(nil),
	})
	if err == nil {
		t.Fatal("want mismatch error")
	}
	if got := err.Error(); got != "found mismatching SHARELATEX_CONFIG, rename to OVERLEAF_CONFIG" {
		t.Fatalf("err = %q, want the mismatch error", got)
	}
	// equal values -> no throw (OVERLEAF_CONFIG falls back to SHARELATEX_CONFIG)
	res, err := Load(&LoadOpts{
		Env:        envLookup(map[string]string{"SHARELATEX_CONFIG": "/x.js", "OVERLEAF_CONFIG": "/x.js"}),
		FS:         fakeFS{"/x.js": true},
		ReadModule: mkRead(map[string]map[string]any{"/x.js": {"a": 1.0}}),
	})
	if err != nil {
		t.Fatalf("equal envs: %v", err)
	}
	if res.Value["a"] != 1.0 {
		t.Fatalf("equal envs: value = %v", res.Value)
	}
}

// TestLoadDefaultsResolution drives the real Load path (no parallel helper)
// and pins Settings.js's defaultsPath resolution order: cwd .cjs, cwd .js,
// entry-dir .cjs, entry-dir .js, else flying-blind {}.
func TestLoadDefaultsResolution(t *testing.T) {
	cases := []struct {
		name       string
		fs         fakeFS
		cwd, entry string
		wantPath   string
	}{
		{"cwd-cjs-first",
			fakeFS{"/c/config/settings.defaults.cjs": true, "/e/config/settings.defaults.js": true},
			"/c", "/e", "/c/config/settings.defaults.cjs"},
		{"cwd-js-second",
			fakeFS{"/c/config/settings.defaults.js": true, "/e/config/settings.defaults.cjs": true},
			"/c", "/e", "/c/config/settings.defaults.js"},
		{"entry-cjs-when-cwd-absent",
			fakeFS{"/e/config/settings.defaults.cjs": true},
			"/c", "/e", "/e/config/settings.defaults.cjs"},
		{"entry-js-when-cwd-absent",
			fakeFS{"/e/config/settings.defaults.js": true},
			"/c", "/e", "/e/config/settings.defaults.js"},
		{"entry-skipped-when-empty",
			fakeFS{"/e/config/settings.defaults.cjs": true},
			"/c", "", ""},
	}
	for _, c := range cases {
		res, err := Load(&LoadOpts{
			FS: c.fs,
			ReadModule: mkRead(map[string]map[string]any{
				c.wantPath: {"from": "defaults"},
			}),
			CWD:           c.cwd,
			EntryPointDir: c.entry,
		})
		if err != nil {
			t.Errorf("%s: err = %v", c.name, err)
			continue
		}
		if res.DefaultsPath != c.wantPath {
			t.Errorf("%s: DefaultsPath = %q, want %q", c.name, res.DefaultsPath, c.wantPath)
		}
		if c.wantPath != "" && res.Value["from"] != "defaults" {
			t.Errorf("%s: value = %v, want defaults module", c.name, res.Value)
		}
	}
}

func TestLoadFlyingBlind(t *testing.T) {
	res, err := Load(&LoadOpts{
		FS:            fakeFS{},
		ReadModule:    mkRead(nil),
		CWD:           "/c",
		EntryPointDir: "/e",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !res.FlyingBlind || res.DefaultsPath != "" || res.OverridesPath != "" || len(res.Value) != 0 {
		t.Fatalf("flying-blind result = %+v", res)
	}
}

func TestLoadOverridesResolution(t *testing.T) {
	// env OVERLEAF_CONFIG beats NODE_ENV fallback files
	res, err := Load(&LoadOpts{
		FS: fakeFS{"/env/c.js": true, "/c/config/settings.test.cjs": true},
		ReadModule: func(path string) (any, error) {
			switch path {
			case "/env/c.js":
				return map[string]any{"ovr": "env"}, nil
			case "/c/config/settings.test.cjs":
				return map[string]any{"ovr": "file"}, nil
			}
			return nil, fmt.Errorf("unexpected path %v", path)
		},
		CWD:           "/c",
		EntryPointDir: "/e",
		Env: envLookup(map[string]string{
			"OVERLEAF_CONFIG": "/env/c.js",
			"NODE_ENV":        "TEST", // lowercasing arm
		}),
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if res.Value["ovr"] != "env" || res.OverridesPath != "/env/c.js" {
		t.Fatalf("env-override: value=%v path=%q", res.Value, res.OverridesPath)
	}
	// NODE_ENV file fallback when env var empty
	res2, err := Load(&LoadOpts{
		FS: fakeFS{"/c/config/settings.test.cjs": true},
		ReadModule: func(path string) (any, error) {
			if path == "/c/config/settings.test.cjs" {
				return map[string]any{"ovr": "file"}, nil
			}
			return nil, fmt.Errorf("unexpected path %v", path)
		},
		CWD:           "/c",
		EntryPointDir: "/e",
		Env:           envLookup(map[string]string{"NODE_ENV": "TEST"}),
	})
	if err != nil || res2.Value["ovr"] != "file" {
		t.Fatalf("file-override: value=%v err=%v", res2.Value, err)
	}
	// NODE_ENV default "development"
	res3, err := Load(&LoadOpts{
		FS: fakeFS{"/c/config/settings.development.js": true},
		ReadModule: func(path string) (any, error) {
			if path == "/c/config/settings.development.js" {
				return map[string]any{"ovr": "dev"}, nil
			}
			return nil, fmt.Errorf("unexpected path %v", path)
		},
		CWD:           "/c",
		EntryPointDir: "/e",
		Env:           envLookup(map[string]string{}),
	})
	if err != nil || res3.Value["ovr"] != "dev" {
		t.Fatalf("dev-override: value=%v err=%v", res3.Value, err)
	}
}

// mergeWithBad is a defaults-module seam returning a non-map (Go analog of a
// buggy web module) — Load must reject it with the typed error arm.
type mergeWithBad struct{ mergeWithModule }

func (m mergeWithBad) MergeWith(any) any { return "not-a-map" }

func TestLoadR11MergeWithDispatch(t *testing.T) {
	// defaultsPath + overridesPath, defaults module implements mergeWith
	// -> dispatched through it (not plain merge)
	res, err := Load(&LoadOpts{
		FS: fakeFS{"/c/config/settings.defaults.cjs": true, "/env/c.js": true},
		ReadModule: func(path string) (any, error) {
			switch path {
			case "/c/config/settings.defaults.cjs":
				return mergeWithModule{base: map[string]any{"base": "d"}}, nil
			case "/env/c.js":
				return map[string]any{"added": "o"}, nil
			}
			return nil, fmt.Errorf("unexpected path %v", path)
		},
		CWD:           "/c",
		EntryPointDir: "/e",
		Env:           envLookup(map[string]string{"OVERLEAF_CONFIG": "/env/c.js"}),
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if v := res.Value; v["base"] != "d" || v["added"] != "o" {
		t.Fatalf("R11 mergeWith: value = %v", v)
	}
	if res.DefaultsPath == "" || res.OverridesPath == "" {
		t.Fatalf("paths: %+v", res)
	}
}

func TestLoadMergeWithNonMapRejects(t *testing.T) {
	_, err := Load(&LoadOpts{
		FS: fakeFS{"/c/config/settings.defaults.cjs": true, "/env/c.js": true},
		ReadModule: func(path string) (any, error) {
			switch path {
			case "/c/config/settings.defaults.cjs":
				return mergeWithBad{mergeWithModule{base: map[string]any{"base": "d"}}}, nil
			case "/env/c.js":
				return map[string]any{"added": "o"}, nil
			}
			return nil, fmt.Errorf("unexpected path %v", path)
		},
		CWD:           "/c",
		EntryPointDir: "/e",
		Env:           envLookup(map[string]string{"OVERLEAF_CONFIG": "/env/c.js"}),
	})
	if err == nil {
		t.Fatal("want mergeWith non-map rejection")
	}
}

func TestLoadReadModuleError(t *testing.T) {
	// defaults file present on FS but ReadModule errors -> propagated
	_, err := Load(&LoadOpts{
		FS:         fakeFS{"/c/config/settings.defaults.cjs": true},
		ReadModule: func(string) (any, error) { return nil, fmt.Errorf("boom") },
		CWD:        "/c",
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
	// env set but no module registered for it
	_, err = Load(&LoadOpts{
		FS: fakeFS{"/env/c.js": true},
		ReadModule: func(path string) (any, error) {
			return nil, fmt.Errorf("no module %v", path)
		},
		CWD: "/c", EntryPointDir: "/e",
		Env: envLookup(map[string]string{"OVERLEAF_CONFIG": "/env/c.js"}),
	})
	if err == nil {
		t.Fatal("want override module error")
	}
}

// toMap: ReadModule returning a non-map value (Go analog of a CJS module
// importing to a non-object) becomes an empty settings-shape map.
func TestLoadModuleNonMap(t *testing.T) {
	// plain arm: module value not a map -> toMap fallback {}
	res, err := Load(&LoadOpts{
		FS:         fakeFS{"/c/config/settings.defaults.cjs": true},
		ReadModule: func(string) (any, error) { return "not-map", nil },
		CWD:        "/c",
	})
	if err != nil || len(res.Value) != 0 {
		t.Fatalf("toMap fallback: value=%v err=%v", res.Value, err)
	}
}
