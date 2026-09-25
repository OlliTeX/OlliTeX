package collab

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/encoding"
	ysync "github.com/reearth/ygo/sync"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// --- fakes + fixtures ------------------------------------------------------// seedRoomPID — a valid 24-hex room name (= project id) for the seed tests.
const seedRoomPID = "66a00000000000000000dead"

type seedFakeProjects struct {
	doc  bson.D
	err  error
	seen []string // every requested room (test introspection)
}

func (f *seedFakeProjects) ProjectByID(_ context.Context, id string) (bson.D, error) {
	f.seen = append(f.seen, id)
	if f.err != nil {
		return nil, f.err
	}
	return f.doc, nil
}

// seedProjectDoc — a project doc in the live shape the web's file proxy
// reads: rootFolder = array of elements; element = {fileRefs, folders};
// overleaf.history.id carrying the blob store id (string or ObjectID).
func seedProjectDoc(root any, hid any) bson.D {
	return bson.D{
		{Key: "_id", Value: primitive.NewObjectID()},
		{Key: "rootFolder", Value: root},
		{Key: "overleaf", Value: bson.D{
			{Key: "history", Value: bson.D{{Key: "id", Value: hid}}},
		}},
	}
}

func fileRef(name, hash string) bson.D {
	return bson.D{
		{Key: "_id", Value: primitive.NewObjectID()},
		{Key: "name", Value: name},
		{Key: "hash", Value: hash},
	}
}

// seedBlobServer — a fake of the history-v1 blob API (GET
// /api/projects/{hid}/blobs/{hash}, basic auth) recording the last
// request. Bodies by hash; explicit statuses override (404/500 cases).
type seedBlobServer struct {
	*httptest.Server
	lastPath, lastUser, lastPass, lastAuth string
	bodies                                 map[string]string
	statusFor                              map[string]int
}

func newSeedBlobServer(t *testing.T, bodies map[string]string, statuses map[string]int) *seedBlobServer {
	t.Helper()
	s := &seedBlobServer{bodies: bodies, statusFor: statuses}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastPath = r.URL.Path
		s.lastUser, s.lastPass, _ = r.BasicAuth()
		s.lastAuth = r.Header.Get("Authorization")
		hash := strings.TrimPrefix(r.URL.Path, "/api/projects/hid1/blobs/")
		if st, ok := s.statusFor[hash]; ok {
			w.WriteHeader(st)
			return
		}
		if b, ok := s.bodies[hash]; ok {
			io.WriteString(w, b)
			return
		}
		http.NotFound(w, r)
	}))
	return s
}

func seedSrc(p SeedProjects, srv *seedBlobServer, user, pass string) *SeedSource {
	// Route through NewSeedSource so the production defaults (staging /
	// empty password) are what the tests verify.
	return NewSeedSource(p, srv.URL+"/api", user, pass, srv.Server.Client())
}

// readWSRoomText — joins a room over a real WebSocket (gorilla), performs
// the y-protocol sync handshake (server step1 → echo our step1 → server
// delivers step2), and returns the room's Y.Text (TEXT_TYPE). The shared
// initial-sync probe for seed-path tests.
func readWSRoomText(t *testing.T, base, room string) string {
	t.Helper()
	c, _, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL(base, room), http.Header{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(8 * time.Second))
	readFrame := func() []byte {
		t.Helper()
		_, raw, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		dec := encoding.NewDecoder(raw)
		msgType, err := dec.ReadVarUint()
		if err != nil || msgType != 0 {
			t.Fatalf("frame msgType = %d, %v; want sync(0)", msgType, err)
		}
		return dec.RemainingBytes()
	}
	doc := crdt.New()
	echoStep1 := func() {
		c.WriteMessage(websocket.BinaryMessage, encoding.EncodeBytes(func(enc *encoding.Encoder) {
			enc.WriteVarUint(0)
			enc.WriteRaw(ysync.EncodeSyncStep1(doc))
		}))
	}
	inner := readFrame()
	st, _, err := ysync.ReadSyncMessage(inner)
	if err != nil || st != ysync.MsgSyncStep1 {
		t.Fatalf("first sync = type %d, %v; want step1", st, err)
	}
	echoStep1()
	for i := 0; i < 3; i++ {
		inner = readFrame()
		st, _, err := ysync.ReadSyncMessage(inner)
		if err != nil {
			t.Fatalf("sync frame %d: %v", i, err)
		}
		if st == ysync.MsgSyncStep1 {
			echoStep1()
			continue
		}
		if _, err := ysync.ApplySyncMessage(doc, inner, "client-probe"); err != nil {
			t.Fatalf("apply %d: %v", st, err)
		}
		break
	}
	return doc.GetText(TextType).ToString()
}

// --- unit tests -------------------------------------------------------------

func TestSeedSourceMainTexPreferred(t *testing.T) {
	t.Parallel()
	// notes.tex appears FIRST in walk order; main.tex is nested — the
	// selection rule must still choose main.tex (the template contract:
	// POST /project/new seeds rootFolder main.tex).
	root := []any{
		bson.D{
			{Key: "fileRefs", Value: []any{fileRef("notes.tex", "h-notes")}},
			{Key: "folders", Value: []any{
				bson.D{{Key: "fileRefs", Value: []any{fileRef("main.tex", "h-main")}}},
			}},
		},
	}
	srv := newSeedBlobServer(t, map[string]string{"h-main": "MAIN", "h-notes": "NOTES"}, nil)
	defer srv.Close()
	p := &seedFakeProjects{doc: seedProjectDoc(root, "hid1")}

	got, err := seedSrc(p, srv, "staging", "secret").SeedText(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("SeedText: %v", err)
	}
	if got != "MAIN" {
		t.Fatalf("seed = %q, want main.tex content (MAIN)", got)
	}
	// Oracle-pinned wire expectations (the web's file-proxy contract):
	if srv.lastPath != "/api/projects/hid1/blobs/h-main" {
		t.Errorf("blob path = %q, want /api/projects/hid1/blobs/h-main", srv.lastPath)
	}
	if srv.lastUser != "staging" || srv.lastPass != "secret" {
		t.Errorf("basic auth = %q:%q, want staging:secret", srv.lastUser, srv.lastPass)
	}
	if !strings.HasPrefix(srv.lastAuth, "Basic ") {
		t.Errorf("Authorization = %q, want Basic …", srv.lastAuth)
	}
	if len(p.seen) != 1 || p.seen[0] != seedRoomPID {
		t.Errorf("projects lookups = %v, want [%s]", p.seen, seedRoomPID)
	}
}

func TestSeedSourceFirstTexFallback(t *testing.T) {
	t.Parallel()
	// No main.tex anywhere → the first .tex in walk order wins.
	root := []any{
		bson.D{{Key: "fileRefs", Value: []any{fileRef("a.tex", "h-a"), fileRef("b.tex", "h-b")}}},
	}
	srv := newSeedBlobServer(t, map[string]string{"h-a": "A", "h-b": "B"}, nil)
	defer srv.Close()
	p := &seedFakeProjects{doc: seedProjectDoc(root, "hid1")}

	got, err := seedSrc(p, srv, "", "").SeedText(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("SeedText: %v", err)
	}
	if got != "A" {
		t.Fatalf("seed = %q, want first .tex (A)", got)
	}
	// Web-contract auth defaults: user "staging", empty password.
	if srv.lastUser != "staging" || srv.lastPass != "" {
		t.Errorf("default auth = %q:%q, want staging:(empty)", srv.lastUser, srv.lastPass)
	}
}

func TestSeedSourceNoTextFileIsEmptySeed(t *testing.T) {
	t.Parallel()
	// A project whose content is only non-text (image) legitimately seeds
	// EMPTY — and must not even fetch a blob.
	root := []any{bson.D{{Key: "fileRefs", Value: []any{fileRef("image.png", "h-img")}}}}
	srv := newSeedBlobServer(t, map[string]string{"h-img": "PNG"}, nil)
	defer srv.Close()
	p := &seedFakeProjects{doc: seedProjectDoc(root, "hid1")}

	got, err := seedSrc(p, srv, "u", "p").SeedText(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("SeedText: %v (no-text-file must be a valid empty seed)", err)
	}
	if got != "" {
		t.Fatalf("seed = %q, want empty", got)
	}
	if srv.lastPath != "" {
		t.Errorf("no blob request expected, got %q", srv.lastPath)
	}
}

func TestSeedSourceMalformedRoomIsError(t *testing.T) {
	t.Parallel()
	s := &SeedSource{P: &seedFakeProjects{doc: seedProjectDoc(nil, "hid1")},
		Base: "http://127.0.0.1:1/api", User: "u", Pass: "p", HTTP: http.DefaultClient}
	if _, err := s.SeedText(context.Background(), "not-a-project"); !errors.Is(err, ErrSeedProject) {
		t.Fatalf("err = %v, want ErrSeedProject for a malformed room", err)
	}
}

func TestSeedSourceBlob404IsEmptySeed(t *testing.T) {
	t.Parallel()
	// The fileRef is in the tree but its blob is gone → honest empty seed.
	root := []any{bson.D{{Key: "fileRefs", Value: []any{fileRef("main.tex", "h-missing")}}}}
	srv := newSeedBlobServer(t, nil, map[string]int{"h-missing": http.StatusNotFound})
	defer srv.Close()
	p := &seedFakeProjects{doc: seedProjectDoc(root, "hid1")}

	got, err := seedSrc(p, srv, "u", "p").SeedText(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("404 should be an honest empty seed, got error: %v", err)
	}
	if got != "" {
		t.Fatalf("seed = %q, want empty", got)
	}
}

func TestSeedSourceFailureIsFailClosed(t *testing.T) {
	t.Parallel()
	root := []any{bson.D{{Key: "fileRefs", Value: []any{fileRef("main.tex", "h-flaky")}}}}

	t.Run("noProjectDoc", func(t *testing.T) {
		t.Parallel()
		p := &seedFakeProjects{doc: nil} // ProjectByID: (nil, nil)
		dummy := &SeedSource{P: p, Base: "http://127.0.0.1:1/api", User: "u", Pass: "p", HTTP: http.DefaultClient}
		_, err := dummy.SeedText(context.Background(), seedRoomPID)
		if !errors.Is(err, ErrSeedProject) {
			t.Fatalf("err = %v, want ErrSeedProject", err)
		}
	})

	t.Run("projectLookupError", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("mongo down")
		p := &seedFakeProjects{err: sentinel}
		dummy := &SeedSource{P: p, Base: "http://127.0.0.1:1/api", User: "u", Pass: "p", HTTP: http.DefaultClient}
		_, err := dummy.SeedText(context.Background(), seedRoomPID)
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want lookup sentinel", err)
		}
	})

	t.Run("blob500", func(t *testing.T) {
		t.Parallel()
		srv := newSeedBlobServer(t, nil, map[string]int{"h-flaky": http.StatusInternalServerError})
		defer srv.Close()
		p := &seedFakeProjects{doc: seedProjectDoc(root, "hid1")}
		_, err := seedSrc(p, srv, "u", "p").SeedText(context.Background(), seedRoomPID)
		if err == nil {
			t.Fatal("non-2xx/non-404 blob status must be an error (fail-closed)")
		}
	})

	t.Run("transportError", func(t *testing.T) {
		t.Parallel()
		p := &seedFakeProjects{doc: seedProjectDoc(root, "hid1")}
		s := &SeedSource{P: p, Base: "http://127.0.0.1:1/api", User: "u", Pass: "p",
			HTTP: &http.Client{Timeout: 150 * time.Millisecond}}
		_, err := s.SeedText(context.Background(), seedRoomPID)
		if err == nil {
			t.Fatal("transport failure must be an error (fail-closed)")
		}
	})
}

func TestSeedSourceObjectIDHistory(t *testing.T) {
	t.Parallel()
	// overleaf.history.id as a real ObjectID (both shapes occur in this
	// lineage) — the blob URL must carry its hex form.
	hid := primitive.NewObjectID()
	root := []any{bson.D{{Key: "fileRefs", Value: []any{fileRef("main.tex", "h-oid")}}}}
	srv := newSeedBlobServer(t, map[string]string{"h-oid": "OID"}, nil)
	defer srv.Close()
	// retarget the recorded path prefix to this ObjectID hex (the fake
	// server keys bodies by the trimmed hash, so the same fixture serves)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.lastPath = r.URL.Path
		if strings.HasSuffix(r.URL.Path, "/h-oid") {
			io.WriteString(w, "OID")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv2.Close()
	p := &seedFakeProjects{doc: seedProjectDoc(root, hid)}
	s := &SeedSource{P: p, Base: srv2.URL + "/api", User: "staging", Pass: "",
		HTTP: srv2.Client()}

	got, err := s.SeedText(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("SeedText: %v", err)
	}
	if got != "OID" {
		t.Fatalf("seed = %q, want OID", got)
	}
	want := "/api/projects/" + hid.Hex() + "/blobs/h-oid"
	if srv.lastPath != want {
		t.Errorf("blob path = %q, want %q", srv.lastPath, want)
	}
}

// TestSeedSourceWiredIntoService — the production path: Service.New with
// SeedFn = SeedSource.SeedText over a real (temp-dir) store and a real
// WebSocket client: the first peer must receive the seeded text during
// initial sync, and exactly ONE version (the seed) must be persisted.
func TestSeedSourceWiredIntoService(t *testing.T) {
	auth := &fakeAuth{sessionUID: "ownerA", projRole: map[string]Role{seedRoomPID: ReadWrite}}
	srv := newSeedBlobServer(t,
		map[string]string{"h-main": "% OlliTeX — seeded\n\\section{S4}\n"}, nil)
	defer srv.Close()
	p := &seedFakeProjects{doc: seedProjectDoc([]any{
		bson.D{{Key: "fileRefs", Value: []any{fileRef("main.tex", "h-main")}}},
	}, "hid1")}
	seed := &SeedSource{P: p, Base: srv.URL + "/api", User: "staging", Pass: "secret",
		HTTP: srv.Server.Client()}

	svc, err := New(Options{Auth: auth, DataDir: t.TempDir(), SeedFn: seed.SeedText})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	})
	ts := httptest.NewServer(svc)
	defer ts.Close()

	want := "% OlliTeX — seeded\n\\section{S4}\n"
	if got := readWSRoomText(t, ts.URL, seedRoomPID); got != want {
		t.Fatalf("first peer initial text = %q, want seeded blob content %q", got, want)
	}
	metas, err := svc.Store().ListVersions(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("versions = %d, want exactly 1 (the seed)", len(metas))
	}
}
