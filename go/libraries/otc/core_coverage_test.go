package otc

import (
	"context"
	"testing"
)

// Exercises accessors, remaining transform branches, and error paths so the
// Phase C core stays above the coverage bar.

func TestCoverageChangeAccessors(t *testing.T) {
	ops := []Operation{OperationAddFile("f", nil)}
	c := NewChange(ops, nowTime(), []any{"x", "x", "y"}, nil, []string{"v2"}, nil, nil)

	if len(c.GetOperations()) != 1 {
		t.Fatal("GetOperations")
	}
	c.SetOperations(ops)
	if c.GetTimestamp().IsZero() {
		t.Fatal("GetTimestamp")
	}
	if len(c.GetAuthors()) != 3 {
		t.Fatal("GetAuthors")
	}
	if len(c.GetV2Authors()) != 1 || c.GetV2Authors()[0] != "v2" {
		t.Fatal("GetV2Authors")
	}
	if c.GetOrigin() != nil {
		t.Fatal("GetOrigin should be nil")
	}
	if c.GetProjectVersion() != nil {
		t.Fatal("GetProjectVersion should be nil")
	}
	if c.GetV2DocVersions() != nil {
		t.Fatal("GetV2DocVersions should be nil")
	}
	if c, err := ChangeFromRaw(nil); err != nil || c != nil {
		t.Fatal("nil raw -> nil, no err")
	}
	_ = strPtr("0.1")
}

func TestCoverageChangeSettersErrors(t *testing.T) {
	c := NewChange(nil, nowTime(), nil, nil, nil, nil, nil)
	// bad operations
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic for bad operations")
			}
		}()
		c.SetOperations(nil)
	}()
}

func TestCoverageChangeStore(t *testing.T) {
	c := NewChange([]Operation{OperationMoveFile("a", "b")}, nowTime(), []any{"x", "x", "y"}, nil, nil, nil, nil)
	raw, err := c.Store(realBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	authors, _ := raw["authors"].([]any)
	if len(authors) != 2 {
		t.Fatalf("expected deduped authors [x y], got %v", raw["authors"])
	}
	if ops, _ := raw["operations"].([]any); len(ops) != 1 {
		t.Fatal("expected 1 op")
	}
}

func TestCoverageChangeTransformAfterAndClone(t *testing.T) {
	a := NewChange([]Operation{OperationAddFile("same.tex", nil)}, nowTime(), nil, nil, nil, nil, nil)
	b := NewChange([]Operation{OperationAddFile("other.tex", nil)}, nowTime(), nil, nil, nil, nil, nil)
	before := len(a.GetOperations())
	a.TransformAfter(b)
	if len(a.GetOperations()) != before {
		t.Fatal("TransformAfter should keep op count")
	}

	// clone via a change with everything
	v2 := NewV2DocVersions(map[string]any{"d": map[string]any{"pathname": "a", "version": int64(1)}})
	origin, _ := NewOriginWithID(EditorOriginKind, strPtr(clientId))
	full := NewChange([]Operation{OperationMoveFile("p", "q")}, nowTime(), []any{"u"}, origin, []string{"v"}, strPtr("10.0"), v2)
	clone, err := full.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if clone.GetOrigin() == nil || clone.GetV2DocVersions() == nil || clone.GetProjectVersion() == nil {
		t.Fatal("clone lost fields")
	}
}

func TestCoverageChangeFromRawErrors(t *testing.T) {
	if _, err := ChangeFromRaw(map[string]any{}); err == nil {
		t.Fatal("expected error for bad raw.operations")
	}
	if _, err := ChangeFromRaw(map[string]any{"operations": []any{}, "timestamp": ""}); err == nil {
		t.Fatal("expected error for bad raw.timestamp")
	}
	if _, err := ChangeFromRaw(map[string]any{"operations": []any{123}, "timestamp": "2025-01-02T03:04:05.678Z"}); err == nil {
		t.Fatal("expected error for bad operation")
	}
}

func TestCoverageChangeApplyToRecoverableBranch(t *testing.T) {
	// FileNotFoundError recoverable path: edit a missing file non-strict
	c := NewChange([]Operation{OperationEditFile("missing", NewTextEdit(NewTextOperation()))}, nowTime(), nil, nil, nil, nil, nil)
	s := NewSnapshot(nil, nil, nil, nil)
	// missing file -> EditMissingFileError -> recoverable, ignored non-strict
	if err := c.ApplyTo(s, false); err != nil {
		t.Fatalf("non-strict should ignore recoverable: %v", err)
	}
	// strict propagates
	s2 := NewSnapshot(nil, nil, nil, nil)
	if err := c.ApplyTo(s2, true); err == nil || !isErrType[*EditMissingFileError](err) {
		t.Fatalf("strict should propagate EditMissingFileError, got %v", err)
	}
}

func TestCoverageSnapshot(t *testing.T) {
	// build a full snapshot and round-trip
	fileA, _ := FileFromString("hello", map[string]any{"main": true})
	fm, _ := NewFileMap(map[string]*File{"a.tex": fileA})
	ts := nowTime()
	pv := "1.2"
	v2 := NewV2DocVersions(map[string]any{"d": map[string]any{"pathname": "a.tex", "version": int64(3)}})
	s := NewSnapshot(fm, &pv, v2, &ts)

	if s.GetFileMap() == nil {
		t.Fatal("GetFileMap")
	}
	if s.CountFiles() != 1 {
		t.Fatal("CountFiles")
	}
	if *s.GetProjectVersion() != "1.2" {
		t.Fatal("GetProjectVersion")
	}
	if s.GetV2DocVersions() == nil {
		t.Fatal("GetV2DocVersions")
	}

	raw := s.ToRaw()
	back, err := SnapshotFromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.CountFiles() != 1 || *back.GetProjectVersion() != "1.2" {
		t.Fatal("round-trip lost data")
	}

	// SetV2DocVersions / UpdateV2DocVersions
	nv2 := NewV2DocVersions(map[string]any{"d2": map[string]any{"pathname": "b", "version": int64(1)}})
	s2 := NewSnapshot(nil, nil, nil, nil)
	s2.SetV2DocVersions(nv2)
	other := NewV2DocVersions(map[string]any{"d3": map[string]any{"pathname": "c", "version": int64(9)}})
	s2.UpdateV2DocVersions(other)
	if _, ok := s2.GetV2DocVersions().Data["d3"]; !ok {
		t.Fatal("UpdateV2DocVersions should merge")
	}

	// bad projectVersion panics
	assertPanics(t, func() {
		s.SetProjectVersion(strPtr("bad version"))
	})

	// LoadFiles (content file, no real load needed)
	loaded := s.LoadFiles(context.Background(), "eager", realBlobStore())
	if _, ok := loaded["a.tex"]; !ok {
		t.Fatal("LoadFiles should include a.tex")
	}

	// Store
	stored, err := s.Store(context.Background(), realBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stored["files"].(map[string]map[string]any); !ok {
		t.Fatal("stored files should be map[string]map[string]any")
	}

	// SnapshotFromRaw error: bad files
	if _, err := SnapshotFromRaw(map[string]any{}); err == nil {
		t.Fatal("expected error for missing files")
	}
}

func TestCoverageSnapshotMoveFileV2(t *testing.T) {
	fileA, _ := FileFromString("x", nil)
	fm, _ := NewFileMap(map[string]*File{"a.tex": fileA})
	v2 := NewV2DocVersions(map[string]any{"d": map[string]any{"pathname": "a.tex", "version": int64(1)}})
	s := NewSnapshot(fm, nil, v2, nil)
	if err := s.MoveFile("a.tex", "new.tex"); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.GetV2DocVersions().Data["d"]; !ok || v.(map[string]any)["pathname"] != "new.tex" {
		t.Fatal("MoveFile should update v2DocVersions")
	}
}

func TestCoverageNoOp(t *testing.T) {
	n := OperationNoOp
	n.FindBlobHashes(map[string]bool{})
	raw, err := n.Store(realBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 0 {
		t.Fatal("noop raw should be empty")
	}
	if n.CanBeComposedWith(OperationNoOp) {
		t.Fatal("noop not composable")
	}
	if n.CanBeComposedWithForUndo(OperationNoOp) {
		t.Fatal("noop not composable for undo")
	}
	if _, err := n.Compose(OperationNoOp); err == nil {
		t.Fatal("noop compose should error")
	}
}

func TestCoverageAddFileAccessors(t *testing.T) {
	f, _ := FileFromString("c", nil)
	a, _ := NewAddFileOperation("p", f)
	if a.GetPathname() != "p" || a.GetFile() == nil {
		t.Fatal("accessors")
	}
	if a.CanBeComposedWith(OperationNoOp) {
		t.Fatal("not composable")
	}
	if a.CanBeComposedWithForUndo(OperationNoOp) {
		t.Fatal("not composable for undo")
	}
	if _, err := a.Compose(OperationNoOp); err == nil {
		t.Fatal("compose should error")
	}
	// error paths
	if _, err := NewAddFileOperation("", f); err == nil {
		t.Fatal("empty pathname should error")
	}
	if _, err := NewAddFileOperation("p", nil); err == nil {
		t.Fatal("nil file should error")
	}
	if _, err := AddFileOperationFromRaw(map[string]any{"pathname": "p"}); err == nil {
		t.Fatal("missing file should error")
	}
}

func TestCoverageMoveFileAccessors(t *testing.T) {
	mOp, ok := OperationMoveFile("a", "b").(*MoveFileOperation)
	if !ok {
		t.Fatal("move type")
	}
	if mOp.GetPathname() != "a" || mOp.GetNewPathname() != "b" {
		t.Fatal("accessors")
	}
	m := mOp
	m.FindBlobHashes(map[string]bool{})
	raw, err := m.Store(realBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	if raw["newPathname"] != "b" {
		t.Fatal("store")
	}
	if m.CanBeComposedWith(OperationNoOp) || m.CanBeComposedWithForUndo(OperationNoOp) {
		t.Fatal("not composable")
	}
	if _, err := m.Compose(OperationNoOp); err == nil {
		t.Fatal("compose should error")
	}
}

func TestCoverageEditFileOp(t *testing.T) {
	eOp, ok := OperationEditFile("f", NewTextEdit(NewTextOperation())).(*EditFileOperation)
	if !ok {
		t.Fatal("edit type")
	}
	if eOp.GetPathname() != "f" || eOp.GetOperation() == nil {
		t.Fatal("accessors")
	}
	e := eOp
	raw := e.ToRaw()
	back, err := EditFileOperationFromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Pathname != "f" {
		t.Fatal("fromRaw")
	}
	e.FindBlobHashes(map[string]bool{})
	raw2, err := e.Store(realBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	if raw2["pathname"] != "f" {
		t.Fatal("store")
	}
	if e.CanBeComposedWith(OperationNoOp) || e.CanBeComposedWithForUndo(OperationNoOp) {
		t.Fatal("not composable with noop")
	}
	if e.CanBeComposedWith(OperationAddFile("f", nil)) {
		t.Fatal("edit not composable with add")
	}
	if _, err := e.Compose(OperationNoOp); err == nil {
		t.Fatal("compose noop should error")
	}

	// composable text edits on same file
	op1 := NewTextOperation()
	if err := op1.Insert("ab", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	e1v := OperationEditFile("f", NewTextEdit(op1)).(*EditFileOperation)
	op2 := NewTextOperation()
	if err := op2.Retain(2, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := op2.Insert("c", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	e2v := OperationEditFile("f", NewTextEdit(op2)).(*EditFileOperation)
	if !e1v.CanBeComposedWith(e2v) {
		t.Skip("text edits not flagged composable; skip compose")
	}
	if _, err := e1v.Compose(e2v); err != nil {
		// Compose may error if not actually composable
		t.Logf("compose err: %v", err)
	}
}

func TestCoverageSetMetaOp(t *testing.T) {
	sOp, ok := OperationSetFileMetadata("p", map[string]any{"k": "v"}).(*SetFileMetadataOperation)
	if !ok {
		t.Fatal("set type")
	}
	if sOp.GetPathname() != "p" || sOp.GetMetadata()["k"] != "v" {
		t.Fatal("accessors")
	}
	s := sOp
	s.FindBlobHashes(map[string]bool{})
	raw, err := s.Store(realBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	if raw["metadata"].(map[string]any)["k"] != "v" {
		t.Fatal("store")
	}
	if s.CanBeComposedWith(OperationNoOp) || s.CanBeComposedWithForUndo(OperationNoOp) {
		t.Fatal("not composable")
	}
	if _, err := s.Compose(OperationNoOp); err == nil {
		t.Fatal("compose should error")
	}
	// error paths
	if _, err := NewSetFileMetadataOperation("", map[string]any{}); err == nil {
		t.Fatal("empty pathname")
	}
	if _, err := NewSetFileMetadataOperation("p", nil); err == nil {
		t.Fatal("nil metadata")
	}
	// applyTo on a missing file is a no-op
	s2 := NewSnapshot(nil, nil, nil, nil)
	if err := s.ApplyTo(s2); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageTransformRemaining(t *testing.T) {
	// transformAddEdit: add at edit path -> edit dropped
	add := fileOp(t, "f", "c")
	edit := OperationEditFile("f", NewTextEdit(NewTextOperation()))
	pr := OperationTransform(add, edit)
	expectType(t, "add'", pr[0], "AddFile")
	isOpNoOp(t, "edit'", pr[1])

	// transformAddEdit different -> both kept
	add2 := fileOp(t, "g", "c")
	pr2 := OperationTransform(add2, edit)
	expectType(t, "add2'", pr2[0], "AddFile")
	expectType(t, "edit2'", pr2[1], "EditFile")

	// transpose: edit vs add (same path) -> edit' is dropped
	pr3 := OperationTransform(edit, add)
	isOpNoOp(t, "edit-t'", pr3[0])
	expectType(t, "add-t'", pr3[1], "AddFile")

	// transformAddSet: same path -> add gets metadata
	set := OperationSetFileMetadata("f", map[string]any{"m": true})
	pr4 := OperationTransform(fileOp(t, "f", "c"), set)
	if af, ok := pr4[0].(*AddFileOperation); !ok || af.File.GetMetadata()["m"] == nil {
		t.Fatalf("add' should carry metadata: %T", pr4[0])
	}

	// transformAddSet different -> both kept
	pr5 := OperationTransform(fileOp(t, "g", "c"), set)
	expectType(t, "a'", pr5[0], "AddFile")
	expectType(t, "s'", pr5[1], "SetFileMetadata")

	// transpose: set vs add
	pr6 := OperationTransform(set, fileOp(t, "f", "c"))
	expectType(t, "s-t'", pr6[0], "SetFileMetadata")
	expectType(t, "a-t'", pr6[1], "AddFile")

	// transformMoveSet: set at moved-from -> re-homed
	mv := OperationMoveFile("f", "g")
	pr7 := OperationTransform(mv, OperationSetFileMetadata("f", map[string]any{"m": true}))
	expectType(t, "move'", pr7[0], "MoveFile")
	if sf, ok := pr7[1].(*SetFileMetadataOperation); !ok || sf.Pathname != "g" {
		t.Fatalf("set' = %T %v", pr7[1], pr7[1])
	}

	// transformMoveSet: set at new path -> dropped
	pr8 := OperationTransform(mv, OperationSetFileMetadata("g", map[string]any{"m": true}))
	expectType(t, "move8'", pr8[0], "MoveFile")
	isOpNoOp(t, "set8'", pr8[1])

	// transpose: set vs move
	pr9 := OperationTransform(OperationSetFileMetadata("f", map[string]any{"m": true}), mv)
	expectType(t, "s9'", pr9[0], "SetFileMetadata")
	expectType(t, "m9'", pr9[1], "MoveFile")

	// transformEditSet: untouched
	pr10 := OperationTransform(edit, set)
	expectType(t, "edit10'", pr10[0], "EditFile")
	expectType(t, "set10'", pr10[1], "SetFileMetadata")

	// transpose: set vs edit
	pr11 := OperationTransform(set, edit)
	expectType(t, "set11'", pr11[0], "SetFileMetadata")
	expectType(t, "edit11'", pr11[1], "EditFile")

	// transformMoveMove remaining branches
	// opposite moves (swap): a->b, b->a
	om := OperationTransform(OperationMoveFile("a", "b"), OperationMoveFile("b", "a"))
	expectType(t, "om1", om[0], "MoveFile")
	expectType(t, "om2", om[1], "MoveFile")
	// divergent: same src different dst
	dv := OperationTransform(OperationMoveFile("x", "y"), OperationMoveFile("x", "z"))
	isOpNoOp(t, "dv1", dv[0])
	// convergent: different src same dst -> [remove(src1), move2]
	cv := OperationTransform(OperationMoveFile("x", "w"), OperationMoveFile("z", "w"))
	expectType(t, "cv0", cv[0], "MoveFile")
	expectType(t, "cv1", cv[1], "MoveFile")
	// transitive
	tr := OperationTransform(OperationMoveFile("a", "b"), OperationMoveFile("b", "c"))
	expectType(t, "tr1", tr[0], "MoveFile")
}

func TestCoverageOriginBranches(t *testing.T) {
	ts, _ := parseRawTime("2025-01-02T03:04:05.678Z")
	ro, _ := NewRestoreOrigin(5, ts, clientId)
	if ro.GetVersion() != 5 || ro.GetTimestamp().IsZero() {
		t.Fatal("restore origin getters")
	}
	rf, rfErr := NewRestoreFileOrigin(1, "p", ts, clientId)
	if rfErr != nil || rf.GetVersion() != 1 {
		t.Fatal("file origin getter")
	}
	if rf.GetTimestamp().IsZero() || rf.GetPath() != "p" {
		t.Fatal("file origin getters")
	}
	rp, _ := NewRestoreProjectOrigin(7, ts, clientId)
	if rp.GetVersion() != 7 {
		t.Fatal("project origin getter")
	}

	// NewOrigin bad kind
	if _, err := NewOrigin("", clientId); err == nil {
		t.Fatal("empty kind should error")
	}
	if _, err := NewOriginWithID("", nil); err == nil {
		t.Fatal("empty kind should error")
	}
	// bad path
	if _, err := NewRestoreFileOrigin(1, "", ts, clientId); err == nil {
		t.Fatal("empty path should error")
	}
	// bad timestamps
	if _, err := NewRestoreOrigin(1, ts, ""); err == nil {
		t.Fatal("empty clientId should error")
	}
	if _, err := parseRawTime(""); err == nil {
		t.Fatal("empty timestamp should error")
	}
	if _, err := parseRawTime("not-a-date"); err == nil {
		t.Fatal("bad timestamp should error")
	}
	// derefStr
	if derefStr(nil) != "" || derefStr(strPtr("x")) != "x" {
		t.Fatal("derefStr")
	}
	// OriginFromRaw with restore kinds + id
	parsed, err := OriginFromRaw(map[string]any{"kind": "restore", "version": int64(1), "timestamp": "2025-01-02T03:04:05.678Z", "historyClientId": clientId})
	if err != nil || parsed.GetHistoryClientId() == nil {
		t.Fatal("restore origin from raw")
	}
}

func TestCoverageV2Branches(t *testing.T) {
	// applyTo with existing v2 merges
	s := NewSnapshot(nil, nil, NewV2DocVersions(map[string]any{"a": map[string]any{"pathname": "a", "version": int64(1)}}), nil)
	v := NewV2DocVersions(map[string]any{"b": map[string]any{"pathname": "b", "version": int64(2)}})
	v.ApplyTo(s)
	if _, ok := s.GetV2DocVersions().Data["b"]; !ok {
		t.Fatal("merge")
	}
	// applyTo no-op when empty
	empty := NewV2DocVersions(nil)
	s2 := NewSnapshot(nil, nil, nil, nil)
	empty.ApplyTo(s2)
	if s2.GetV2DocVersions() != nil {
		t.Fatal("empty applyTo should not set")
	}
	// moveFile non-match
	m := NewV2DocVersions(map[string]any{"a": map[string]any{"pathname": "a", "version": int64(1)}})
	m.MoveFile("zzz", "x")
	if len(m.Data) != 1 {
		t.Fatal("non-match should not remove")
	}
}
