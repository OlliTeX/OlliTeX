package collab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// seedRoomPID / seedRoomPID2 — 24-hex project ids (the room names).
const (
	seedRoomPID  = "66a00000000000000000dead"
	seedRoomPID2 = "66a00000000000000000beef"
)

// seedText — the docstore lines joined (the live-oracle shape: a rendered
// mainbasic.tex with a trailing blank line).
const seedDocText = "% OlliTeX — seeded\n\\begin{document}\n42\n\\end{document}\n"

// ooid — 24-hex → ObjectID (test helper; mongo-driver v1 has no Must*).
func ooid(h string) primitive.ObjectID {
	o, err := primitive.ObjectIDFromHex(h)
	if err != nil {
		panic(err)
	}
	return o
}

type seedFakeProjects struct {
	doc bson.D // (nil, nil) when the "doc" field is absent AND noDoc=false
	err error
}

func (f *seedFakeProjects) ProjectByID(_ context.Context, id string) (bson.D, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.doc == nil {
		return nil, nil
	}
	return f.doc, nil
}

// seedProjectDoc — a projects doc with the given rootDoc_id (ObjectID or
// string) — the live lineage shape (rootFolder is ABSENT in the docstore
// model).
func seedProjectDoc(rootDocID any) bson.D {
	d := bson.D{
		{Key: "_id", Value: ooid(seedRoomPID)},
		{Key: "name", Value: "seed-probe"},
	}
	if rootDocID != nil {
		d = append(d, bson.E{Key: "rootDoc_id", Value: rootDocID})
	}
	return d
}

// seedDocServer — a fake of the docstore document API:
// GET /project/{pid}/doc/{did} → {"lines":[...]} (200) or the per-did
// status (bodies map: did → text; statuses map: did → status code).
type seedDocServer struct {
	t         *testing.T
	base      string
	gotAuth   http.Header
	gotPaths  []string
	statusFor map[string]int
	ts        *httptest.Server
}

func newSeedDocServer(t *testing.T, body string) *seedDocServer {
	t.Helper()
	s := &seedDocServer{t: t, statusFor: map[string]int{}}
	s.t = t
	mux := http.NewServeMux()
	mux.HandleFunc("/project/", func(w http.ResponseWriter, r *http.Request) {
		s.gotAuth = r.Header.Clone()
		s.gotPaths = append(s.gotPaths, r.URL.Path)
		// did = last path segment
		seg := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if st, ok := s.statusFor[seg]; ok {
			w.WriteHeader(st)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		lines := strings.Split(body, "\n") // TRAILING "" element preserved (live docstore shape)
		if body == "" {
			lines = []string{} // zero-line doc
		}
		w.Write([]byte(`{"_id":"` + seg + `","lines":[` + jsonLineList(lines) + `],"rev":1,"version":0,"ranges":{}}`))
	})
	srv := httptest.NewServer(mux)
	s.ts = srv
	t.Cleanup(srv.Close)
	s.base = srv.URL
	return s
}

// Close stops the backing httptest server (for transport-failure tests).
func (s *seedDocServer) Close() { s.ts.Close() }

func jsonLineList(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(l) + `"`)
	}
	return b.String()
}

func seedSrc(p SeedProjects, srv *seedDocServer, user, pass string) *SeedSource {
	return &SeedSource{P: p, Base: srv.base, User: user, Pass: pass, HTTP: http.DefaultClient}
}

// ---- content --------------------------------------------------------------

func TestSeedFromDocstoreLines(t *testing.T) {
	srv := newSeedDocServer(t, seedDocText)
	p := &seedFakeProjects{doc: seedProjectDoc("66a00000000000000000aaaa")}
	s := seedSrc(p, srv, "", "")
	got, err := s.SeedText(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("SeedText: %v", err)
	}
	if got != seedDocText {
		t.Fatalf("seed = %q, want the joined docstore lines %q", got, seedDocText)
	}
	// the EXACT path the docstore document API uses (pid hex + doc id)
	want := "/project/" + seedRoomPID + "/doc/66a00000000000000000aaaa"
	if len(srv.gotPaths) != 1 || srv.gotPaths[0] != want {
		t.Fatalf("paths seen = %v, want [%s]", srv.gotPaths, want)
	}
}

func TestSeedEmptyLinesIsEmpty(t *testing.T) {
	// a doc that exists with zero lines → empty seed (a state, not an error)
	srv := newSeedDocServer(t, "")
	p := &seedFakeProjects{doc: seedProjectDoc("66a00000000000000000bbbb")}
	s := seedSrc(p, srv, "", "")
	got, err := s.SeedText(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("SeedText: %v", err)
	}
	if got != "" {
		t.Fatalf("seed = %q, want empty", got)
	}
}

func TestSeedNoRootDocIsEmpty(t *testing.T) {
	// a project doc WITHOUT rootDoc_id has no content to seed
	srv := newSeedDocServer(t, "should not even be fetched")
	p := &seedFakeProjects{doc: seedProjectDoc(nil)}
	s := seedSrc(p, srv, "", "")
	got, err := s.SeedText(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("SeedText: %v", err)
	}
	if got != "" || len(srv.gotPaths) != 0 {
		t.Fatalf("seed=%q paths=%v — no docstore fetch is expected", got, srv.gotPaths)
	}
}

// ---- rootDoc_id shapes ------------------------------------------------------

func TestSeedRootDocObjectID(t *testing.T) {
	did := ooid("66a00000000000000000cccc")
	srv := newSeedDocServer(t, "x")
	p := &seedFakeProjects{doc: seedProjectDoc(did)}
	s := seedSrc(p, srv, "", "")
	if _, err := s.SeedText(context.Background(), seedRoomPID); err != nil {
		t.Fatalf("SeedText: %v", err)
	}
	if want := "/project/" + seedRoomPID + "/doc/" + did.Hex(); len(srv.gotPaths) != 1 || srv.gotPaths[0] != want {
		t.Fatalf("paths = %v, want [%s]", srv.gotPaths, want)
	}
}

// ---- auth parity ------------------------------------------------------------

func TestSeedBasicAuthWhenConfigured(t *testing.T) {
	srv := newSeedDocServer(t, "x")
	p := &seedFakeProjects{doc: seedProjectDoc("66a00000000000000000dddd")}
	s := seedSrc(p, srv, "staging", "pw")
	if _, err := s.SeedText(context.Background(), seedRoomPID); err != nil {
		t.Fatalf("SeedText: %v", err)
	}
	if got := srv.gotAuth.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
		t.Fatalf("Authorization = %q, want Basic … (parity with the file-proxy style)", got)
	}
	// and ABSENT when no user is configured (internal service needs none)
	srv2 := newSeedDocServer(t, "x")
	s2 := seedSrc(&seedFakeProjects{doc: seedProjectDoc("66a00000000000000000dddd")}, srv2, "", "")
	if _, err := s2.SeedText(context.Background(), seedRoomPID); err != nil {
		t.Fatalf("SeedText: %v", err)
	}
	if got := srv2.gotAuth.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want absent", got)
	}
}

// ---- failure semantics -------------------------------------------------------

func TestSeedMalformedRoomIsError(t *testing.T) {
	srv := newSeedDocServer(t, "x")
	s := seedSrc(&seedFakeProjects{}, srv, "", "")
	for _, bad := range []string{"", "short", "zzz000000000000000000000", "../../etc"} {
		if _, err := s.SeedText(context.Background(), bad); !errors.Is(err, ErrSeedProject) {
			t.Fatalf("room %q: err = %v, want ErrSeedProject", bad, err)
		}
	}
	if len(srv.gotPaths) != 0 {
		t.Fatalf("no docstore fetch is expected, got %v", srv.gotPaths)
	}
}

func TestSeedNoProjectDocIsError(t *testing.T) {
	srv := newSeedDocServer(t, "x")
	s := seedSrc(&seedFakeProjects{doc: nil}, srv, "", "")
	if _, err := s.SeedText(context.Background(), seedRoomPID); !errors.Is(err, ErrSeedProject) {
		t.Fatalf("err = %v, want ErrSeedProject (fail-closed: the gate proved membership)", err)
	}
}

func TestSeedProjectReadErrorPropagates(t *testing.T) {
	sentinel := errors.New("mongo unavailable")
	s := seedSrc(&seedFakeProjects{err: sentinel}, newSeedDocServer(t, "x"), "", "")
	if _, err := s.SeedText(context.Background(), seedRoomPID); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the projects-store error to propagate", err)
	}
}

func TestSeedDocstore404IsEmpty(t *testing.T) {
	srv := newSeedDocServer(t, "unused")
	did := "66a00000000000000000eeee"
	srv.statusFor[did] = http.StatusNotFound
	s := seedSrc(&seedFakeProjects{doc: seedProjectDoc(did)}, srv, "", "")
	got, err := s.SeedText(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("SeedText: %v (404 must read as empty, not error)", err)
	}
	if got != "" {
		t.Fatalf("seed = %q, want empty", got)
	}
}

func TestSeedFailureIsFailClosed(t *testing.T) {
	// 5xx
	srv := newSeedDocServer(t, "unused")
	did := "66a00000000000000000ffff"
	srv.statusFor[did] = http.StatusInternalServerError
	s := seedSrc(&seedFakeProjects{doc: seedProjectDoc(did)}, srv, "", "")
	if _, err := s.SeedText(context.Background(), seedRoomPID); err == nil {
		t.Fatal("docstore 500 must fail the seed (a room seeded from a failed read diverges)")
	}

	// transport error (server gone)
	srv.Close()
	s2 := seedSrc(&seedFakeProjects{doc: seedProjectDoc("66a00000000000000001111")}, srv, "", "")
	if _, err := s2.SeedText(context.Background(), seedRoomPID); err == nil {
		t.Fatal("transport failure must fail the seed")
	}
}

func TestSeedMalformedDocBodyFails(t *testing.T) {
	// a 200 that is NOT the lines view → fail (never guess content)
	mux := http.NewServeMux()
	mux.HandleFunc("/project/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"unexpected":"shape"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := &SeedSource{P: &seedFakeProjects{doc: seedProjectDoc("66a00000000000000002222")},
		Base: srv.URL, HTTP: srv.Client()}
	if _, err := s.SeedText(context.Background(), seedRoomPID); err == nil {
		t.Fatal("a docstore 200 without lines must fail (content shape is a contract)")
	}
}

func TestSeedHex24Shape(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{24}$`)
	if !re.MatchString(seedRoomPID) || !re.MatchString(seedRoomPID2) {
		t.Fatal("test room ids must be 24-hex (the room-name contract)")
	}
}

// TestSeedWiredIntoService — the S4 seam: a REAL service + temp persistence
// store, with the SeedFn coming from the real SeedSource over a fake
// projects/docstore pair. A real WS peer connecting to the EMPTY room must
// receive the docstore content as its initial state, and the persisted
// history must hold exactly ONE version.
func TestSeedWiredIntoService(t *testing.T) {
	body := "wire seed\nline two\n"
	srv := newSeedDocServer(t, body)
	p := &seedFakeProjects{doc: seedProjectDoc("66a00000000000000003333")}
	seed := &SeedSource{P: p, Base: srv.base, HTTP: http.DefaultClient}

	auth := &fakeAuth{sessionUID: "ownerA", projRole: map[string]Role{seedRoomPID: ReadWrite}}
	svc, err := New(Options{
		Auth:    auth,
		DataDir: t.TempDir(),
		SeedFn:  seed.SeedText,
	})
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

	text := readWSRoomText(t, ts.URL, seedRoomPID)
	if text != "wire seed\nline two\n" { // trailing line preserved (live docstore shape)
		t.Fatalf("peer initial content = %q, want %q", text, "wire seed\nline two\n")
	}
	metas, err := svc.Store().ListVersions(context.Background(), seedRoomPID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("versions = %d, want exactly 1 (the seed)", len(metas))
	}
}
