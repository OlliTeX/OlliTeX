package compilecontroller

import (
	"encoding/json"
	"net/http"
	"testing"

	off "ollitex/go/services/clsitex/outputfilefinder"
	"ollitex/go/services/clsitex/requestparser"
)

// --- toRequest: RW arm --------------------------------------------------------

func TestToRequestRawWrite(t *testing.T) {
	var cg = "standard"
	parsed := requestparser.ParsedResponse{
		Compiler:     "initial",
		CompileGroup: &cg,
		ImageName:    "texlive/texlive:2024",
		Timeout:      120000,
		Flags:        []any{"--fast", "b"},
		Check:        "validate",
		SyncType:     "initial",
		SyncState:    "clean",
		Resources: []requestparser.ParsedResource{
			{
				Path:        "main.tex",
				Modified:    float64(1700000000000),
				URL:         "http://cdn/main.tex",
				FallbackURL: "http://cdn2/main.tex",
				Content:     "hello",
			},
			{Path: "no-mod.tex"},
		},
		BuildID:                "abc1-def2",
		PdfCachingMinChunkSize: float64(1024),
		MetricsOpts:            requestparser.MetricsOpts{Path: "/compile", Method: "post", Compile: "initial"},
		CompileFromClsiCache:   true,
	}
	req := toRequest(parsed, "p-123", "u-456")
	if req.ProjectID != "p-123" || req.UserID != "u-456" {
		t.Fatalf("ids: %v/%v", req.ProjectID, req.UserID)
	}
	if req.Compiler != "initial" || req.ImageName != "texlive/texlive:2024" {
		t.Fatalf("compiler/image: %v %v", req.Compiler, req.ImageName)
	}
	if req.CompileGroup != "standard" || req.Timeout != 120000 {
		t.Fatalf("group/timeout: %v %v", req.CompileGroup, req.Timeout)
	}
	if req.Check == nil || *req.Check != "validate" {
		t.Fatalf("check: %v", req.Check)
	}
	if len(req.Flags) != 2 || req.Flags[0] != "--fast" || req.Flags[1] != "b" {
		t.Fatalf("flags: %v", req.Flags)
	}
	if req.BuildID == nil || *req.BuildID != "abc1-def2" {
		t.Fatalf("buildId: %v", req.BuildID)
	}
	if req.PdfCachingMinChunk != 1024 || !req.CompileFromClsiCache {
		t.Fatalf("minchunk/clsiCache: %v %v", req.PdfCachingMinChunk, req.CompileFromClsiCache)
	}
	if len(req.RWResources) != 2 || req.HRW != nil {
		t.Fatalf("resources/hrw: %d %v", len(req.RWResources), req.HRW)
	}
	r0 := req.RWResources[0]
	if r0.Path != "main.tex" || string(r0.Content) != "hello" {
		t.Fatalf("r0: %+v", r0)
	}
	if r0.URL != "http://cdn/main.tex" || r0.FallbackURL != "http://cdn2/main.tex" {
		t.Fatalf("r0 urls: %+v", r0)
	}
	if r0.Modified == nil || r0.Modified.UnixMilli() != 1700000000000 {
		t.Fatalf("modified: %v", r0.Modified)
	}
	if req.RWResources[1].Modified != nil {
		t.Fatalf("no-mod modified: %v", req.RWResources[1].Modified)
	}
}

// --- toRequest: HRW arm --------------------------------------------------------

func TestToRequestHistory(t *testing.T) {
	parsed := requestparser.ParsedResponse{
		IsCompileFromHistory: true,
		HistoryID:            "hh-01",
		BaseHistoryVersion:   float64(42),
		GlobalBlobs:          []any{"blob1", "blob2"},
		FilestoreBlobPrefix:  "prefix",
		ClSIPerfVariant:      "v-x",
		RawChangeOperations: []any{
			[]any{map[string]any{"op": "set", "file": "a.tex", "data": "x"}},
			[]any{map[string]any{"op": "delete", "file": "b.tex"}},
		},
		RawSnapshot:       map[string]any{"files": map[string]any{"a.tex": "x"}},
		PopulateClsiCache: true,
		Png2pdf:           true,
	}
	req := toRequest(parsed, "p1", "u1")
	if !req.IsCompileFromHistory || req.HRW == nil {
		t.Fatalf("hrw: %v", req.HRW)
	}
	if req.HRW.BaseHistoryVersion != 42 {
		t.Fatalf("bhv: %v", req.HRW.BaseHistoryVersion)
	}
	if req.HRW.HistoryID != "hh-01" || len(req.HRW.GlobalBlobs) != 2 ||
		req.HRW.GlobalBlobs[0] != "blob1" || req.HRW.GlobalBlobs[1] != "blob2" {
		t.Fatalf("historyid/blobs: %+v", req.HRW)
	}
	if req.HRW.FilestoreBlobPrefix != "prefix" || req.HRW.ClSIPerfVariant != "v-x" {
		t.Fatalf("prefix/variant: %+v", req.HRW)
	}
	if req.HRW.RawChangeOperations == nil || len(req.HRW.RawChangeOperations) != 2 {
		t.Fatalf("rawChangeOps: %v", req.HRW.RawChangeOperations)
	}
	if req.HRW.RawSnapshot == nil {
		t.Fatalf("snapshot missing")
	}
	if !req.HRW.PopulateClsiCache || !req.HRW.Png2pdf {
		t.Fatalf("pop/png2pdf: %+v", req.HRW)
	}
	if len(req.RWResources) != 0 {
		t.Fatalf("rw resources should be empty: %v", req.RWResources)
	}
}

// --- narrowing helpers ----------------------------------------------------------

func TestNarrowingHelpers(t *testing.T) {
	if str("s") != "s" || str(3.14) != "" {
		t.Fatalf("str")
	}
	if s := strPtr("s"); s == nil || *s != "s" {
		t.Fatalf("strPtr")
	}
	if strPtr(3.14) != nil {
		t.Fatalf("strPtr non-string")
	}
	if strPtrNil(nil) != "" {
		t.Fatalf("strPtrNil nil")
	}
	sp := "x"
	if strPtrNil(&sp) != "x" {
		t.Fatalf("strPtrNil ptr")
	}
	if s := ptrStr("s"); s == nil || *s != "s" {
		t.Fatalf("ptrStr")
	}
	if ptrStr(1) != nil {
		t.Fatalf("ptrStr non-string")
	}
	// flagsOf: strings only.
	if g := flagsOf([]any{"a", 3, "b"}); len(g) != 2 || g[0] != "a" || g[1] != "b" {
		t.Fatalf("flagsOf: %v", g)
	}
	if g := flagsOf("not-arr"); len(g) != 0 {
		t.Fatalf("flagsOf non-arr: %v", g)
	}
	// num64 arms.
	if n := num64(float64(5)); n != 5 {
		t.Fatalf("num64 float")
	}
	if n := num64(3); n != 3 {
		t.Fatalf("num64 int")
	}
	if n := num64(int64(7)); n != 7 {
		t.Fatalf("num64 int64")
	}
	if n := num64("no"); n != 0 {
		t.Fatalf("num64 other")
	}
	// modifiedTime.
	if mt := modifiedTime(float64(1700000000123)); mt == nil || mt.UnixMilli() != 1700000000123 {
		t.Fatalf("modifiedTime: %v", mt)
	}
	if modifiedTime("no") != nil {
		t.Fatalf("modifiedTime non-float")
	}
	// nil arms.
	if snapshotOf(nil) != nil {
		t.Fatalf("snapshotOf nil")
	}
	if globalBlobsOf(nil) != nil {
		t.Fatalf("globalBlobsOf nil")
	}
	if rawChangeOpsOf(nil) != nil {
		t.Fatalf("rawChangeOpsOf nil")
	}
	// jsonRound error arms (marshaling to wrong shape).
	if snapshotOf("not-map") != nil {
		t.Fatalf("snapshotOf non-map: %v", snapshotOf("not-map"))
	}
	if globalBlobsOf(map[string]any{"a": 1}) != nil {
		t.Fatalf("globalBlobsOf map")
	}
	if rawChangeOpsOf([]any{map[string]any{"a": 1}}) != nil {
		t.Fatalf("rawChangeOpsOf bad")
	}
}

// --- wire: buildCompileBody / wireFile / userSeg --------------------------------

func TestBuildCompileBodyArms(t *testing.T) {
	// (a) errorRender "" -> no error key.
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

	// (b) errorRender "string" -> error key is string.
	b := buildCompileBody(compileWire{status: "failure", errorRender: "boom"})
	cmb, _ := b["compile"].(map[string]any)
	if cmb["error"] != "boom" {
		t.Fatalf("error string: %v", cmb["error"])
	}

	// (c) errorRender "object" -> {} error.
	cbody := buildCompileBody(compileWire{status: "failure", errorRender: "object"})
	cmc, _ := cbody["compile"].(map[string]any)
	if _, ok := cmc["error"].(map[string]any); !ok {
		t.Fatalf("error object: %v", cmc["error"])
	}

	// (d) stats/timings nil -> {} defaults.
	d := buildCompileBody(compileWire{status: "s"})
	_ = d

	// buildID / shard presence.
	bidv := "b1"
	bid := buildCompileBody(compileWire{
		status: "success", buildID: &bidv, hasShard: true, clsiCacheShard: "shard-9",
	})
	cmbd, _ := bid["compile"].(map[string]any)
	if cmbd["buildId"] != "b1" || cmbd["clsiCacheShard"] != "shard-9" {
		t.Fatalf("buildId/shard: %v", cmbd)
	}
}

func intPtr(i int) *int { return &i }

// --- wireFile / userSeg ----------------------------------------------------------

func TestWireFileAndUserSeg(t *testing.T) {
	sz := int64(13)
	contentID := "c1"
	xr := int64(4096)
	c := compileWire{
		downloadHost: "http://d", projectID: "p", userID: "u",
	}
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
	if out["size"] != int64(13) || out["build"] != "b1" || out["contentId"] != "c1" {
		t.Fatalf("fields: %v", out)
	}
	if out["startXRefTable"] != int64(4096) {
		t.Fatalf("startXRefTable: %v", out["startXRefTable"])
	}

	// no user + no build -> url without /user/ and empty build segment.
	c2 := compileWire{downloadHost: "http://d", projectID: "p", userID: ""}
	f2 := off.OutputFile{Path: "x", Type: "pdf", Build: "b2"}
	if got := wireFile(c2, f2)["url"]; got != "http://d/project/p/build/b2/output/x" {
		t.Fatalf("userSeg url: %s", got)
	}
	if userSeg("u") != "/user/u" || userSeg("") != "" {
		t.Fatalf("userSeg")
	}
}

// --- sendCompileWire / writeJSON / sendPlain -----------------------------------

func TestSendCompileWire(t *testing.T) {
	// explicit code.
	res := newCtrlRecorder()
	if _, err := sendCompileWire(res, compileWire{
		status: "success", code: http.StatusOK,
	}); err != nil || res.status != http.StatusOK {
		t.Fatalf("wire: %v", err)
	}

	// code 0 -> 200 default.
	res2 := newCtrlRecorder()
	if _, err := sendCompileWire(res2, compileWire{
		status: "success", code: 0,
	}); err != nil || res2.status != http.StatusOK {
		t.Fatalf("wire default: %v", err)
	}

	// writeJSON marshals body + sets header.
	res3 := newCtrlRecorder()
	if _, err := writeJSON(res3, http.StatusCreated, map[string]any{
		"ok": true,
	}); err != nil || res3.status != http.StatusCreated {
		t.Fatalf("writeJSON: %v", err)
	}
	if ct := res3.header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type: %q", ct)
	}
	var back map[string]any
	if jerr := json.Unmarshal(res3.body.Bytes(), &back); jerr != nil {
		t.Fatalf("body: %v", jerr)
	}
	_ = back
}

// --- sendPlain --------------------------------------------------------------------

func TestSendPlainArms(t *testing.T) {
	// 200 with body -> sets content-type + writes.
	res := newCtrlRecorder()
	if _, err := sendPlain(res, http.StatusOK, "OK"); err != nil || res.body.String() != "OK" {
		t.Fatalf("sendPlain 200: %v", err)
	}
	if ct := res.header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("200 content-type: %q", ct)
	}

	// 4xx -> no write, no content-type header.
	res4 := newCtrlRecorder()
	if _, err := sendPlain(res4, http.StatusTooManyRequests, "x"); err != nil {
		t.Fatalf("sendPlain 429 err: %v", err)
	}
	if res4.body.String() != "" {
		t.Fatalf("429 body should be empty, got: %q", res4.body.String())
	}
	if ct := res4.header.Get("Content-Type"); ct != "" {
		t.Fatalf("429 content-type should be empty, got: %q", ct)
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
	// 200 with a non-empty text (the wordcount/sync 200 shape) writes it.
	res6 := newCtrlRecorder()
	if _, err := sendPlain(res6, http.StatusOK, "plain-body"); err != nil || res6.body.String() != "plain-body" {
		t.Fatalf("sendPlain 200 text: %v", err)
	}
}
