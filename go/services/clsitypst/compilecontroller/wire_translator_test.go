package compilecontroller

import (
	"encoding/json"
	"net/http"
	"testing"

	cltypstcfg "ollitex/go/services/clsitypst/config"
	off "ollitex/go/services/clsitypst/outputfilefinder"
	requestparser "ollitex/go/services/clsitypst/requestparser"
)

// --- New (production wiring) ---------------------------------------------------------

func TestNewWiring(t *testing.T) {
	// env so config.ForTest populates the clsi block per D23.
	t.Setenv("SANDBOXED_COMPILES", "true")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	t.Setenv("TYPST_IMAGE", "pandoc/typst:latest-alpine")
	t.Setenv("INSTANCE_TYPE", "t3a")
	t.Setenv("ZONE", "z-b")
	t.Setenv("DOWNLOAD_HOST", "http://dh.test")
	t.Setenv("ALLOWED_COMPILE_GROUPS", "standard heavy")
	t.Setenv("PDF_CACHING_MIN_CHUNK_SIZE", "2048")
	cfg := cltypstcfg.ForTest()

	m := newTestManager(t, t.TempDir())
	c := New(m, cfg)
	if c.Manager != m {
		t.Fatal("manager wiring")
	}
	if c.MarkProjectAccessed == nil {
		t.Fatal("MarkProjectAccessed not wired (lastprojectaccess)")
	}
	if c.Config.InstanceType != "t3a" || c.Config.Zone != "z-b" ||
		c.Config.DownloadHost != "http://dh.test" {
		t.Fatalf("apis.clsi reads: %+v", c.Config)
	}
	if len(c.Config.AllowedImages) != 1 || c.Config.AllowedImages[0] != "pandoc/typst:latest-alpine" {
		t.Fatalf("allowedImages: %v", c.Config.AllowedImages)
	}
	if len(c.Config.AllowedCompileGroups) != 2 || !c.Config.AllowedCompileGroupsSet {
		t.Fatalf("allowedCompileGroups: %v %v", c.Config.AllowedCompileGroups, c.Config.AllowedCompileGroupsSet)
	}
	if c.Config.PdfCachingMinChunk != 2048 {
		t.Fatalf("pdfCachingMinChunk: %v", c.Config.PdfCachingMinChunk)
	}
	if c.Config.IsSpotInstance != false {
		t.Fatalf("isSpotInstance default: %v", c.Config.IsSpotInstance)
	}
	// ParserConfig surface (the requestparser.Config the controller hands
	// to Parse).
	pc := c.ParserConfig()
	if pc.AllowedCompileGroupsSet != true || pc.PdfCachingMinChunkSize != 2048 ||
		len(pc.AllowedImages) != 1 {
		t.Fatalf("ParserConfig: %+v", pc)
	}
}

func TestTimeSinceLastSuccessfulCompile(t *testing.T) {
	setLastSuccessfulCompile(0)
	if got := TimeSinceLastSuccessfulCompile(); got <= 0 {
		t.Fatalf("stamp 0 -> positive ms, got %d", got)
	}
}

// --- toRequest (Node: parse -> request field mapping) ------------------------------

func TestToRequestTypst(t *testing.T) {
	parsed := requestparser.ParsedResponse{
		IsCompileFromHistory:   true,
		MetricsOpts:            requestparser.MetricsOpts{Path: "/compile", Method: "post", Compile: "initial"},
		Compiler:               "typst",
		ImageName:              "pandoc/typst:latest-alpine",
		Timeout:                120000,
		StopOnFirstError:       true,
		CompileFromClsiCache:   true,
		EnablePdfCaching:       true,
		PdfCachingMinChunkSize: float64(1024),
		Resources: []requestparser.ParsedResource{
			{
				Path:     "main.typ",
				Modified: float64(1700000000000),
				URL:      "http://cdn/main.typ",
				Content:  "1 + 1",
			},
			{Path: "no-mod.typ"},
		},
		BuildID:            "abc1-def2",
		SyncType:           "initial",
		SyncState:          "clean",
		RootResourcePath:   "main.typ",
		BaseHistoryVersion: float64(42),
	}
	req := toRequest(parsed, "p-123", "u-456")
	if req.ProjectID != "p-123" || req.UserID != "u-456" {
		t.Fatalf("ids: %v/%v", req.ProjectID, req.UserID)
	}
	if req.Compiler != "typst" || req.ImageName != "pandoc/typst:latest-alpine" {
		t.Fatalf("compiler/image: %v %v", req.Compiler, req.ImageName)
	}
	if req.Timeout != 120000 || !req.StopOnFirstError {
		t.Fatalf("timeout/stop: %v %v", req.Timeout, req.StopOnFirstError)
	}
	if !req.CompileFromClsiCache || !req.EnablePdfCaching || req.PdfCachingMinChunk != 1024 {
		t.Fatalf("caching: %v %v %v", req.CompileFromClsiCache, req.EnablePdfCaching, req.PdfCachingMinChunk)
	}
	if req.BuildID == nil || *req.BuildID != "abc1-def2" {
		t.Fatalf("buildId: %v", req.BuildID)
	}
	if req.SyncType != "initial" || req.SyncState != "clean" || req.RootResourcePath != "main.typ" {
		t.Fatalf("sync: %v %v %v", req.SyncType, req.SyncState, req.RootResourcePath)
	}
	if req.BaseHistoryVersion != float64(42) {
		t.Fatalf("bhv echo: %v", req.BaseHistoryVersion)
	}
	if !req.IsCompileFromHistory {
		t.Fatalf("isCompileFromHistory: %v", req.IsCompileFromHistory)
	}
	if req.MetricsOpts.Path != "/compile" || req.MetricsOpts.Method != "post" {
		t.Fatalf("metricsOpts: %+v", req.MetricsOpts)
	}
	if len(req.RWResources) != 2 {
		t.Fatalf("resources: %d", len(req.RWResources))
	}
	r0 := req.RWResources[0]
	if r0.Path != "main.typ" || string(r0.Content) != "1 + 1" {
		t.Fatalf("r0: %+v", r0)
	}
	if r0.URL != "http://cdn/main.typ" || r0.FallbackURL != "" {
		t.Fatalf("r0 urls: %+v", r0)
	}
	if r0.Modified == nil || r0.Modified.UnixMilli() != 1700000000000 {
		t.Fatalf("modified: %v", r0.Modified)
	}
	if req.RWResources[1].Modified != nil {
		t.Fatalf("no-mod modified: %v", req.RWResources[1].Modified)
	}
}

func TestToRequestCompileGroup(t *testing.T) {
	// default (nil) -> "" (the dockerrunner default mount); set -> the value.
	if req := toRequest(requestparser.ParsedResponse{}, "p", "u"); req.CompileGroup != "" {
		t.Fatalf("nil compileGroup: %q", req.CompileGroup)
	}
	g := "standard"
	parsed := requestparser.ParsedResponse{CompileGroup: &g}
	if req := toRequest(parsed, "p", "u"); req.CompileGroup != "standard" {
		t.Fatalf("set compileGroup: %q", req.CompileGroup)
	}
}

// --- narrowing helpers (requestparser already validated the input) -------------------

func TestNarrowingHelpers(t *testing.T) {
	if str("s") != "s" || str(3.14) != "" {
		t.Fatalf("str")
	}
	if s := ptrStr("s"); s == nil || *s != "s" {
		t.Fatalf("ptrStr")
	}
	if ptrStr(3.14) != nil {
		t.Fatalf("ptrStr non-string")
	}
	if n := num64(float64(5)); n != 5 {
		t.Fatalf("num64 float: %v", n)
	}
	if n := num64(int(3)); n != 3 {
		t.Fatalf("num64 int: %v", n)
	}
	if n := num64(int64(7)); n != 7 {
		t.Fatalf("num64 int64: %v", n)
	}
	if n := num64("no"); n != 0 {
		t.Fatalf("num64 other: %v", n)
	}
	if mt := modifiedTime(float64(1700000000123)); mt == nil || mt.UnixMilli() != 1700000000123 {
		t.Fatalf("modifiedTime: %v", mt)
	}
	if modifiedTime("no") != nil {
		t.Fatalf("modifiedTime non-float")
	}
}

// --- wire (buildCompileBody / wireFile / userSeg / sendCompileWire) ------------------

func TestBuildCompileBodyArms(t *testing.T) {
	// (a) errorRender "" -> no error key (the success tail).
	a := buildCompileBody(compileWire{
		status: "success", code: 0,
		stats: map[string]any{"a": 1}, timings: map[string]any{},
		instanceType: "t3a", zone: "z1", isSpotInstance: true,
		outputURLPrefix: "http://o", downloadHost: "http://d",
		projectID: "p", userID: "u",
		outputFiles:        []off.OutputFile{{Path: "output.pdf", Type: "pdf", Build: "b1"}},
		baseHistoryVersion: intPtr(9),
	})
	cm, _ := a["compile"].(map[string]any)
	if _, has := cm["error"]; has {
		t.Fatalf("no error key expected: %v", cm)
	}
	if cm["baseHistoryVersion"] != 9 {
		t.Fatalf("bhv: %v", cm["baseHistoryVersion"])
	}
	if _, has := cm["clsiCacheShard"]; has {
		t.Fatalf("D15: clsiCacheShard must never be present: %v", cm)
	}

	// (b) errorRender "string" -> error key is the string.
	b := buildCompileBody(compileWire{status: "failure", code: 409, errorRender: "boom"})
	cmb, _ := b["compile"].(map[string]any)
	if cmb["error"] != "boom" {
		t.Fatalf("error string: %v", cmb["error"])
	}

	// (c) errorRender "object" -> {} error (Node: Error object -> {}).
	cc := buildCompileBody(compileWire{status: "failure", code: 500, errorRender: "object"})
	cmc, _ := cc["compile"].(map[string]any)
	if _, ok := cmc["error"].(map[string]any); !ok {
		t.Fatalf("error object: %v", cmc["error"])
	}

	// (d) buildID present + bhv nil.
	bidv := "b9"
	d := buildCompileBody(compileWire{status: "success", buildID: &bidv, code: 200})
	cmd, _ := d["compile"].(map[string]any)
	if cmd["buildId"] != "b9" {
		t.Fatalf("buildId: %v", cmd["buildId"])
	}
	if _, has := cmd["baseHistoryVersion"]; has {
		t.Fatalf("bhv omit when nil: %v", cmd)
	}
}

func intPtr(i int) *int { return &i }

func TestWireFileAndUserSeg(t *testing.T) {
	sz := int64(13)
	contentID := "c1"
	xr := int64(4096)
	c := compileWire{downloadHost: "http://d", projectID: "p", userID: "u"}
	f := off.OutputFile{
		Path: "output.pdf", Type: "pdf", Build: "b1",
		Size: &sz, ContentID: &contentID,
		Ranges:         []off.ContentRange{{ObjectID: "o1", Start: 0, End: 13}},
		StartXRefTable: &xr,
	}
	out := wireFile(c, f)
	wantURL := "http://d/project/p/user/u/build/b1/output/output.pdf"
	if out["url"] != wantURL {
		t.Fatalf("url: %v", out["url"])
	}
	if out["path"] != "output.pdf" || out["type"] != "pdf" {
		t.Fatalf("path/type: %v", out)
	}
	if out["size"] != int64(13) || out["build"] != "b1" || out["contentId"] != "c1" {
		t.Fatalf("fields: %v", out)
	}
	if out["startXRefTable"] != int64(4096) {
		t.Fatalf("startXRefTable: %v", out["startXRefTable"])
	}

	// no user + no build -> url without /user/ and build empty (Node parity:
	// the template yields 'build//'); build key omitted from the wire.
	c2 := compileWire{downloadHost: "http://d", projectID: "p", userID: ""}
	f2 := off.OutputFile{Path: "x", Type: "pdf"}
	out2 := wireFile(c2, f2)
	if out2["url"] != "http://d/project/p/build//output/x" {
		t.Fatalf("userSeg url: %s", out2["url"])
	}
	if _, has := out2["build"]; has {
		t.Fatalf("empty build omitted: %v", out2)
	}

	// no user, build set -> /build/{build}/output/{path} without /user/.
	f3 := off.OutputFile{Path: "x", Type: "pdf", Build: "b9"}
	out3 := wireFile(c2, f3)
	if out3["url"] != "http://d/project/p/build/b9/output/x" {
		t.Fatalf("no-user url: %s", out3["url"])
	}
	if out3["build"] != "b9" {
		t.Fatalf("build: %v", out3["build"])
	}
	if userSeg("u") != "/user/u" || userSeg("") != "" {
		t.Fatalf("userSeg")
	}
}

func TestSendCompileWire(t *testing.T) {
	// explicit code.
	res := newCtrlRecorder()
	if _, err := sendCompileWire(res, compileWire{
		status: "success", code: http.StatusCreated,
	}); err != nil || res.status != http.StatusCreated {
		t.Fatalf("wire: %v", err)
	}
	// code 0 -> 200 default (Node: res.status(code || 200)).
	res2 := newCtrlRecorder()
	if _, err := sendCompileWire(res2, compileWire{
		status: "success", code: 0,
	}); err != nil || res2.status != http.StatusOK {
		t.Fatalf("wire default: %v", err)
	}
}

// --- writeJSON / sendPlain -----------------------------------------------------------

func TestWriteJSON(t *testing.T) {
	res := newCtrlRecorder()
	if _, err := writeJSON(res, http.StatusOK, map[string]any{"ok": true}); err != nil ||
		res.status != http.StatusOK {
		t.Fatalf("writeJSON: %v", err)
	}
	if ct := res.header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type: %q", ct)
	}
	var back map[string]any
	if jerr := json.Unmarshal(res.body.Bytes(), &back); jerr != nil {
		t.Fatalf("body: %v", jerr)
	}
	if back["ok"] != true {
		t.Fatalf("roundtrip: %v", back)
	}
}

func TestSendPlainArms(t *testing.T) {
	// 200 with body -> sets content-type + writes.
	res := newCtrlRecorder()
	if _, err := sendPlain(res, http.StatusOK, "OK"); err != nil || res.body.String() != "OK" {
		t.Fatalf("sendPlain 200: %v", err)
	}
	if ct := res.header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("200 content-type: %q", ct)
	}

	// 4xx -> no write, no content-type header (sendPlain is 2xx-only).
	res4 := newCtrlRecorder()
	if _, err := sendPlain(res4, http.StatusConflict, "x"); err != nil {
		t.Fatalf("sendPlain 409 err: %v", err)
	}
	if res4.body.String() != "" {
		t.Fatalf("409 body should be empty, got: %q", res4.body.String())
	}
	if ct := res4.header.Get("Content-Type"); ct != "" {
		t.Fatalf("409 content-type should be empty, got: %q", ct)
	}

	// 204 (the clearCache/stopCompile 204 shape) -> empty text, no write.
	res5 := newCtrlRecorder()
	if _, err := sendPlain(res5, http.StatusNoContent, ""); err != nil {
		t.Fatalf("sendPlain 204 err: %v", err)
	}
	if res5.body.String() != "" {
		t.Fatalf("204 body should be empty")
	}
	if ct := res5.header.Get("Content-Type"); ct != "" {
		t.Fatalf("204 content-type should be empty, got %q", ct)
	}
}
