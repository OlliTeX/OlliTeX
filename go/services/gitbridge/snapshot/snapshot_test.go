// Port of PostbackManagerTest + snapshot wire/facade coverage via httptest.
package snapshot

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/giterrors"
)

// ---------------------------------------------------------------------------
// NetSnapshotApi wire tests (httptest server driving the global base URL).
// ---------------------------------------------------------------------------

func setBase(t *testing.T, u string) {
	t.Helper()
	SetAPIBaseURL(u)
	t.Cleanup(func() { SetAPIBaseURL("http://127.0.0.1:1/docs/") })
}

func TestGetDocStatusCodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/docs/proj-ok":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"latestVerId":2,"latestVerAt":"2020-01-01T00:00:00.000Z","latestVerBy":{"name":"u","email":"u@e.com"}}`))
		case r.URL.Path == "/docs/proj-401":
			w.WriteHeader(401)
		case r.URL.Path == "/docs/proj-404":
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Exported to v2","newRemote":"git@example.com:x"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	setBase(t, srv.URL)

	api := &NetSnapshotApi{}
	// success: path is {base}docs/proj (note: base ends with "docs/").
	got, err := api.GetDoc(nil, "proj-ok")
	if err != nil || got == nil {
		t.Fatalf("GetDoc ok: %v %+v", err, got)
	}
	if got.VersionID != 2 || got.Name != "u" || got.Email != "u@e.com" {
		t.Fatalf("GetDoc fields wrong: %+v", got)
	}
	if v, e := got.VersionIDErr(); e != nil || v != 2 {
		t.Fatalf("VersionIDErr = %d %v, want 2 nil", v, e)
	}
	// 401 -> ForbiddenException.
	if _, err := api.GetDoc(nil, "proj-401"); err == nil || !asForbidden(err) {
		t.Fatalf("GetDoc 401 must yield ForbiddenException, got %v", err)
	}
	// 404 with Exported to v2 -> MissingRepositoryException carrying the
	// v2 message lines.
	_, err = api.GetDoc(nil, "proj-404")
	mre, ok := err.(*giterrors.MissingRepositoryException)
	if !ok {
		t.Fatalf("GetDoc 404-exported must be MissingRepositoryException, got %v", err)
	}
	if len(mre.Description()) == 0 {
		t.Fatalf("exported v2 description must be populated")
	}
}

func e2(err error) error { _ = err; return err }

func asForbidden(err error) bool {
	_, ok := err.(*giterrors.ForbiddenException)
	return ok
}

func TestGetDocExportedMessageContent(t *testing.T) {
	// The 404 "Exported to v2" lines must embed the new remote (index 3
	// per ExportV2Message line layout, 5th line index 8 is the remote set-url).
	got := notFoundError([]byte(`{"message":"Exported to v2","newRemote":"https://new.example.com/git"}`))
	mre, ok := got.(*giterrors.MissingRepositoryException)
	if !ok {
		t.Fatalf("notFoundError exported: %v", got)
	}
	lines := mre.Description()
	if len(lines) < 8 || !containsStr(lines[3], "https://new.example.com/git") {
		t.Fatalf("exported-v2 lines missing remote URL: %v", lines)
	}
}

func TestDeprecatedMessage(t *testing.T) {
	got := notFoundError([]byte(`{"message":"Overleaf v1 is Deprecated","newUrl":"https://new.example.com/edit/x"}`))
	mre, ok := got.(*giterrors.MissingRepositoryException)
	if !ok {
		t.Fatalf("deprecated 404: %v", got)
	}
	lines := mre.Description()
	if len(lines) < 5 || !containsStr(lines[4], "https://new.example.com/edit/x") {
		t.Fatalf("deprecated lines missing URL: %v", lines)
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if containsStr(h, needle) {
			return true
		}
	}
	return false
}

func containsStr(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestStatusErrorBranches(t *testing.T) {
	var errs = map[int]error{
		401: statusError(401, nil),
		403: statusError(403, nil),
		429: statusError(429, nil),
		500: statusError(500, nil),
		409: statusError(409, []byte(`{"code":"projectHasDotGit"}`)),
		410: statusError(410, nil),
	}
	if _, ok := errs[401].(*giterrors.ForbiddenException); !ok {
		t.Fatalf("401 must be Forbidden, got %v", errs[401])
	}
	if _, ok := errs[403].(*giterrors.ForbiddenException); !ok {
		t.Fatalf("403 must be Forbidden, got %v", errs[403])
	}
	if _, ok := errs[429].(*giterrors.MissingRepositoryException); !ok {
		t.Fatalf("429 must be MissingRepository (rate-limit), got %v", errs[429])
	}
	if _, ok := errs[500].(*giterrors.FailedConnectionException); !ok {
		t.Fatalf("5xx must be FailedConnection, got %v", errs[500])
	}
	if _, ok := errs[409].(*giterrors.MissingRepositoryException); !ok {
		t.Fatalf("409 projectHasDotGit must be MissingRepository, got %v", errs[409])
	}
	if _, ok := errs[410].(*giterrors.MissingRepositoryException); !ok {
		t.Fatalf("other 4xx must be generic MissingRepository, got %v", errs[410])
	}
	// 409 without the matching code => generic "Conflict: 409" description.
	c := conflictError([]byte(`{"code":"other"}`))
	if mc, ok := c.(*giterrors.MissingRepositoryException); !ok || mc.Description()[0] != "Conflict: 409" {
		t.Fatalf("409 fallback description: %v", c)
	}
	// 404 fallback (no message object) => generic reason.
	c404 := notFoundError([]byte(`{}`))
	if m404, ok := c404.(*giterrors.MissingRepositoryException); !ok || len(m404.Description()) == 0 {
		t.Fatalf("404 fallback description: %v", c404)
	}
}

func TestParseGetDocResponse(t *testing.T) {
	// status mapping.
	four := 404
	if got := ParseGetDocResponse(&four, nil, nil, nil); got == nil || !got.Invalid {
		t.Fatalf("status 404 must be Invalid marker, got %v", got)
	}
	four01 := 401
	if got := ParseGetDocResponse(&four01, nil, nil, nil); got == nil || !got.Forbidden {
		t.Fatalf("status 401 must be Forbidden marker, got %v", got)
	}
	// unknown status -> nil (Java: IllegalArgumentException path is a
	// constructor guard; the Go contract is nil result).
	weird := 599
	if got := ParseGetDocResponse(&weird, nil, nil, nil); got != nil {
		t.Fatalf("unknown status must yield nil result, got %v", got)
	}
	// success: latest ver id + name/email; absent latestVerBy must zero the user.
	id := 7
	by := &User{Name: "n", Email: "e"}
	if got := ParseGetDocResponse(nil, &id, nil, by); got == nil || got.VersionID != 7 || got.Name != "n" {
		t.Fatalf("success parse: %v", got)
	}
	// all nil pointers -> empty result with Invalid false and VersionID 0.
	empty := ParseGetDocResponse(nil, nil, nil, nil)
	if empty == nil || empty.Invalid || empty.Forbidden || empty.VersionID != 0 {
		t.Fatalf("nil-ptr parse: %v", empty)
	}
}

func TestProjectURLShape(t *testing.T) {
	// The config value (with or without trailing "/") is normalised to
	// "{value}/docs/" and then project + apiCall are appended verbatim
	// (Java: BASE_URL + projectName + apiCall, no separator).
	SetAPIBaseURL("http://127.0.0.1:9/api/v0")
	if got := projectURL("proj", ""); got != "http://127.0.0.1:9/api/v0/docs/proj" {
		t.Fatalf("projectURL = %q", got)
	}
	if got := projectURL("proj", "/snapshots"); got != "http://127.0.0.1:9/api/v0/docs/proj/snapshots" {
		t.Fatalf("projectURL call = %q", got)
	}
	// An already-trailing-slash URL must not double the slash.
	SetAPIBaseURL("http://127.0.0.1:9/api/v0/")
	if got := projectURL("proj", ""); got != "http://127.0.0.1:9/api/v0/docs/proj" {
		t.Fatalf("projectURL trailing-slash = %q", got)
	}
}

func TestPushAcceptedOutOfDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/docs/p1/snapshots":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":"accepted"}`))
		case "/docs/p2/snapshots":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":"outOfDate"}`))
		case "/docs/p3/snapshots":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":"bogus"}`))
		}
	}))
	defer srv.Close()
	setBase(t, srv.URL)
	api := &NetSnapshotApi{}
	if res, err := api.Push(nil, "p1", []byte(`{"a":1}`)); err != nil || !res.WasSuccessful {
		t.Fatalf("push p1 = %v %v, want accepted", res, err)
	}
	if res, err := api.Push(nil, "p2", nil); err != nil || res.WasSuccessful {
		t.Fatalf("push p2 = %v %v, want outOfDate not-success", res, err)
	}
	if _, err := api.Push(nil, "p3", nil); err == nil || !containsStr(err.Error(), "unknown push response code") {
		t.Fatalf("push p3 must be unknown-code error, got %v", err)
	}
}

func TestGetForVersionWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs/v1/snapshots/3" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"srcs":[["hello.txt","a.txt"],["binary.bin","b.bin"]],"atts":[["file","file.pdf"]]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	setBase(t, srv.URL)
	api := &NetSnapshotApi{}
	sd, err := api.GetForVersion(nil, "v1", 3)
	if err != nil {
		t.Fatalf("GetForVersion: %v", err)
	}
	if len(sd.Srcs) != 2 || string(sd.Srcs[0].Contents) != "hello.txt" || sd.Srcs[0].Path != "a.txt" {
		t.Fatalf("srcs wire = %+v", sd.Srcs)
	}
	if len(sd.Atts) != 1 || sd.Atts[0].URL != "file" || sd.Atts[0].Path != "file.pdf" {
		t.Fatalf("atts wire = %+v", sd.Atts)
	}
	// 404 on the version GET -> MissingRepository (generic reason).
	if _, err := api.GetForVersion(nil, "missing", 1); err == nil {
		t.Fatalf("GetForVersion missing project must error")
	}
}

func TestGetSavedVersWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/docs/g1/saved_vers":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`[{"versionId":1,"comment":"c1","user":{"email":"a","name":"A"}},{"versionId":2,"comment":"c2","user":{"email":"b","name":"B"},"createdAt":"2020-01-01T00:00:00.000Z"}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	setBase(t, srv.URL)
	api := &NetSnapshotApi{}
	vs, err := api.GetSavedVers(nil, "g1")
	if err != nil {
		t.Fatalf("GetSavedVers: %v", err)
	}
	if len(vs) != 2 || vs[0].VersionID != 1 || vs[1].User.Name != "B" || vs[1].User.Email != "b" {
		t.Fatalf("getSavedVers wire = %+v", vs)
	}
	// missing project -> error (404 mapping).
	if _, err := api.GetSavedVers(nil, "nope"); err == nil {
		t.Fatalf("GetSavedVers missing project must error")
	}
}

func TestFailedConnectionWhenUnreachable(t *testing.T) {
	// Point the base URL at a closed port; doGET/doPOST return err =>
	// FailedConnectionException.
	setBase(t, "http://127.0.0.1:1/")
	api := &NetSnapshotApi{}
	if _, err := api.GetDoc(nil, "x"); e2(err) == nil {
		t.Fatalf("unreachable GetDoc must error")
	}
	if _, err := api.GetSavedVers(nil, "x"); e2(err) == nil {
		t.Fatalf("unreachable GetSavedVers must error")
	}
	if _, err := api.GetForVersion(nil, "x", 1); e2(err) == nil {
		t.Fatalf("unreachable GetForVersion must error")
	}
	if _, err := api.Push(nil, "x", nil); e2(err) == nil {
		t.Fatalf("unreachable Push must error")
	}
}

// ---------------------------------------------------------------------------
// Facade (ports bridge/snapshot/SnapshotApiFacade + SnapshotApi contract).
// ---------------------------------------------------------------------------

type fakeApi struct {
	getDocResult     *GetDocResult
	getDocErr        error
	getForVersionErr error
	getForVersionSD  *SnapshotData
	savedVers        []SnapshotInfo
	savedVersErr     error
}

func (f *fakeApi) GetDoc(_ *data.Oauth2, _ string) (*GetDocResult, error) {
	return f.getDocResult, f.getDocErr
}
func (f *fakeApi) GetForVersion(_ *data.Oauth2, _ string, _ int) (*SnapshotData, error) {
	return f.getForVersionSD, f.getForVersionErr
}
func (f *fakeApi) GetSavedVers(_ *data.Oauth2, _ string) ([]SnapshotInfo, error) {
	return f.savedVers, f.savedVersErr
}
func (f *fakeApi) Push(_ *data.Oauth2, _ string, _ []byte) (*PushResult, error) {
	return &PushResult{WasSuccessful: true}, nil
}

func TestFacadeProjectExists(t *testing.T) {
	// valid project -> true.
	f := &fakeApi{getDocResult: &GetDocResult{VersionID: 5}}
	facade := NewFacade(f)
	if ok, err := facade.ProjectExists(nil, "p"); err != nil || !ok {
		t.Fatalf("valid project must exist: %v %v", ok, err)
	}
	// invalid (MissingRepository) -> (false, nil).
	f2 := &fakeApi{getDocErr: &giterrors.MissingRepositoryException{DescriptionLines: []string{"no git access"}}}
	if ok, err := NewFacade(f2).ProjectExists(nil, "p"); err != nil || ok {
		t.Fatalf("missing repo must be (false, nil), got %v %v", ok, err)
	}
	// GetDoc 404 marker (Invalid) -> (false, nil).
	invalid := ParseGetDocResponse(ptrStrPtr(404), nil, nil, nil)
	f3 := &fakeApi{getDocResult: invalid}
	if ok, err := NewFacade(f3).ProjectExists(nil, "p"); err != nil || ok {
		t.Fatalf("invalid marker must be (false, nil), got %v %v", ok, err)
	}
	// Other error propagates.
	oerr := errors.New("boom")
	f4 := &fakeApi{getDocErr: oerr}
	if ok, err := NewFacade(f4).ProjectExists(nil, "p"); err == nil || ok {
		t.Fatalf("transport error must propagate, got %v %v", ok, err)
	}
}

func TestFacadeGetDoc(t *testing.T) {
	// not present (Invalid marker) -> (nil, nil).
	invalid := ParseGetDocResponse(ptrStrPtr(404), nil, nil, nil)
	if got, err := NewFacade(&fakeApi{getDocResult: invalid}).GetDoc(nil, "p"); err != nil || got != nil {
		t.Fatalf("GetDoc present=invalid must be (nil, nil), got %v %v", got, err)
	}
	// present -> the parsed result.
	doc := &GetDocResult{VersionID: 9, Name: "n", Email: "e"}
	if got, err := NewFacade(&fakeApi{getDocResult: doc}).GetDoc(nil, "p"); err != nil || got != doc {
		t.Fatalf("GetDoc present = %v %v, want the same doc", got, err)
	}
	// error propagates except MissingRepository -> (nil, nil).
	if got, err := NewFacade(&fakeApi{getDocErr: &giterrors.MissingRepositoryException{DescriptionLines: []string{"x"}}}).GetDoc(nil, "p"); err != nil || got != nil {
		t.Fatalf("GetDoc missing must be (nil, nil), got %v %v", got, err)
	}
	if _, err := NewFacade(&fakeApi{getDocErr: errors.New("err")}).GetDoc(nil, "p"); err == nil {
		t.Fatalf("GetDoc other error must propagate")
	}
}

func TestFacadeGetSnapshots(t *testing.T) {
	// afterVersion 0 with a 404 doc => no snapshots (project missing).
	if _, err := NewFacade(&fakeApi{getDocResult: ParseGetDocResponse(ptrStrPtr(404), nil, nil, nil)}).GetSnapshots(nil, "p", 0); err != nil {
		t.Fatalf("missing project GetSnapshots must be (nil, nil), got %v", err)
	}
	// happy path: doc v5 name u, saved_vers v1 (filtered), v4 (kept).
	latest := &GetDocResult{VersionID: 5, CreatedAt: "2020-01-01T00:00:00.000Z", Name: "latest", Email: "l@e.com"}
	f := &fakeApi{
		getDocResult: latest,
		savedVers: []SnapshotInfo{
			{VersionID: 4, Comment: "old", User: User{Name: "o", Email: "o@e.com"}, CreatedAt: "2020-01-01T00:00:00.000Z"},
			{VersionID: 5, Comment: "same", User: User{Name: "s", Email: "s@e.com"}, CreatedAt: "2020-01-01T00:00:00.000Z"},
		},
		getForVersionSD: &SnapshotData{Srcs: []SnapshotFile{{Contents: []byte("c"), Path: "p"}}, Atts: []SnapshotAttachment{{URL: "u", Path: "a"}}},
	}
	got, err := NewFacade(f).GetSnapshots(nil, "p", 0)
	if err != nil {
		t.Fatalf("GetSnapshots: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetSnapshots len = %d, want 2 (v4 + synthetic v5)", len(got))
	}
	if got[0].VersionID != 4 || got[0].Comment != "old" || got[1].VersionID != 5 || got[1].User.Email != "l@e.com" {
		t.Fatalf("GetSnapshots wrong merge: %v", got)
	}
	if got[0].Srcs[0].Contents == nil || string(got[0].Srcs[0].Contents) != "c" || got[0].Srcs[0].Path != "p" || got[0].Atts[0].URL != "u" || got[0].Atts[0].Path != "a" {
		t.Fatalf("snapshot srcs/atts not combined: %v", got[0])
	}
	// filter: afterVersion=4 -> only the synthetic v5.
	got2, _ := NewFacade(f).GetSnapshots(nil, "p", 4)
	if len(got2) != 1 || got2[0].VersionID != 5 {
		t.Fatalf("after=4 filter: %v", got2)
	}
	// getForVersion error propagates.
	fErr := &fakeApi{getDocResult: latest, getForVersionErr: errors.New("vers err")}
	if _, err := NewFacade(fErr).GetSnapshots(nil, "p", 0); err == nil {
		t.Fatalf("GetSnapshots must propagate getForVersion error")
	}
	// getSavedVers nil-result (404) => still the synthetic latest.
	fNil := &fakeApi{getDocResult: latest, savedVersErr: &giterrors.MissingRepositoryException{DescriptionLines: []string{"x"}},
		getForVersionSD: &SnapshotData{Srcs: []SnapshotFile{}, Atts: []SnapshotAttachment{}}}
	got3, err := NewFacade(fNil).GetSnapshots(nil, "p", 0)
	if err != nil || len(got3) != 1 || got3[0].VersionID != 5 {
		t.Fatalf("savedVers nil => synthetic latest only, got %v %v", got3, err)
	}
	// getDoc other error propagates.
	if _, err := NewFacade(&fakeApi{getDocErr: errors.New("d")}).GetSnapshots(nil, "p", 0); err == nil {
		t.Fatalf("GetSnapshots must propagate getDoc error")
	}
	// getForVersion invalid marker is treated as not-found -> nil list.
	if got, err := NewFacade(&fakeApi{getDocErr: &giterrors.MissingRepositoryException{DescriptionLines: []string{"x"}}}).GetSnapshots(nil, "p", 0); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("missing project => empty list, got %v %v", got, err)
	}
}

func TestFacadePush(t *testing.T) {
	if res, err := NewFacade(&fakeApi{}).Push(nil, "p", []byte("x")); err != nil || !res.WasSuccessful {
		t.Fatalf("Facade.Push not a passthrough: %v %v", res, err)
	}
}

func ptrStrPtr(v int) *int { return &v }

// ---------------------------------------------------------------------------
// PostbackManager (ports PostbackManagerTest).
// ---------------------------------------------------------------------------

func TestPostbackManagerWaitAndPost(t *testing.T) {
	// testRaceWithVersionId: post before wait; the key must match.
	m := NewPostbackManager()
	key := m.MakeKeyForProject("proj")
	if key == "" {
		t.Fatalf("MakeKeyForProject empty")
	}
	if err := m.PostVersionIDForProject("proj", 1, key); err != nil {
		t.Fatalf("PostVersionIDForProject known project: %v", err)
	}
	vid, err := m.WaitProjectForVersionIdOrThrow("proj")
	if err != nil || vid != 1 {
		t.Fatalf("wait = %d %v, want 1 nil (posted version)", vid, err)
	}

	// testTableConsistency: posting does not remove; each wait removes.
	m2 := NewPostbackManager()
	key1 := m2.MakeKeyForProject("proj1")
	key2 := m2.MakeKeyForProject("proj2")
	if err := m2.PostVersionIDForProject("proj1", 1, key1); err != nil {
		t.Fatalf("post proj1: %v", err)
	}
	if err := m2.PostVersionIDForProject("proj2", 1, key2); err != nil {
		t.Fatalf("post proj2: %v", err)
	}
	// table still holds 2 until the waits.
	if v, e2 := m2.WaitProjectForVersionIdOrThrow("proj1"); e2 != nil || v != 1 {
		t.Fatalf("wait proj1 = %d %v, want 1", v, e2)
	}
	if v, e2 := m2.WaitProjectForVersionIdOrThrow("proj2"); e2 != nil || v != 1 {
		t.Fatalf("wait proj2 = %d %v, want 1", v, e2)
	}
	// both waits removed the entries; a third wait is UnexpectedPostback.
	if _, e2 := m2.WaitProjectForVersionIdOrThrow("proj1"); e2 == nil {
		t.Fatalf("wait on removed project must error")
	}
	// posting to a removed project is UnexpectedPostbackJava.
	if e2 := m2.PostVersionIDForProject("proj1", 1, key1); e2 == nil {
		t.Fatalf("post to removed project must error")
	}
}

// testRaceWithException: assertSame -> Go pointer identity, posted BEFORE the
// wait (the "race").
func TestPostbackManagerWaitReturnsPostedException(t *testing.T) {
	m := NewPostbackManager()
	key := m.MakeKeyForProject("proj")
	exc := &giterrors.ForbiddenException{}
	if err := m.PostExceptionForProject("proj", exc, key); err != nil {
		t.Fatalf("PostExceptionForProject: %v", err)
	}
	_, got := m.WaitProjectForVersionIdOrThrow("proj")
	if got != errorValue(exc) || got == nil {
		t.Fatalf("wait must return the exact posted exception, got %v", got)
	}
}

func errorValue(got error) error { return got }

func TestPostbackManagerUnknownProject(t *testing.T) {
	m := NewPostbackManager()
	if _, err := m.WaitProjectForVersionIdOrThrow("ghost"); err == nil {
		t.Fatalf("unknown project wait must error")
	}
	if err := m.PostVersionIDForProject("ghost", 0, "k"); err == nil {
		t.Fatalf("unknown project post must error")
	}
	if err := m.PostExceptionForProject("ghost", errors.New("x"), "k"); err == nil {
		t.Fatalf("unknown project post-exception must error")
	}
	if err := m.CheckPostbackKey("ghost", "k"); err == nil {
		t.Fatalf("unknown project check must error")
	}
}

func TestPostbackKeysDistinct(t *testing.T) {
	m := NewPostbackManager()
	k1 := m.MakeKeyForProject("a")
	k2 := m.MakeKeyForProject("b")
	if k1 == "" || k2 == "" || k1 == k2 {
		t.Fatalf("postback keys must be non-empty and distinct: %q vs %q", k1, k2)
	}
}

func TestPostbackExceptionPropagates(t *testing.T) {
	m := NewPostbackManager()
	pbKey := m.MakeKeyForProject("p")
	exc := errors.New("push failed")
	if err := m.PostExceptionForProject("p", exc, pbKey); err != nil {
		t.Fatalf("PostExceptionForProject: %v", err)
	}
	if _, err := m.WaitProjectForVersionIdOrThrow("p"); err == nil || !containsStr(err.Error(), "push failed") {
		// WaitPostback must surface the posted error.
		t.Fatalf("WaitProject must surface the posted exception, got %v", err)
	}
}

func TestCheckPostbackKeyWrongKey(t *testing.T) {
	m := NewPostbackManager()
	pbKey := m.MakeKeyForProject("p")
	_ = pbKey
	// Wrong key against a known project must be InvalidPostbackKey.
	if err := m.CheckPostbackKey("p", "wrong"); err == nil {
		t.Fatalf("wrong key must be InvalidPostbackKey")
	}
}

// WaitPostback loop body: post arrives while the waiter is blocked in the
// select (the real Java "race" that PostbackManager's ReentrantLock+Condition
// covers). A fresh promise is registered in the table, the wait starts, and
// a goroutine posts the version after a short delay so the select path
// (received = true) is taken rather than the timeout branch.
func TestWaitPostbackLoopWake(t *testing.T) {
	m := NewPostbackManager()
	key := m.MakeKeyForProject("p")
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = m.PostVersionIDForProject("p", 99, key)
	}()
	vid, err := m.WaitProjectForVersionIdOrThrow("p")
	if err != nil || vid != 99 {
		t.Fatalf("loop wait = %d %v, want 99 nil", vid, err)
	}
}

func TestSnapshotFileSize(t *testing.T) {
	if got := (SnapshotFile{Contents: []byte("abcd")}).Size(); got != 4 {
		t.Fatalf("SnapshotFile.Size = %d, want 4", got)
	}
}

// ptrStr nil branch: a 404 body has the message but no URL field.
func Test404MessageNoURLFallback(t *testing.T) {
	var mre *giterrors.MissingRepositoryException
	if v, ok := notFoundError([]byte(`{"message":"Exported to v2"}`)).(*giterrors.MissingRepositoryException); ok {
		mre = v
	}
	if len(mre.Description()) == 0 {
		t.Fatalf("exported w/o URL must still produce lines")
	}
	var dre *giterrors.MissingRepositoryException
	if v, ok := notFoundError([]byte(`{"message":"Overleaf v1 is Deprecated"}`)).(*giterrors.MissingRepositoryException); ok {
		dre = v
	}
	if len(dre.Description()) == 0 {
		t.Fatalf("deprecated w/o URL must still produce lines")
	}
}
