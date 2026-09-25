// Tests for the BuildSetContentChange (set-content) pipeline, mirroring the
// Node oracle test/acceptance/js/storage/build_set_content_change.test.js
// (the Redis-buffer diff case is out of scope: the Go port is hermetic with
// no Redis persist buffer).
package persist

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"history-v1/internal/core"
)

// Test constants mirroring the Node oracle (fixed for determinism).
const (
	scUserID = "abcdef0123456789abcdef01"
	scTSISO  = "2026-09-15T12:00:00.000Z"
	scPID    = "0000000000000000000000ab"
)

var (
	scTS = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
)

func farFutureLimits() Limits {
	min, max := farFuture()
	return Limits{MinChangeTimestamp: min, MaxChangeTimestamp: max}
}

// seedDoc persists an AddFile of a string doc at pathname (Node
// setupProjectWithDoc).
func seedDoc(t *testing.T, svc *Service, pid, pathname, content string) {
	t.Helper()
	ts := time.Now()
	change := fixedChange(ts, core.AddFile(pathname, stringFile(content)))
	if _, err := svc.PersistChanges(pid, []*core.Change{change}, farFutureLimits(), 0); err != nil {
		t.Fatalf("seed %s: %v", pathname, err)
	}
}

// seedBinaryDoc persists an AddFile of a binary file (Node:
// AddFileOperation(path, File.fromHash(GRAPH_PNG_HASH))).
func seedBinaryDoc(t *testing.T, svc *Service, pid, pathname string, bin []byte) string {
	t.Helper()
	pbs := svc.bs.Project(pid)
	h, err := pbs.PutBytes(bin)
	if err != nil {
		t.Fatalf("seedBinaryDoc PutBytes: %v", err)
	}
	change := fixedChange(scTS, core.AddFile(pathname, &core.File{Kind: "binary", Hash: h, ByteLength: len(bin)}))
	if _, err := svc.PersistChanges(pid, []*core.Change{change}, farFutureLimits(), 0); err != nil {
		t.Fatalf("seedBinaryDoc persist: %v", err)
	}
	return h
}

// commitSetContent commits the built change at level 0 (Node oracle commits
// with historyBufferLevel: 4; the final chunk state is equivalent).
func commitSetContent(t *testing.T, svc *Service, pid string, res *SetContentResult) {
	t.Helper()
	if _, err := svc.CommitChanges(pid, []*core.Change{res.Change}, farFutureLimits(), res.BaseVersion, CommitOptions{HistoryBufferLevel: 0}); err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
}

// loadEager mirrors Node getFile: load latest chunk, apply changes, eager
// load the file at pathname.
func loadEager(t *testing.T, svc *Service, pid, pathname string) (*core.File, int) {
	t.Helper()
	chunk, err := svc.cs.LoadLatest(pid)
	if err != nil {
		t.Fatalf("LoadLatest: %v", err)
	}
	pbs := svc.bs.Project(pid)
	// Node chunkStore.loadLatest eagerly lazy-loads; Go LoadLatest does not,
	// so load files before applying (same as persist.PersistChanges).
	if err := chunk.LoadFiles("lazy", pbs); err != nil {
		t.Fatalf("LoadFiles: %v", err)
	}
	snapshot := chunk.GetSnapshot()
	if err := snapshot.ApplyAll(chunk.GetChanges()); err != nil {
		t.Fatalf("ApplyAll: %v", err)
	}
	file := snapshot.GetFile(pathname)
	if file == nil {
		t.Fatalf("file %s not in snapshot", pathname)
	}
	loaded, err := file.Load("eager", pbs)
	if err != nil {
		t.Fatalf("eager load %s: %v", pathname, err)
	}
	return loaded, chunk.GetEndVersion()
}

func TestSetContentEditsExistingDocWithMinimalDiff(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "one\ntwo\nthree\n")

	result, err := svc.BuildSetContentChange(scPID, "main.tex", BuildSetContentOpts{
		Content:   strPtr("one\ntwo and a half\nthree\n"),
		UserID:    scUserID,
		Timestamp: scTS,
		Origin:    &core.Origin{Kind: "test-source"},
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.BaseVersion != 1 {
		t.Fatalf("baseVersion = %d, want 1", result.BaseVersion)
	}
	if result.Change == nil {
		t.Fatal("change = nil, want applied")
	}

	// The change carries the given origin, v2Authors, and the single edit op.
	var raw struct {
		Origin    json.RawMessage   `json:"origin"`
		V2Authors []json.RawMessage `json:"v2Authors"`
		Ops       []json.RawMessage `json:"operations"`
	}
	if err := json.Unmarshal(result.Change.ToRaw(), &raw); err != nil {
		t.Fatalf("unmarshal change raw: %v", err)
	}
	if len(raw.Origin) == 0 || string(raw.Origin) == "null" {
		t.Fatal("origin missing from change raw")
	}
	var origin struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(raw.Origin, &origin)
	if origin.Kind != "test-source" {
		t.Fatalf("origin = %+v, want kind test-source", raw.Origin)
	}
	if len(raw.V2Authors) != 1 {
		t.Fatalf("v2Authors = %s, want [userId]", raw.V2Authors)
	}
	if len(raw.Ops) != 1 {
		t.Fatalf("operations = %d ops, want 1", len(raw.Ops))
	}
	var op struct {
		Pathname string          `json:"pathname"`
		TextOp   json.RawMessage `json:"textOperation"`
	}
	if err := json.Unmarshal(raw.Ops[0], &op); err != nil {
		t.Fatalf("unmarshal op raw: %v", err)
	}
	if op.Pathname != "main.tex" || len(op.TextOp) == 0 {
		t.Fatalf("op raw = %s, want main.tex editFile op", raw.Ops[0])
	}

	commitSetContent(t, svc, scPID, result)
	file, version := loadEager(t, svc, scPID, "main.tex")
	if version != 2 {
		t.Fatalf("version = %d, want 2", version)
	}
	got, err := file.GetContent(false)
	if err != nil {
		t.Fatalf("GetContent: %v", err)
	}
	if got != "one\ntwo and a half\nthree\n" {
		t.Fatalf("content = %q, want edited", got)
	}
}

func TestSetContentReportsIdenticalContentAsNoop(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "same\n")

	result, err := svc.BuildSetContentChange(scPID, "main.tex", BuildSetContentOpts{
		Content:   strPtr("same\n"),
		UserID:    scUserID,
		Timestamp: scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.Change != nil || result.BaseVersion != 1 {
		t.Fatalf("result = %+v, want {nil change, baseVersion 1}", result)
	}
}

func TestSetContentUpdatesDocMetadata(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "same\n")

	result, err := svc.BuildSetContentChange(scPID, "main.tex", BuildSetContentOpts{
		Content:   strPtr("same\n"),
		Metadata:  json.RawMessage(`{"main":true}`),
		UserID:    scUserID,
		Timestamp: scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.BaseVersion != 1 || result.Change == nil {
		t.Fatalf("result = %+v, want applied at base 1", result)
	}

	// Exactly the setFileMetadata op.
	var probe struct {
		Ops []json.RawMessage `json:"operations"`
	}
	_ = json.Unmarshal(result.Change.ToRaw(), &probe)
	if len(probe.Ops) != 1 {
		t.Fatalf("operations = %d, want 1", len(probe.Ops))
	}
	var op map[string]json.RawMessage
	if err := json.Unmarshal(probe.Ops[0], &op); err != nil {
		t.Fatalf("unmarshal op: %v", err)
	}
	if string(op["pathname"]) != `"main.tex"` {
		t.Fatalf("op pathname = %s, want main.tex", op["pathname"])
	}
	if !bytes.Equal(op["metadata"], []byte(`{"main":true}`)) {
		t.Fatalf("op metadata = %s, want {\"main\":true}", op["metadata"])
	}
	if len(op) != 2 || op["file"] != nil || op["newPathname"] != nil || op["textOperation"] != nil {
		t.Fatalf("op raw = %s, want only {pathname, metadata}", op)
	}

	commitSetContent(t, svc, scPID, result)
	file, _ := loadEager(t, svc, scPID, "main.tex")
	got, _ := file.GetContent(false)
	if got != "same\n" {
		t.Fatalf("content = %q, want same\\n", got)
	}
	if !bytes.Equal(file.Metadata, []byte(`{"main":true}`)) {
		t.Fatalf("metadata = %s, want {\"main\":true}", file.Metadata)
	}
}

func TestSetContentReplacesBinaryFileWithEditableDoc(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedBinaryDoc(t, svc, scPID, "figure.tex", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	result, err := svc.BuildSetContentChange(scPID, "figure.tex", BuildSetContentOpts{
		Content:   strPtr("hello world"),
		Metadata:  json.RawMessage(`{"main":true}`),
		UserID:    scUserID,
		Timestamp: scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.BaseVersion != 1 || result.Change == nil {
		t.Fatalf("result = %+v, want applied at base 1", result)
	}
	// Node oracle: the seeded file loads binary (blob has no stringLength),
	// so the replacement is [removeFile (moveFile newPathname=""), addFile].
	ops := result.Change.Operations
	if len(ops) != 2 || ops[0].Kind != "moveFile" || ops[0].NewPathname != "" || ops[1].Kind != "addFile" {
		var kinds []string
		for _, o := range ops {
			kinds = append(kinds, o.Kind)
		}
		t.Fatalf("op kinds = %v, want [moveFile addFile]", kinds)
	}
	if ops[0].Pathname != "figure.tex" {
		t.Fatalf("ops[0].pathname = %q", ops[0].Pathname)
	}
	if ops[1].AddFile.GetHash() == "" {
		t.Fatal("ops[1].file.hash missing")
	}
	commitSetContent(t, svc, scPID, result)
	file, _ := loadEager(t, svc, scPID, "figure.tex")
	if !file.IsEditable() {
		t.Fatal("file not editable after replacement")
	}
	got, _ := file.GetContent(false)
	if got != "hello world" {
		t.Fatalf("content = %q, want hello world", got)
	}
	if !bytes.Equal(file.Metadata, []byte(`{"main":true}`)) {
		t.Fatalf("metadata = %s, want {\"main\":true}", file.Metadata)
	}
}

func TestSetContentCreatesNewDocWhenNoFileAtPathname(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "main\n")

	result, err := svc.BuildSetContentChange(scPID, "chapters/one.tex", BuildSetContentOpts{
		Content:   strPtr("chapter one\n"),
		UserID:    scUserID,
		Timestamp: scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.BaseVersion != 1 || result.Change == nil {
		t.Fatalf("result = %+v, want applied at base 1", result)
	}

	commitSetContent(t, svc, scPID, result)
	file, _ := loadEager(t, svc, scPID, "chapters/one.tex")
	got, _ := file.GetContent(false)
	if got != "chapter one\n" {
		t.Fatalf("content = %q, want chapter one\\n", got)
	}
}

func TestSetContentRecordsEditAsTrackedChanges(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "hello cruel world")

	result, err := svc.BuildSetContentChange(scPID, "main.tex", BuildSetContentOpts{
		Content:      strPtr("hello brave world"),
		TrackChanges: true,
		UserID:       scUserID,
		Timestamp:    scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.BaseVersion != 1 || result.Change == nil {
		t.Fatalf("result = %+v, want applied at base 1", result)
	}

	commitSetContent(t, svc, scPID, result)
	file, _ := loadEager(t, svc, scPID, "main.tex")

	filtered, _ := file.GetContent(true)
	if filtered != "hello brave world" {
		t.Fatalf("filtered content = %q, want hello brave world", filtered)
	}
	full, _ := file.GetContent(false)
	if !strings.Contains(full, "cruel") {
		t.Fatalf("unfiltered content %q does not retain the tracked delete", full)
	}

	tcs := file.TrackedChanges
	if len(tcs) != 2 {
		t.Fatalf("trackedChanges = %d, want 2 (delete + insert)", len(tcs))
	}
	var haveDelete, haveInsert bool
	for _, tc := range tcs {
		if tc.Tracking == nil || tc.Tracking.Type != "delete" && tc.Tracking.Type != "insert" {
			t.Fatalf("tracked change missing tracking: %+v", tc)
		}
		if tc.Tracking.Type == "delete" {
			haveDelete = true
		}
		if tc.Tracking.Type == "insert" {
			haveInsert = true
		}
		if tc.Tracking.UserID != scUserID || tc.Tracking.TSISO != scTSISO {
			t.Fatalf("tracking = %s/%s, want %s / %s", tc.Tracking.Type, tc.Tracking.UserID, scUserID, scTSISO)
		}
	}
	if !haveDelete || !haveInsert {
		t.Fatalf("tracked changes missing delete/insert: %v", file.TrackedChanges.ToRaw())
	}
}

func TestSetContentRecordsNewDocContentAsTrackedInsert(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "main\n")

	result, err := svc.BuildSetContentChange(scPID, "new.tex", BuildSetContentOpts{
		Content:      strPtr("new content"),
		TrackChanges: true,
		UserID:       scUserID,
		Timestamp:    scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.Change == nil {
		t.Fatal("change = nil, want applied")
	}

	commitSetContent(t, svc, scPID, result)
	file, _ := loadEager(t, svc, scPID, "new.tex")
	got, _ := file.GetContent(false)
	if got != "new content" {
		t.Fatalf("content = %q, want new content", got)
	}
	rawTC := file.TrackedChanges.ToRaw()
	var tc []struct {
		Range struct {
			Pos    int `json:"pos"`
			Length int `json:"length"`
		} `json:"range"`
		Tracking struct {
			Type   string `json:"type"`
			UserID string `json:"userId"`
			Ts     string `json:"ts"`
		} `json:"tracking"`
	}
	if err := json.Unmarshal(rawTC, &tc); err != nil || len(tc) != 1 {
		t.Fatalf("trackedChanges raw = %s, want one range", rawTC)
	}
	if tc[0].Range.Pos != 0 || tc[0].Range.Length != len("new content") {
		t.Fatalf("range = %d/%d, want 0/%d", tc[0].Range.Pos, tc[0].Range.Length, len("new content"))
	}
	if tc[0].Tracking.Type != "insert" || tc[0].Tracking.UserID != scUserID || tc[0].Tracking.Ts != scTSISO {
		t.Fatalf("tracking = %s/%s/%s, want insert/%s/%s",
			tc[0].Tracking.Type, tc[0].Tracking.UserID, tc[0].Tracking.Ts, scUserID, scTSISO)
	}
}

func TestSetContentRejectsTooLargeContent(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "main\n")

	tooLarge := strings.Repeat("x", core.MaxStringLength+1)
	_, err := svc.BuildSetContentChange(scPID, "main.tex", BuildSetContentOpts{
		Content:   &tooLarge,
		UserID:    scUserID,
		Timestamp: scTS,
	})
	var ctle *ContentTooLargeError
	if !errors.As(err, &ctle) {
		t.Fatalf("err = %v, want *ContentTooLargeError", err)
	}
	if ctle.ContentLength != core.MaxStringLength+1 {
		t.Fatalf("contentLength = %d, want %d", ctle.ContentLength, core.MaxStringLength+1)
	}
}

func TestSetContentThrowsWhenProjectDoesNotExist(t *testing.T) {
	svc, _, _ := newHarness(t)
	missing := "111111111111111111111111"

	_, err := svc.BuildSetContentChange(missing, "main.tex", BuildSetContentOpts{
		Content:   strPtr("hello"),
		UserID:    scUserID,
		Timestamp: scTS,
	})
	var cnfe *core.ChunkNotFoundError
	if !errors.As(err, &cnfe) {
		t.Fatalf("err = %v, want *core.ChunkNotFoundError", err)
	}
}

func TestSetContentReplacesDocWithBinaryFile(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "figure.png", "placeholder")
	pbs := svc.bs.Project(scPID)
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	pngHash, err := pbs.PutBytes(pngBytes)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	result, err := svc.BuildSetContentChange(scPID, "figure.png", BuildSetContentOpts{
		BlobHash:  &pngHash,
		UserID:    scUserID,
		Timestamp: scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.BaseVersion != 1 || result.Change == nil {
		t.Fatalf("result = %+v, want applied at base 1", result)
	}
	if len(result.Change.Operations) != 2 {
		t.Fatalf("operations = %d, want 2 (remove, add)", len(result.Change.Operations))
	}

	commitSetContent(t, svc, scPID, result)
	file, _ := loadEager(t, svc, scPID, "figure.png")
	if file.IsEditable() {
		t.Fatal("file editable after binary replacement, want binary")
	}
	if file.GetHash() != pngHash {
		t.Fatalf("hash = %s, want %s", file.GetHash(), pngHash)
	}
}

func TestSetContentReportsSameHashAndMetadataAsNoop(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	pngHash := seedBinaryDoc(t, svc, scPID, "figure.png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	result, err := svc.BuildSetContentChange(scPID, "figure.png", BuildSetContentOpts{
		BlobHash:  &pngHash,
		UserID:    scUserID,
		Timestamp: scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.Change != nil || result.BaseVersion != 1 {
		t.Fatalf("result = %+v, want {nil change, baseVersion 1}", result)
	}
}

func TestSetContentOnlySetsMetadataWhenHashMatchesButMetadataDiffers(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	pngHash := seedBinaryDoc(t, svc, scPID, "figure.png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	result, err := svc.BuildSetContentChange(scPID, "figure.png", BuildSetContentOpts{
		BlobHash:  &pngHash,
		Metadata:  json.RawMessage(`{"importer":"github"}`),
		UserID:    scUserID,
		Timestamp: scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.BaseVersion != 1 || result.Change == nil {
		t.Fatalf("result = %+v, want applied at base 1", result)
	}

	var probe struct {
		Ops []json.RawMessage `json:"operations"`
	}
	_ = json.Unmarshal(result.Change.ToRaw(), &probe)
	if len(probe.Ops) != 1 {
		t.Fatalf("operations = %d, want 1", len(probe.Ops))
	}
	var op map[string]json.RawMessage
	if err := json.Unmarshal(probe.Ops[0], &op); err != nil {
		t.Fatalf("unmarshal op: %v", err)
	}
	if string(op["pathname"]) != `"figure.png"` {
		t.Fatalf("op pathname = %s", op["pathname"])
	}
	if !bytes.Equal(op["metadata"], []byte(`{"importer":"github"}`)) {
		t.Fatalf("op metadata = %s", op["metadata"])
	}
	if len(op) != 2 {
		t.Fatalf("op raw = %s, want only {pathname, metadata}", op)
	}

	commitSetContent(t, svc, scPID, result)
	file, _ := loadEager(t, svc, scPID, "figure.png")
	if file.GetHash() != pngHash {
		t.Fatalf("hash = %s, want %s", file.GetHash(), pngHash)
	}
	if !bytes.Equal(file.Metadata, []byte(`{"importer":"github"}`)) {
		t.Fatalf("metadata = %s, want {\"importer\":\"github\"}", file.Metadata)
	}
}

func TestSetContentAddsNewBinaryFileWhenNoFileAtPathname(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "main\n")
	pbs := svc.bs.Project(scPID)
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	pngHash, err := pbs.PutBytes(pngBytes)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	result, err := svc.BuildSetContentChange(scPID, "images/graph.png", BuildSetContentOpts{
		BlobHash:  &pngHash,
		UserID:    scUserID,
		Timestamp: scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.BaseVersion != 1 || result.Change == nil {
		t.Fatalf("result = %+v, want applied at base 1", result)
	}

	commitSetContent(t, svc, scPID, result)
	file, _ := loadEager(t, svc, scPID, "images/graph.png")
	if file.GetHash() != pngHash {
		t.Fatalf("hash = %s, want %s", file.GetHash(), pngHash)
	}
}

func TestSetContentThrowsWhenBlobDoesNotExist(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "main\n")
	unknownHash := "0123456789012345678901234567890123456789"

	_, err := svc.BuildSetContentChange(scPID, "images/graph.png", BuildSetContentOpts{
		BlobHash:  &unknownHash,
		UserID:    scUserID,
		Timestamp: scTS,
	})
	var bnf *ServiceBlobNotFoundError
	if !errors.As(err, &bnf) {
		t.Fatalf("err = %v, want *ServiceBlobNotFoundError", err)
	}
}

func TestSetContentIgnoresTrackChangesFlagForBlobs(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "main\n")
	pbs := svc.bs.Project(scPID)
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	pngHash, _ := pbs.PutBytes(pngBytes)

	result, err := svc.BuildSetContentChange(scPID, "images/graph.png", BuildSetContentOpts{
		BlobHash:     &pngHash,
		TrackChanges: true,
		UserID:       scUserID,
		Timestamp:    scTS,
	})
	if err != nil {
		t.Fatalf("BuildSetContentChange: %v", err)
	}
	if result.Change == nil {
		t.Fatal("change = nil, want applied")
	}

	commitSetContent(t, svc, scPID, result)
	file, _ := loadEager(t, svc, scPID, "images/graph.png")
	if file.GetRangesHash() != "" {
		t.Fatalf("rangesHash = %s, want absent", file.GetRangesHash())
	}
}

func TestSetContentRequiresExactlyOneOfContentAndBlobHash(t *testing.T) {
	svc, cs, _ := newHarness(t)
	initProject(t, cs, scPID)
	seedDoc(t, svc, scPID, "main.tex", "main\n")
	pngHash := "0123456789012345678901234567890123456789"

	for _, opts := range []BuildSetContentOpts{
		{UserID: scUserID, Timestamp: scTS},
		{Content: strPtr("a"), BlobHash: &pngHash, UserID: scUserID, Timestamp: scTS},
	} {
		_, err := svc.BuildSetContentChange(scPID, "main.tex", opts)
		var xor *SetContentXorError
		if !errors.As(err, &xor) {
			t.Fatalf("err = %v, want *SetContentXorError", err)
		}
		if !strings.Contains(xor.Msg, "exactly one of content and blobHash") {
			t.Fatalf("msg = %q, want XOR error", xor.Msg)
		}
	}

	// trackChanges without a userId is rejected.
	_, err := svc.BuildSetContentChange(scPID, "main.tex", BuildSetContentOpts{
		Content:      strPtr("a"),
		TrackChanges: true,
		Timestamp:    scTS,
	})
	var noUser *SetContentXorError
	if !errors.As(err, &noUser) || !strings.Contains(noUser.Msg, "trackChanges requires a userId") {
		t.Fatalf("err = %v, want *SetContentXorError (trackChanges requires a userId)", noUser)
	}
}

func strPtr(s string) *string { return &s }
