package collab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// NewMemStore — in-memory versioned store for the 024 Option B tests.
func NewMemStore() persistence.VersionedPersistence {
	return persistence.NewMemoryPersistence()
}

// bsonD / bsonE — short aliases for the test fixtures.
type bsonD = bson.D
type bsonE = bson.E

// wsDefaultDial — gorilla dial against a room URL (resp retained for 401 checks).
func wsDefaultDial(url string) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.DialContext(context.Background(), url, http.Header{})
}

// 024 Option B — per-doc rooms: seed, auth and write-through behavior.

var bdocRoom = seedRoomPID + "-" + "66a00000000000000000bbbb"

func TestSeedPerDocRoom(t *testing.T) {
	srv := newSeedDocServer(t, "bib content\n")
	// project with the per-doc in its file tree (the legitimate shape)
	p := &seedFakeProjects{doc: seedProjectDocWithFolder("66a00000000000000000bbbb")}
	s := seedSrc(p, srv, "", "")
	got, err := s.SeedText(context.Background(), bdocRoom)
	if err != nil {
		t.Fatalf("SeedText(per-doc): %v", err)
	}
	if got != "bib content\n" {
		t.Fatalf("seed = %q, want the PER-DOC docstore lines", got)
	}
	// the doc id in the room name must drive the docstore fetch (not the
	// project's rootDoc_id)
	want := "/project/" + seedRoomPID + "/doc/66a00000000000000000bbbb"
	if len(srv.gotPaths) != 1 || srv.gotPaths[0] != want {
		t.Fatalf("paths = %v, want [%s] (per-doc seed)", srv.gotPaths, want)
	}
}

func TestSeedPerDocRoomForeignDocIsEmpty(t *testing.T) {
	// a room name carrying a doc id that is NOT the project's and not in
	// the tree: seeds nothing (no cross-project leakage path).
	srv := newSeedDocServer(t, "foreign content")
	// no rootFolder in the fake project doc at all → tree check fails
	p := &seedFakeProjects{doc: seedProjectDoc("66a00000000000000000aaaa")}
	s := seedSrc(p, srv, "", "")
	got, err := s.SeedText(context.Background(), bdocRoom)
	if err != nil {
		t.Fatalf("SeedText(foreign doc): %v", err)
	}
	if got != "" || len(srv.gotPaths) != 0 {
		t.Fatalf("seed=%q paths=%v — a foreign doc must seed nothing without a fetch", got, srv.gotPaths)
	}
}

func TestSeedPerDocRoomInTree(t *testing.T) {
	srv := newSeedDocServer(t, "nested doc\n")
	// project with a rootFolder array carrying the doc (the live lineage shape)
	p := &seedFakeProjects{doc: seedProjectDocWithFolder("66a00000000000000000bbbb")}
	s := seedSrc(p, srv, "", "")
	got, err := s.SeedText(context.Background(), bdocRoom)
	if err != nil {
		t.Fatalf("SeedText(in-tree): %v", err)
	}
	if got != "nested doc\n" {
		t.Fatalf("seed = %q, want the in-tree doc content", got)
	}
}

// seedProjectDocWithFolder — a projects doc whose rootFolder array wraps
// one folder whose docs[] contains the given doc id (live "Test 2" shape).
func seedProjectDocWithFolder(docID string) bsonD {
	d := bsonD{
		{Key: "_id", Value: ooid(seedRoomPID)},
		{Key: "name", Value: "seed-probe"},
	}
	d = append(d, bsonE{Key: "rootFolder", Value: []any{
		bsonD{
			{Key: "name", Value: "rootFolder"},
			{Key: "docs", Value: []any{
				bsonD{{Key: "name", Value: "main.tex"}, {Key: "_id", Value: "66a00000000000000000aaaa"}},
				bsonD{{Key: "name", Value: "sample.bib"}, {Key: "_id", Value: docID}},
			}},
		},
	}})
	return d
}

// ---- auth over per-doc rooms ------------------------------------------------

func TestAuthorizePerDocRoomUsesProjectRole(t *testing.T) {
	// owner of the project must get the per-doc room (same role surface)
	auth := &fakeAuth{
		sessionUID: "ownerA",
		projRole:   map[string]Role{seedRoomPID: ReadWrite},
	}
	svc := newTestService(t, auth)
	ts := httptest.NewServer(svc)
	defer ts.Close()
	c, _, err := wsDefaultDial(wsURL(ts.URL, bdocRoom))
	if err != nil {
		t.Fatalf("owner dial to per-doc room failed (must reuse the project role): %v", err)
	}
	_ = c.Close()
}

func TestAuthorizePerDocRoomDeniesNonMember(t *testing.T) {
	auth := &fakeAuth{
		sessionUID: "stranger",
		projRole:   map[string]Role{},
	}
	svc := newTestService(t, auth)
	ts := httptest.NewServer(svc)
	defer ts.Close()
	_, resp, err := wsDefaultDial(wsURL(ts.URL, bdocRoom))
	if err == nil {
		t.Fatal("stranger dial to per-doc room SUCCEEDED — must be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %v (resp %+v)", err, resp)
	}
}

// ---- write-through ----------------------------------------------------------

// fakeFetcher — DocFetcher fake with observable writes.
type fakeFetcher struct {
	existing map[string]DocView   // project/doc → current docstore view
	writes   []fetchedWrite
	getErr   error
	putErr   error
}

type fetchedWrite struct {
	Pid   string
	Did   string
	Lines []string
	Ver   int
}

func (f *fakeFetcher) key(pid, did string) string { return pid + "/" + did }
func (f *fakeFetcher) GetDoc(_ context.Context, pid, did string) (DocView, error) {
	if f.getErr != nil {
		return DocView{}, f.getErr
	}
	v, ok := f.existing[f.key(pid, did)]
	if !ok {
		return DocView{}, ErrDocNotFound
	}
	return v, nil
}
func (f *fakeFetcher) PutDoc(_ context.Context, pid, did string, dv DocView) (DocView, error) {
	if f.putErr != nil {
		return dv, f.putErr
	}
	f.writes = append(f.writes, fetchedWrite{Pid: pid, Did: did, Lines: dv.Lines, Ver: dv.Version})
	f.existing[f.key(pid, did)] = dv
	return dv, nil
}

func TestWriteBackPerDocRoomWritesHead(t *testing.T) {
	st := NewMemStore() // in-memory versioned store
	room := bdocRoom
	// v1 seed + v2 edit (the client-edit shape roomdoc.ClientEdit uses)
	if _, err := SeedTextContent(context.Background(), st, room, "line one\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := ClientEdit(context.Background(), st, room, "line one\n", "line one\nline two\n"); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{existing: map[string]DocView{}}
	wb := NewWriteBack(f, st, nil)
	wb.Delay = 0 // immediate deterministic writes
	wb.FlushRoom(context.Background(), room)
	if len(f.writes) != 1 {
		t.Fatalf("writes = %d, want exactly 1; %v", len(f.writes), f.writes)
	}
	w := f.writes[0]
	if w.Pid != seedRoomPID || w.Did != "66a00000000000000000bbbb" {
		t.Fatalf("wrote (pid=%q did=%q), want (pid=%q did=%q)", w.Pid, w.Did, seedRoomPID, "66a00000000000000000bbbb")
	}
	if got := joinLines(w.Lines); got != "line one\nline two\n" {
		t.Fatalf("lines = %q, want the room HEAD text split on newlines", got)
	}
	if w.Ver != 2 {
		t.Fatalf("version = %d, want the room head version 2 (monotone, ≥ stale-reject bound)", w.Ver)
	}
}

func TestWriteBackRootRoomUsesRootDoc(t *testing.T) {
	st := NewMemStore()
	room := seedRoomPID
	if _, err := SeedTextContent(context.Background(), st, room, "root doc text\n"); err != nil {
		t.Fatal(err)
	}
	f := &fakeFetcher{existing: map[string]DocView{}}
	const root = "66a00000000000000000aaaa"
	wb := NewWriteBack(f, st, func(_ context.Context, pid string) (string, error) {
		if pid != seedRoomPID {
			t.Fatalf("resolver called for %q, want %q", pid, seedRoomPID)
		}
		return root, nil
	})
	wb.Delay = 0
	wb.FlushRoom(context.Background(), room)
	if len(f.writes) != 1 || f.writes[0].Did != root {
		t.Fatalf("writes = %v, want the root doc %q", f.writes, root)
	}
}

func TestWriteBackPreservesRanges(t *testing.T) {
	st := NewMemStore()
	room := bdocRoom
	if _, err := SeedTextContent(context.Background(), st, room, "keep me\n"); err != nil {
		t.Fatal(err)
	}
	k := seedRoomPID + "/66a00000000000000000bbbb"
	f := &fakeFetcher{existing: map[string]DocView{
		k: {Lines: []string{"old"}, Version: 1, Ranges: map[string]any{"comment-a": map[string]any{"start": 0}}},
	}}
	wb := NewWriteBack(f, st, nil)
	wb.Delay = 0
	wb.FlushRoom(context.Background(), room)
	if len(f.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(f.writes))
	}
	stored := f.existing[k]
	if _, ok := stored.Ranges["comment-a"]; !ok {
		t.Fatalf("stored ranges = %v, the docstore's comment ranges must be preserved", stored.Ranges)
	}
}

func TestWriteBackEmptyRoomSkips(t *testing.T) {
	st := NewMemStore()
	f := &fakeFetcher{existing: map[string]DocView{}}
	wb := NewWriteBack(f, st, nil)
	wb.Delay = 0
	wb.FlushRoom(context.Background(), bdocRoom)
	if len(f.writes) != 0 {
		t.Fatalf("writes = %d, want 0 (a room with no versions has no content)", len(f.writes))
	}
}

func TestWriteBackFailSoft(t *testing.T) {
	st := NewMemStore()
	if _, err := SeedTextContent(context.Background(), st, bdocRoom, "x\n"); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("docstore down")
	f := &fakeFetcher{existing: map[string]DocView{}, getErr: sentinel}
	var sinkErr error
	wb := NewWriteBack(f, st, nil)
	wb.Delay = 0
	wb.ErrSink = func(err error) { sinkErr = err }
	wb.FlushRoom(context.Background(), bdocRoom) // must NOT panic
	if sinkErr == nil || !errors.Is(sinkErr, sentinel) {
		t.Fatalf("sink = %v, want the docstore error surfaced (fail-soft, logged)", sinkErr)
	}
}

func TestWriteBackLogDecoratorSchedules(t *testing.T) {
	st := NewMemStore()
	f := &fakeFetcher{existing: map[string]DocView{}}
	wb := NewWriteBack(f, st, nil)
	wb.Delay = 50 * time.Millisecond // short debounce for the test
	wrapped := WrapWriteBack(st, wb)
	if _, err := SeedTextContent(context.Background(), wrapped, bdocRoom, "debounced\n"); err != nil {
		t.Fatal(err)
	}
	// before the debounce fires: nothing written yet
	if len(f.writes) != 0 {
		t.Fatalf("writes fired before the debounce window: %v", f.writes)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(f.writes) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(f.writes) != 1 {
		t.Fatalf("writes = %d, want 1 after the debounce window; %v", len(f.writes), f.writes)
	}
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

