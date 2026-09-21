package otc

import (
	"testing"
	"time"
)

const clientId = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

func TestOriginRoundTrip(t *testing.T) {
	// kind on its own
	o := &Origin{Kind: "dropbox"}
	sameRaw(t, "dropbox", o.ToRaw(), map[string]any{"kind": "dropbox"})
	if o.GetHistoryClientId() != nil {
		t.Fatalf("expected no historyClientId")
	}
	parsed, err := OriginFromRaw(o.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.GetKind() != "dropbox" {
		t.Fatalf("kind = %v", parsed.GetKind())
	}
	if to, ok := parsed.(*Origin); !ok || to.GetHistoryClientId() != nil {
		t.Fatalf("parsed = %T", parsed)
	}
}

func TestOriginWithClientId(t *testing.T) {
	o, err := NewOrigin(EditorOriginKind, clientId)
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "editor", o.ToRaw(), map[string]any{
		"kind": EditorOriginKind, "historyClientId": clientId,
	})
}

func TestOriginRejectsEmptyClientId(t *testing.T) {
	if _, err := NewOrigin(EditorOriginKind, ""); err == nil {
		t.Fatal("expected error for empty historyClientId")
	}
}

func TestRestoreFileOriginRoundTrip(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	o, err := NewRestoreFileOrigin(7, "main.tex", ts, clientId)
	if err != nil {
		t.Fatal(err)
	}
	sameRaw(t, "file-restore", o.ToRaw(), map[string]any{
		"kind":            "file-restore",
		"historyClientId": clientId,
		"version":         int64(7),
		"path":            "main.tex",
		"timestamp":       "2025-01-02T03:04:05.678Z",
	})
	parsed, err := OriginFromRaw(o.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	rf, ok := parsed.(*RestoreFileOrigin)
	if !ok {
		t.Fatalf("parsed = %T", parsed)
	}
	if rf.GetHistoryClientId() == nil || *rf.GetHistoryClientId() != clientId {
		t.Fatal("historyClientId mismatch")
	}
	if rf.GetPath() != "main.tex" {
		t.Fatalf("path = %v", rf.GetPath())
	}
}

func TestDropHistoryClientId(t *testing.T) {
	o, _ := NewOrigin(EditorOriginKind, clientId)
	o.DropHistoryClientId()
	if o.GetHistoryClientId() != nil {
		t.Fatal("expected nil id after drop")
	}
	sameRaw(t, "dropped", o.ToRaw(), map[string]any{"kind": EditorOriginKind})

	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	rf, _ := NewRestoreFileOrigin(7, "main.tex", ts, clientId)
	rf.DropHistoryClientId()
	sameRaw(t, "dropped-file", rf.ToRaw(), map[string]any{
		"kind": "file-restore", "version": int64(7), "path": "main.tex",
		"timestamp": "2025-01-02T03:04:05.678Z",
	})
}

func TestOriginChangeCarry(t *testing.T) {
	// through a Change
	raw := map[string]any{
		"operations": []any{},
		"timestamp":  "2025-01-02T03:04:05.678Z",
		"origin":     map[string]any{"kind": EditorOriginKind, "historyClientId": clientId},
	}
	c, err := ChangeFromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.GetOrigin().GetHistoryClientId() == nil || *c.GetOrigin().GetHistoryClientId() != clientId {
		t.Fatal("id not carried")
	}
	_, ok := c.GetOrigin().(*Origin)
	if !ok {
		t.Fatalf("origin type = %T", c.GetOrigin())
	}
	sameRaw(t, "change.origin", c.ToRaw()["origin"],
		map[string]any{"kind": EditorOriginKind, "historyClientId": clientId})
}

func TestRestoreOriginTypes(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	ro, _ := NewRestoreOrigin(3, ts, clientId)
	sameRaw(t, "restore", ro.ToRaw(), map[string]any{
		"kind": "restore", "historyClientId": clientId,
		"version": int64(3), "timestamp": "2025-01-02T03:04:05.678Z",
	})
	roParsed, err := OriginFromRaw(ro.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := roParsed.(*RestoreOrigin); !ok {
		t.Fatal("want *RestoreOrigin")
	}

	rpo, _ := NewRestoreProjectOrigin(9, ts, clientId)
	rpoParsed, err := OriginFromRaw(rpo.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rpoParsed.(*RestoreProjectOrigin); !ok {
		t.Fatal("want *RestoreProjectOrigin")
	}
}

func TestV2DocVersions(t *testing.T) {
	if V2DocVersionsFromRaw(nil) != nil {
		t.Fatal("nil raw -> nil")
	}
	data := map[string]any{
		"doc1": map[string]any{"pathname": "main.tex", "version": int64(5)},
	}
	v := NewV2DocVersions(data)
	if V2DocVersionsFromRaw(v.ToRaw()) == nil {
		t.Fatal("expected non-nil clone")
	}

	// applyTo: none -> sets
	s := NewSnapshot(nil, nil, nil, nil)
	v.ApplyTo(s)
	if s.GetV2DocVersions() == nil {
		t.Fatal("expected v2DocVersions set")
	}

	// moveFile: re-home
	data2 := map[string]any{
		"doc1": map[string]any{"pathname": "a.tex", "version": int64(1)},
		"doc2": map[string]any{"pathname": "b.tex", "version": int64(2)},
	}
	v2 := NewV2DocVersions(data2)
	v2.MoveFile("a.tex", "renamed.tex")
	if v2.Data["doc1"].(map[string]any)["pathname"] != "renamed.tex" {
		t.Fatal("moveFile did not re-home")
	}
	v2.MoveFile("b.tex", "")
	if _, exists := v2.Data["doc2"]; exists {
		t.Fatal("moveFile did not remove")
	}
}
