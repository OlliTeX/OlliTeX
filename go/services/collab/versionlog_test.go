package collab

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/reearth/ygo/persistence"
)

// fakeInner — a minimal VersionedPersistence that records appends.
type fakeInner struct {
	versions []persistence.Version
	failNext bool
	loadErr  error
}

func (f *fakeInner) AppendUpdate(ctx context.Context, room string, update []byte) (persistence.Version, error) {
	if f.failNext {
		f.failNext = false
		return 0, errors.New("fake append failure")
	}
	f.versions = append(f.versions, persistence.Version(len(f.versions)+1))
	return persistence.Version(len(f.versions)), nil
}
func (f *fakeInner) Load(ctx context.Context, room string) (persistence.LoadResult, error) {
	return persistence.LoadResult{}, f.loadErr
}
func (f *fakeInner) ListVersions(ctx context.Context, room string) ([]persistence.VersionMeta, error) {
	out := make([]persistence.VersionMeta, 0, len(f.versions))
	for v := len(f.versions); v >= 1; v-- {
		out = append(out, persistence.VersionMeta{Version: persistence.Version(v)})
	}
	return out, nil
}
func (f *fakeInner) GetUpdate(ctx context.Context, room string, v persistence.Version) ([]byte, persistence.VersionMeta, bool, error) {
	return nil, persistence.VersionMeta{}, false, nil
}
func (f *fakeInner) MaterializeAt(ctx context.Context, room string, v persistence.Version) ([]byte, error) {
	return []byte("state"), nil
}
func (f *fakeInner) CaptureSnapshot(ctx context.Context, room, name string, state []byte) (persistence.Version, error) {
	return 0, nil
}
func (f *fakeInner) RestoreSnapshot(ctx context.Context, room, name string) ([]byte, persistence.Version, bool, error) {
	return nil, 0, false, nil
}
func (f *fakeInner) PruneAfter(ctx context.Context, room string, target persistence.Version, rolledBack []byte) error {
	return nil
}
func (f *fakeInner) Compact(ctx context.Context, room string, keep int) (int, error) {
	return 0, nil
}
func (f *fakeInner) Delete(ctx context.Context, room string) error {
	return nil
}

func TestActorLogRecordsActorOrigin(t *testing.T) {
	log := NewMemVersionLog()
	inner := &fakeInner{}
	al := &ActorLog{Inner: inner, Log: log, Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
	ctx := WithActor(context.Background(), "uid42")
	ctx = WithOrigin(ctx, "file-restore")
	ctx = WithSource(ctx, "x")

	v1, err := al.AppendUpdate(ctx, "room1", []byte("a"))
	if err != nil || v1 != 1 {
		t.Fatalf("append1 = %v %v", v1, err)
	}
	// No actor in this ctx -> uid recorded empty.
	v2, err := al.AppendUpdate(context.Background(), "room1", []byte("b"))
	if err != nil || v2 != 2 {
		t.Fatalf("append2 = %v %v", v2, err)
	}

	rng, err := log.Range(context.Background(), "room1", 0, 10)
	if err != nil || len(rng) != 2 {
		t.Fatalf("range = %v err=%v", rng, err)
	}
	if rng[0].UID != "uid42" || rng[0].Origin != "file-restore" || rng[0].Source != "x" {
		t.Fatalf("v1 meta = %+v", rng[0])
	}
	if rng[0].At != time.Unix(1700000000, 0).UTC() {
		t.Fatalf("v1 at = %v", rng[0].At)
	}
	if rng[1].UID != "" || rng[1].Origin != "" {
		t.Fatalf("v2 meta (want empty) = %+v", rng[1])
	}
	meta, ok, err := log.Get(context.Background(), "room1", 2)
	if err != nil || !ok || meta.V != 2 {
		t.Fatalf("get = %+v ok=%v err=%v", meta, ok, err)
	}
	if _, ok, _ := log.Get(context.Background(), "room1", 9); ok {
		t.Fatalf("get(9) should be absent")
	}
}

func TestActorLogInnerErrorPropagatesUnrecorded(t *testing.T) {
	log := NewMemVersionLog()
	inner := &fakeInner{failNext: true}
	al := &ActorLog{Inner: inner, Log: log}
	v, err := al.AppendUpdate(WithActor(context.Background(), "u"), "room1", []byte("a"))
	if err == nil {
		t.Fatalf("expected inner error")
	}
	if v != 0 {
		t.Fatalf("v = %d, want 0", v)
	}
	if r, _ := log.Range(context.Background(), "room1", 0, 10); len(r) != 0 {
		t.Fatalf("range = %v, want empty", r)
	}
}

type failingLog struct{}

func (failingLog) Upsert(ctx context.Context, m VersionMeta) error { return errors.New("log down") }
func (failingLog) Range(ctx context.Context, room string, from, to uint64) ([]VersionMeta, error) {
	return nil, nil
}
func (failingLog) Get(ctx context.Context, room string, v uint64) (VersionMeta, bool, error) {
	return VersionMeta{}, false, nil
}

func TestActorLogFailSoft(t *testing.T) {
	inner := &fakeInner{}
	sawErr := false
	al := &ActorLog{Inner: inner, Log: failingLog{}, ErrLog: func(err error) { sawErr = true }}
	v, err := al.AppendUpdate(WithActor(context.Background(), "u"), "room1", []byte("a"))
	if err != nil {
		t.Fatalf("append must succeed despite log failure: %v", err)
	}
	if v != 1 {
		t.Fatalf("v = %d want 1", v)
	}
	if !sawErr {
		t.Fatalf("ErrLog sink not called")
	}
}

func TestWrapIdempotent(t *testing.T) {
	inner := &fakeInner{}
	log := NewMemVersionLog()
	log2 := NewMemVersionLog()
	al := &ActorLog{Inner: inner, Log: log}
	wrapped := Wrap(al, log2)
	if wrapped != al {
		t.Fatalf("Wrap(ActorLog) should return the same *ActorLog")
	}
	if al.Log != log2 {
		t.Fatalf("existing wrapper's Log should be refreshed")
	}
	plain := Wrap(inner, log)
	if _, ok := plain.(*ActorLog); !ok {
		t.Fatalf("Wrap(plain) should return a *ActorLog")
	}
	if Wrap(inner, nil) != inner {
		t.Fatalf("Wrap(log=nil) returns input as-is")
	}
}

// Delegation sanity: Load passes through.
func TestActorLogDelegatesLoad(t *testing.T) {
	inner := &fakeInner{loadErr: errors.New("up")}
	al := &ActorLog{Inner: inner}
	_, err := al.Load(context.Background(), "r")
	if err == nil {
		t.Fatalf("load error must propagate")
	}
	inner2 := &fakeInner{}
	al2 := &ActorLog{Inner: inner2}
	if _, err := al2.Load(context.Background(), "r"); err != nil {
		t.Fatalf("clean load: %v", err)
	}
}
