// C9 oracle — LabelsManager L1–L7.
package labelsmanager

import (
	"context"
	"errors"
	"testing"
)

var cctx = context.Background()

type fakeLabels struct {
	docs     []map[string]any
	inserted any
	findErr  error
	delErr   error
	del      map[string]any
	udp      map[string]any
	findFn   func(filter, projection map[string]any) ([]map[string]any, error)
}

func (f *fakeLabels) Find(ctx context.Context, filter, projection map[string]any) ([]map[string]any, error) {
	if f.findFn != nil {
		return f.findFn(filter, projection)
	}
	return f.docs, f.findErr
}

func (f *fakeLabels) InsertMany(ctx context.Context, docs []map[string]any) error {
	f.docs = append(f.docs, docs...)
	return nil
}

func (f *fakeLabels) InsertOne(ctx context.Context, doc map[string]any) (any, error) {
	f.docs = append(f.docs, doc)
	return f.inserted, nil
}

func (f *fakeLabels) DeleteOne(ctx context.Context, filter map[string]any) error {
	f.del = filter
	return f.delErr
}

func (f *fakeLabels) UpdateMany(ctx context.Context, filter, set map[string]any) error {
	f.udp = set
	_ = filter
	return nil
}

func TestC9_CloneLabels(t *testing.T) {
	f := &fakeLabels{docs: []map[string]any{
		{"comment": "a", "version": 1, "created_at": "t"},
		{"comment": "b", "version": 2, "created_at": "t"},
	}}
	d := &Deps{Labels: f}
	if err := d.CloneLabels(cctx, "src", "dst"); err != nil {
		t.Fatal(err)
	}
	if len(f.docs) != 4 {
		t.Fatalf("docs after clone: %d (want 4)", len(f.docs))
	}
	last := f.docs[3]
	if last["project_id"] != "dst" || last["comment"] != "b" {
		t.Fatalf("cloned doc: %#v", last)
	}
	// L1: no labels → no insert
	f2 := &fakeLabels{docs: nil}
	d2 := &Deps{Labels: f2}
	if err := d2.CloneLabels(cctx, "s", "d"); err != nil {
		t.Fatal(err)
	}
	if len(f2.docs) != 0 {
		t.Fatal("absent → no insert")
	}
}

func TestC9_GetLabelsFormat(t *testing.T) {
	f := &fakeLabels{docs: []map[string]any{
		{"_id": "L1", "comment": "hello", "version": 7, "user_id": "u1", "created_at": "2025-01-01T00:00:00Z", "project_id": "p", "extra": "dropped"},
	}}
	d := &Deps{Labels: f}
	got, err := d.GetLabels(cctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	l := got[0]
	if l["id"] != "L1" || l["comment"] != "hello" || l["version"] != 7 || l["user_id"] != "u1" {
		t.Fatalf("format: %#v", l)
	}
	if _, has := l["extra"]; has {
		t.Fatalf("extra key must not leak through _formatLabel: %#v", l)
	}
}

func TestC9_CreateLabelShapes(t *testing.T) {
	f := &fakeLabels{inserted: "NEWID"}
	d := &Deps{Labels: f}
	got, err := d.CreateLabel(cctx, "p", "u1", 5, "note", "2025-07-01T00:00:00Z", false)
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "NEWID" || got["comment"] != "note" || got["version"] != 5 {
		t.Fatalf("shape: %#v", got)
	}
	if got["created_at"] != "2025-07-01T00:00:00Z" {
		t.Fatalf("created_at: %v", got["created_at"])
	}
	// L3: no userId → the key is ABSENT
	f2 := &fakeLabels{inserted: "I2"}
	d2 := &Deps{Labels: f2, Now: func() any { return "NOW" }}
	_, err = d2.CreateLabel(cctx, "p", "", 1, "c", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	doc := f2.docs[len(f2.docs)-1]
	if _, has := doc["user_id"]; has {
		t.Fatalf("falsy userId must omit the key: %#v", doc)
	}
	if doc["created_at"] != "NOW" {
		t.Fatalf("null createdAt → new Date(): %#v", doc)
	}
}

func TestC9_CreateLabelValidationChain(t *testing.T) {
	var order []string
	d := &Deps{
		ProcessUpdates: func(ctx context.Context, p string) error {
			order = append(order, "process")
			return nil
		},
		GetHistoryId: func(ctx context.Context, p string) (string, error) {
			order = append(order, "hid")
			return "hid-9", nil
		},
		GetChunkAtVersion: func(ctx context.Context, p, hid string, v int) (map[string]any, error) {
			order = append(order, "chunk:"+hid)
			return map[string]any{"chunk": "c"}, nil
		},
	}
	f := &fakeLabels{inserted: "X"}
	d.Labels = f
	if _, err := d.CreateLabel(cctx, "p", "u", 3, "c", nil, true); err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != "process" || order[1] != "hid" || order[2] != "chunk:hid-9" {
		t.Fatalf("chain: %v", order)
	}
}

func TestC9_ValidationErrorAtEachStep(t *testing.T) {
	boom := errors.New("step")
	d := &Deps{ProcessUpdates: func(ctx context.Context, p string) error { return boom }, Labels: &fakeLabels{inserted: "x"}}
	if _, err := d.CreateLabel(cctx, "p", "u", 3, "c", nil, true); err != boom {
		t.Fatalf("process step: %v", err)
	}
	d2 := &Deps{
		ProcessUpdates: func(ctx context.Context, p string) error { return nil },
		GetHistoryId:   func(ctx context.Context, p string) (string, error) { return "", boom },
		Labels:         &fakeLabels{inserted: "x"},
	}
	if _, err := d2.CreateLabel(cctx, "p", "u", 3, "c", nil, true); err != boom {
		t.Fatalf("hid step: %v", err)
	}
	d3 := &Deps{
		ProcessUpdates:    func(ctx context.Context, p string) error { return nil },
		GetHistoryId:      func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion: func(ctx context.Context, p, hid string, v int) (map[string]any, error) { return nil, boom },
		Labels:            &fakeLabels{inserted: "x"},
	}
	if _, err := d3.CreateLabel(cctx, "p", "u", 3, "c", nil, true); err != boom {
		t.Fatalf("chunk step: %v", err)
	}
}

func TestC9_DeletesAndTransfer(t *testing.T) {
	f := &fakeLabels{}
	d := &Deps{Labels: f}
	if err := d.DeleteLabelForUser(cctx, "p", "u", "L"); err != nil {
		t.Fatal(err)
	}
	if f.del["_id"] != "L" || f.del["project_id"] != "p" || f.del["user_id"] != "u" {
		t.Fatalf("filter: %#v", f.del)
	}
	if err := d.DeleteLabel(cctx, "p", "L"); err != nil {
		t.Fatal(err)
	}
	if _, has := f.del["user_id"]; has {
		t.Fatalf("L5: no user_id key: %#v", f.del)
	}
	if err := d.TransferLabels(cctx, "from", "to"); err != nil {
		t.Fatal(err)
	}
	if f.udp["user_id"] != "to" {
		t.Fatalf("update: %#v", f.udp)
	}
	// L7: falsy userId → filter omits user_id
	if err := d.DeleteLabelForUser(cctx, "p", "", "L"); err != nil {
		t.Fatal(err)
	}
	if _, has := f.del["user_id"]; has {
		t.Fatalf("L7: %#v", f.del)
	}
}

func TestC9_StorageErrorsPropagate(t *testing.T) {
	boom := errors.New("mongo down")
	f := &fakeLabels{findErr: boom, delErr: boom}
	d := &Deps{Labels: f}
	if _, err := d.GetLabels(cctx, "p"); err != boom {
		t.Fatal("find err")
	}
	if err := d.CloneLabels(cctx, "s", "d"); err != boom {
		t.Fatal("clone find err")
	}
	if err := d.DeleteLabel(cctx, "p", "L"); err != boom {
		t.Fatal("delete err")
	}
	f3 := &fakeLabels{}
	f3.delErr = boom
	if err := (&Deps{Labels: f3}).DeleteLabelForUser(cctx, "p", "u", "L"); err != boom {
		t.Fatal("deleteForUser err")
	}
}
