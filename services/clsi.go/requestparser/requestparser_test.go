package requestparser

// Test port of services/clsi/test/unit/js/RequestParser.test.js (vitest).
// Every Node case is mirrored 1:1: exact error strings, exact defaults,
// exact timeout cap + ms conversion, exact resource validation order.
// Additional subtests cover the remaining branches of Parse for coverage.

import (
	"math"
	"testing"
	"time"
)

func TestTopLevel(t *testing.T) {
	// Node: parse([], callback) — "without a top level object".
	if _, err := Parse(map[string]interface{}{}, Config{}); err == nil ||
		err.Error() != "top level object should have a compile attribute" {
		t.Fatalf("empty body: want compile attribute error, got %v", err)
	}
	// compile: null.
	if _, err := Parse(map[string]interface{}{"compile": nil}, Config{}); err == nil ||
		err.Error() != "top level object should have a compile attribute" {
		t.Fatalf("null compile: want compile attribute error, got %v", err)
	}
	// compile: wrong type (string).
	if _, err := Parse(map[string]interface{}{"compile": "nope"}, Config{}); err == nil ||
		err.Error() != "top level object should have a compile attribute" {
		t.Fatalf("string compile: want compile attribute error, got %v", err)
	}
}

func TestCompilerInvalid(t *testing.T) {
	body := map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"compiler": "not-a-compiler"},
		},
	}
	_, err := Parse(body, Config{})
	if err == nil || err.Error() != "compiler attribute should be one of: pdflatex, latex, xelatex, lualatex" {
		t.Fatalf("want one-of compiler error, got %v", err)
	}
}

func TestCompilerDefaultAndSet(t *testing.T) {
	// default pdflatex
	got, err := Parse(map[string]interface{}{"compile": map[string]interface{}{}}, Config{})
	if err != nil {
		t.Fatalf("default compile: %v", err)
	}
	if got.Compiler != "pdflatex" {
		t.Fatalf("default compiler = %q, want pdflatex", got.Compiler)
	}
	if got.MetricsOpts.Compile != "initial" || got.MetricsOpts.Path != "" || got.MetricsOpts.Method != "" {
		t.Fatalf("metricsOpts wrong: %+v", got.MetricsOpts)
	}
	// explicit
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"compiler": "lualatex"},
		},
	}, Config{})
	if err != nil || got.Compiler != "lualatex" {
		t.Fatalf("lualatex: got %q, %v", got.Compiler, err)
	}
}

func TestImageNameUnrestricted(t *testing.T) {
	// settings.clsi.docker.allowedImages not set -> plain string attribute.
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{
				"imageName": "basicImageName/here:2017-1",
			},
		},
	}, Config{})
	if err != nil || got.ImageName != "basicImageName/here:2017-1" {
		t.Fatalf("imageName: %v %v", got.ImageName, err)
	}
	// wrong type -> declared-type message
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"imageName": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "imageName attribute should be a string" {
		t.Fatalf("want setImageName error, got %v", err)
	}
}

func TestImageNameRestricted(t *testing.T) {
	cfg := Config{AllowedImages: []string{"repo/name:tag1", "repo/name:tag2"}}
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"imageName": "something/different:latest"},
		},
	}, cfg); err == nil ||
		!contains(err.Error(), "imageName attribute should be one of") {
		t.Fatalf("want one-of imageName error, got %v", err)
	}
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"imageName": "repo/name:tag1"},
		},
	}, cfg)
	if err != nil || got.ImageName != "repo/name:tag1" {
		t.Fatalf("valid imageName: %v %v", got.ImageName, err)
	}
	// non-string value also hits one-of (indexOf -1 first, per JS).
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"imageName": 42},
		},
	}, cfg); err == nil ||
		err.Error() != "imageName attribute should be one of: repo/name:tag1, repo/name:tag2" {
		t.Fatalf("want numeric imageName one-of, got %v", err)
	}
}

func TestFlags(t *testing.T) {
	// flags = ['-file-line-error'] (JS typeof [] === 'object').
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{
				"flags": []interface{}{"-file-line-error"},
			},
		},
	}, Config{})
	if err != nil {
		t.Fatalf("flags: %v", err)
	}
	flags, ok := got.Flags.([]interface{})
	if !ok || len(flags) != 1 || flags[0] != "-file-line-error" {
		t.Fatalf("flags = %v", got.Flags)
	}
	// absent -> default []
	got, err = Parse(map[string]interface{}{"compile": map[string]interface{}{}}, Config{})
	if err != nil {
		t.Fatalf("no flags: %v", err)
	}
	fl, ok := got.Flags.([]interface{})
	if !ok || len(fl) != 0 {
		t.Fatalf("default flags = %v, want empty slice", got.Flags)
	}
	// explicit null -> default [] (JS: null == undefined-ish, typeof check skipped)
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"flags": nil},
		},
	}, Config{})
	if err != nil {
		t.Fatalf("null flags: %v", err)
	}
	fl, ok = got.Flags.([]interface{})
	if !ok || len(fl) != 0 {
		t.Fatalf("null flags = %v, want empty slice", got.Flags)
	}
	// wrong type: number
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"flags": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "flags attribute should be an object" {
		t.Fatalf("want flags object error, got %v", err)
	}
	// object value accepted (typeof {} === 'object')
	_, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"flags": map[string]interface{}{}},
		},
	}, Config{})
	if err != nil {
		t.Fatalf("object flags accepted: %v", err)
	}
}

func TestEnableCheckpoint(t *testing.T) {
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"enableCheckpoint": true},
		},
	}, Config{})
	if err != nil || got.EnableCheckpoint != true {
		t.Fatalf("enableCheckpoint: %v %v", got.EnableCheckpoint, err)
	}
	got, err = Parse(map[string]interface{}{"compile": map[string]interface{}{}}, Config{})
	if err != nil || got.EnableCheckpoint != false {
		t.Fatalf("default enableCheckpoint: %v", err)
	}
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"enableCheckpoint": "true"},
		},
	}, Config{}); err == nil ||
		err.Error() != "enableCheckpoint attribute should be a boolean" {
		t.Fatalf("want boolean error, got %v", err)
	}
}

func TestTimeout(t *testing.T) {
	// absent -> MAX_TIMEOUT ms
	got, err := Parse(map[string]interface{}{"compile": map[string]interface{}{}}, Config{})
	if err != nil || got.Timeout != 600*1000 {
		t.Fatalf("default timeout = %d, want %d", got.Timeout, 600*1000)
	}
	// larger than cap -> capped (Node: compare in SECONDS, then *1000)
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"timeout": 601.0},
		},
	}, Config{})
	if err != nil || got.Timeout != 600*1000 {
		t.Fatalf("capped timeout = %d", got.Timeout)
	}
	// exact value -> ms
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"timeout": 42.0},
		},
	}, Config{})
	if err != nil || got.Timeout != 42*1000 {
		t.Fatalf("timeout 42s -> %d ms", got.Timeout)
	}
	// non-number
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"timeout": "42"},
		},
	}, Config{}); err == nil ||
		err.Error() != "timeout attribute should be a number" {
		t.Fatalf("want timeout number error, got %v", err)
	}
}

func TestResources(t *testing.T) {
	pathErr := "all resources should have a path attribute"
	// resource missing path
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":   map[string]interface{}{},
			"resources": []interface{}{map[string]interface{}{"content": "x"}},
		},
	}, Config{}); err == nil || err.Error() != pathErr {
		t.Fatalf("want path error, got %v", err)
	}
	// resource path non-string (number)
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":   map[string]interface{}{},
			"resources": []interface{}{map[string]interface{}{"path": 5, "url": "u"}},
		},
	}, Config{}); err == nil || err.Error() != pathErr {
		t.Fatalf("want non-string path error, got %v", err)
	}
	// resource not an object
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":   map[string]interface{}{},
			"resources": []interface{}{"not-an-object"},
		},
	}, Config{}); err == nil || err.Error() != pathErr {
		t.Fatalf("want resource object error, got %v", err)
	}
	// no url or content
	urlOrContentErr := "all resources should have either a url or content attribute"
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":   map[string]interface{}{},
			"resources": []interface{}{map[string]interface{}{"path": "a.tex"}},
		},
	}, Config{}); err == nil || err.Error() != urlOrContentErr {
		t.Fatalf("want url/content error, got %v", err)
	}
	// content non-string
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":   map[string]interface{}{},
			"resources": []interface{}{map[string]interface{}{"path": "a.tex", "content": []interface{}{}}},
		},
	}, Config{}); err == nil || err.Error() != "content attribute should be a string" {
		t.Fatalf("want content string error, got %v", err)
	}
	// url non-string
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":   map[string]interface{}{},
			"resources": []interface{}{map[string]interface{}{"path": "a.tex", "url": []interface{}{}}},
		},
	}, Config{}); err == nil || err.Error() != "url attribute should be a string" {
		t.Fatalf("want url string error, got %v", err)
	}
	// fallbackURL non-string
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":   map[string]interface{}{},
			"resources": []interface{}{map[string]interface{}{"path": "a.tex", "url": "u", "fallbackURL": 5}},
		},
	}, Config{}); err == nil ||
		err.Error() != "fallbackURL attribute should be a string" {
		t.Fatalf("want fallbackURL string error, got %v", err)
	}
	// Pin Berlin (the oracle's TZ) for the local-date assertion below; NOT
	// parallel-safe, which matches the module's single-writer test design.
	prevLocal := time.Local
	loc, locErr := time.LoadLocation("Europe/Berlin")
	if locErr != nil {
		t.Fatalf("Europe/Berlin: %v", locErr)
	}
	time.Local = loc

	// valid resource: date '12:00 01/02/03' == 1041505200000 (Berlin).
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{},
			"resources": []interface{}{
				map[string]interface{}{
					"path":     "test.tex",
					"modified": "12:00 01/02/03",
					"url":      "www.example.com",
					"content":  "Hello world",
				},
			},
		},
	}, Config{})
	if err != nil {
		t.Fatalf("valid resource: %v", err)
	}
	if len(got.Resources) != 1 {
		t.Fatalf("want 1 resource, got %d", len(got.Resources))
	}
	r := got.Resources[0]
	if r.Path != "test.tex" || r.URL != "www.example.com" || r.Content != "Hello world" {
		t.Fatalf("resource fields: %+v", r)
	}
	if mod, ok := r.Modified.(float64); !ok || mod != 1041505200000 {
		t.Fatalf("resource modified = %v, want 1041505200000 epoch ms", r.Modified)
	}
	time.Local = prevLocal
	// resources: non-array
	if err := func() (e error) {
		_, e = Parse(map[string]interface{}{
			"compile": map[string]interface{}{
				"options":   map[string]interface{}{},
				"resources": "nope",
			},
		}, Config{})
		return e
	}(); err == nil ||
		err.Error() != "compile.resources.map is not a function" {
		t.Fatalf("want map error, got %v", err)
	}
	// bad modified date
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{},
			"resources": []interface{}{
				map[string]interface{}{"path": "a.tex", "content": "x", "modified": "not-a-date"},
			},
		},
	}, Config{}); err == nil ||
		err.Error() != "resource modified date could not be understood: not-a-date" {
		t.Fatalf("want modified date error, got %v", err)
	}
	// numeric modified (a plain number is a valid date in V8: new Date(0) === epoch 0)
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{},
			"resources": []interface{}{
				map[string]interface{}{"path": "a.tex", "content": "x", "modified": 1700000000000.0},
			},
		},
	}, Config{})
	if err != nil {
		t.Fatalf("numeric modified: %v", err)
	}
	if got.Resources[0].Modified != 1700000000000.0 {
		t.Fatalf("numeric modified = %v", got.Resources[0].Modified)
	}
}

func TestRootResourcePath(t *testing.T) {
	def := "main.tex"
	got, err := Parse(map[string]interface{}{"compile": map[string]interface{}{"options": map[string]interface{}{}}}, Config{})
	if err != nil {
		t.Fatalf("default rootResourcePath: %v", err)
	}
	if got.RootResourcePath != def {
		t.Fatalf("rootResourcePath = %q, want %q", got.RootResourcePath, def)
	}
	got, err = Parse(map[string]interface{}{"compile": map[string]interface{}{"options": map[string]interface{}{}, "rootResourcePath": "test.tex"}}, Config{})
	if err != nil || got.RootResourcePath != "test.tex" {
		t.Fatalf("rootResourcePath = %q, %v", got.RootResourcePath, err)
	}
	// non-string type
	if _, err := Parse(map[string]interface{}{"compile": map[string]interface{}{"options": map[string]interface{}{}, "rootResourcePath": []interface{}{}}}, Config{}); err == nil ||
		err.Error() != "rootResourcePath attribute should be a string" {
		t.Fatalf("want string error, got %v", err)
	}
	// relative path (escaped and unescaped forms, per Node test)
	if _, err := Parse(map[string]interface{}{"compile": map[string]interface{}{"options": map[string]interface{}{}, "rootResourcePath": "foo/../../bar.tex"}}, Config{}); err == nil ||
		err.Error() != "relative path in root resource" {
		t.Fatalf("want relative path error, got %v", err)
	}
	if _, err := Parse(map[string]interface{}{"compile": map[string]interface{}{"options": map[string]interface{}{}, "rootResourcePath": "foo/../bar.tex"}}, Config{}); err == nil ||
		err.Error() != "relative path in root resource" {
		t.Fatalf("want unescaped relative path error, got %v", err)
	}
}

func TestSyncType(t *testing.T) {
	// unknown value
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"syncType": "unexpected"},
		},
	}, Config{}); err == nil ||
		err.Error() != "syncType attribute should be one of: full, incremental, history-full, history-incremental" {
		t.Fatalf("want syncType error, got %v", err)
	}
	// valid
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"syncType": "full"},
		},
	}, Config{})
	if err != nil || got.SyncType != "full" {
		t.Fatalf("syncType = %v, %v", got.SyncType, err)
	}
	// non-string
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"syncType": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "syncType attribute should be one of: full, incremental, history-full, history-incremental" {
		t.Fatalf("want numeric syncType error, got %v", err)
	}
	// syncState
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"syncState": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "syncState attribute should be a string" {
		t.Fatalf("want syncState string error, got %v", err)
	}
	// syncState valid
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"syncState": "state-1"},
		},
	}, Config{})
	if err != nil || got.SyncState != "state-1" {
		t.Fatalf("syncState = %v, %v", got.SyncState, err)
	}
}

func TestHistoryID(t *testing.T) {
	valid := "0123456789abcdef01234567" // 24 hex chars
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"historyId": valid},
		},
	}, Config{})
	if err != nil || got.HistoryID != valid {
		t.Fatalf("historyId = %v, %v", got.HistoryID, err)
	}
	// numeric alternate
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"historyId": "12345"},
		},
	}, Config{})
	if err != nil || got.HistoryID != "12345" {
		t.Fatalf("historyId numeric = %v, %v", got.HistoryID, err)
	}
	// bad (too short for alternates, not a mongo id either)
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"historyId": "00"},
		},
	}, Config{}); err == nil ||
		err.Error() != "historyId attribute does not match regex /^([0-9a-f]{24}|[1-9][0-9]{0,9})$/" {
		t.Fatalf("want historyId regex error, got %v", err)
	}
	// non-string
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"historyId": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "historyId attribute should be a string" {
		t.Fatalf("want historyId string error, got %v", err)
	}
}

func TestEditorID(t *testing.T) {
	// 36-char UUID
	valid := "a0792476-0b84-426a-b873-3757823f0e42"
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"editorId": valid},
		},
	}, Config{})
	if err != nil || got.EditorID != valid {
		t.Fatalf("editorId = %v, %v", got.EditorID, err)
	}
	// bad: wrong length
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"editorId": "short"},
		},
	}, Config{}); err == nil ||
		err.Error() != "editorId attribute does not match regex /^[a-f0-9-]{36}$/" {
		t.Fatalf("want editorId regex error, got %v", err)
	}
	// bad: uppercase (not in [a-f0-9-])
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"editorId": "A0792476-0B84-426A-B873-3757823F0E42"},
		},
	}, Config{}); err == nil ||
		err.Error() != "editorId attribute does not match regex /^[a-f0-9-]{36}$/" {
		t.Fatalf("want uppercase reject, got %v", err)
	}
	// non-string
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"editorId": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "editorId attribute should be a string" {
		t.Fatalf("want editorId string error, got %v", err)
	}
}

func TestBuildID(t *testing.T) {
	valid := "195a4869176-a4ad60bee7bf35e4"
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"buildId": valid},
		},
	}, Config{})
	if err != nil || got.BuildID != valid {
		t.Fatalf("buildId = %v, %v", got.BuildID, err)
	}
	// bad (slash, not a-hex chars)
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"buildId": "foo/bar"},
		},
	}, Config{}); err == nil ||
		err.Error() != "buildId attribute does not match regex /^[0-9a-f]+-[0-9a-f]+$/" {
		t.Fatalf("want buildId error, got %v", err)
	}
	// wrong type
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"buildId": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "buildId attribute should be a string" {
		t.Fatalf("want buildId string error, got %v", err)
	}
}

func TestBaseHistoryVersionGlobalBlobs(t *testing.T) {
	// valid baseHistoryVersion
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":            map[string]interface{}{},
			"baseHistoryVersion": 42.0,
		},
	}, Config{})
	if err != nil || got.BaseHistoryVersion != 42.0 {
		t.Fatalf("baseHistoryVersion = %v, %v", got.BaseHistoryVersion, err)
	}
	// non-number
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":            map[string]interface{}{},
			"baseHistoryVersion": "42",
		},
	}, Config{}); err == nil ||
		err.Error() != "baseHistoryVersion attribute should be a number" {
		t.Fatalf("want baseHistoryVersion number error, got %v", err)
	}
	// globalBlobs
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":     map[string]interface{}{},
			"globalBlobs": []interface{}{"a", "b"},
		},
	}, Config{})
	if err != nil ||
		!isArr(got.GlobalBlobs) || len(got.GlobalBlobs.([]interface{})) != 2 {
		t.Fatalf("globalBlobs = %v, %v", got.GlobalBlobs, err)
	}
	// non-array
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":     map[string]interface{}{},
			"globalBlobs": "nope",
		},
	}, Config{}); err == nil ||
		err.Error() != "globalBlobs attribute should be an array" {
		t.Fatalf("want globalBlobs array error, got %v", err)
	}
}

func TestFilestoreBlobPrefix(t *testing.T) {
	// valid prefix
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":             map[string]interface{}{},
			"filestoreBlobPrefix": "prefix/dir",
		},
	}, Config{})
	if err != nil || got.FilestoreBlobPrefix != "prefix/dir" {
		t.Fatalf("filestoreBlobPrefix = %v, %v", got.FilestoreBlobPrefix, err)
	}
	// relative path -> "relative path in root resource"
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":             map[string]interface{}{},
			"filestoreBlobPrefix": "a/../b",
		},
	}, Config{}); err == nil ||
		err.Error() != "relative path in root resource" {
		t.Fatalf("want relative error, got %v", err)
	}
	// object value -> "compile.filestoreBlobPrefix.split is not a function"
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":             map[string]interface{}{},
			"filestoreBlobPrefix": map[string]interface{}{"x": 1},
		},
	}, Config{}); err == nil ||
		err.Error() != "compile.filestoreBlobPrefix.split is not a function" {
		t.Fatalf("want split error, got %v", err)
	}
}

func TestClSIPerfVariant(t *testing.T) {
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"clsiPerfVariant": "variant-1"},
		},
	}, Config{})
	if err != nil || got.ClSIPerfVariant != "variant-1" {
		t.Fatalf("clsiPerfVariant = %v, %v", got.ClSIPerfVariant, err)
	}
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"clsiPerfVariant": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "clsiPerfVariant attribute should be a string" {
		t.Fatalf("want clsiPerfVariant string error, got %v", err)
	}
}

func TestIsCompileFromHistory(t *testing.T) {
	// rawChangeOperations set -> IsCompileFromHistory = true, RawChangeOperations is set
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options":             map[string]interface{}{},
			"rawChangeOperations": []interface{}{"op1"},
		},
	}, Config{})
	if err != nil || !got.IsCompileFromHistory || !isArr(got.RawChangeOperations) {
		t.Fatalf("IsCompileFromHistory=%v, RawChangeOperations=%v, %v", got.IsCompileFromHistory, got.RawChangeOperations, err)
	}
	// absent -> false
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{"options": map[string]interface{}{}},
	}, Config{})
	if err != nil || got.IsCompileFromHistory {
		t.Fatalf("IsCompileFromHistory should be false")
	}
}

func TestMetricsOpts(t *testing.T) {
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"metricsPath": "metrics/path", "metricsMethod": "GET"},
		},
	}, Config{})
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if got.MetricsOpts.Path != "metrics/path" || got.MetricsOpts.Method != "GET" {
		t.Fatalf("metricsOpts = %+v", got.MetricsOpts)
	}
	// non-string metricsPath
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"metricsPath": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "metricsPath attribute should be a string" {
		t.Fatalf("want metricsPath string error, got %v", err)
	}
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"metricsMethod": 5},
		},
	}, Config{}); err == nil ||
		err.Error() != "metricsMethod attribute should be a string" {
		t.Fatalf("want metricsMethod string error, got %v", err)
	}
	// compile: metricsOpts default (absent) -> empty strings, compile: "initial"
	if got.MetricsOpts.Compile != "initial" {
		t.Fatalf("metrics.Compile = %q, want initial", got.MetricsOpts.Compile)
	}
	_ = math.Pi
}

func TestPdfCachingMinChunkSize(t *testing.T) {
	// absent -> cfg default
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{"options": map[string]interface{}{}},
	}, Config{PdfCachingMinChunkSize: 5})
	if err != nil || got.PdfCachingMinChunkSize != 5 {
		t.Fatalf("default pdfCachingMinChunkSize = %v, want 5", got.PdfCachingMinChunkSize)
	}
	// explicit
	got, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"pdfCachingMinChunkSize": 3.0},
		},
	}, Config{})
	if err != nil || got.PdfCachingMinChunkSize != 3.0 {
		t.Fatalf("pdfCachingMinChunkSize = %v, want 3", got.PdfCachingMinChunkSize)
	}
	// non-number
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"pdfCachingMinChunkSize": "3"},
		},
	}, Config{}); err == nil ||
		err.Error() != "pdfCachingMinChunkSize attribute should be a number" {
		t.Fatalf("want number error, got %v", err)
	}
}

func TestCompileGroup(t *testing.T) {
	// settings.allowedCompileGroups not set -> attribute absent (undefined)
	got, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"compileGroup": "grp"},
		},
	}, Config{})
	if err != nil || got.CompileGroup != nil {
		t.Fatalf("compileGroup should be nil when config does not allow groups; got %v", got.CompileGroup)
	}
	// restrictions set -> default "" (absent), valid, invalid, non-string
	g, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{"options": map[string]interface{}{}},
	}, Config{AllowedCompileGroupsSet: true, AllowedCompileGroups: []string{"a", "b"}})
	if err != nil || g.CompileGroup == nil || *g.CompileGroup != "" {
		t.Fatalf("default compileGroup = %v, want \"\"", g.CompileGroup)
	}
	g, err = Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"compileGroup": "b"},
		},
	}, Config{AllowedCompileGroupsSet: true, AllowedCompileGroups: []string{"a", "b"}})
	if err != nil || g.CompileGroup == nil || *g.CompileGroup != "b" {
		t.Fatalf("compileGroup = %v, %v", g.CompileGroup, err)
	}
	if _, err := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{"compileGroup": "z"},
		},
	}, Config{AllowedCompileGroupsSet: true, AllowedCompileGroups: []string{"a", "b"}}); err == nil ||
		err.Error() != "compileGroup attribute should be one of: a, b" {
		t.Fatalf("want one-of compileGroup error, got %v", err)
	}
}

func TestIsCompileFromHistoryBool(t *testing.T) {
	// isCompileFromHistory = !!rawChangeOperations
	got, _ := Parse(map[string]interface{}{
		"compile": map[string]interface{}{
			"options": map[string]interface{}{},
		},
	}, Config{})
	if got.IsCompileFromHistory {
		t.Fatalf("IsCompileFromHistory default should be false")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
