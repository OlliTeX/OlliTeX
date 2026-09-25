package sharejsupdatemanager

import (
	"errors"
	"reflect"
	"testing"

	"document-updater/internal/errorsx"
	"document-updater/internal/sharejsdb"
	"document-updater/internal/sharejsmodel"
)

// fakeModel mirrors the vendor unit test's stub model ({applyOp,
// getSnapshot, on/emit, db.appliedOps}) as the Model interface.
type fakeModel struct {
	applyErr error
	snapshot string
	snapV    int
	snapErr  error

	applyKey string
	applyOd  sharejsmodel.OpData
	snapKey  string
	fn       func(ev sharejsmodel.Event)
}

func (f *fakeModel) ApplyOp(docKey string, od sharejsmodel.OpData) (int, error) {
	f.applyKey = docKey
	f.applyOd = od
	if od.V == nil {
		return 0, f.applyErr
	}
	return *od.V, f.applyErr
}

func (f *fakeModel) GetSnapshot(docKey string) (sharejsmodel.DocSnapshot, error) {
	f.snapKey = docKey
	if f.snapErr != nil {
		return sharejsmodel.DocSnapshot{}, f.snapErr
	}
	return sharejsmodel.DocSnapshot{Snapshot: f.snapshot, V: f.snapV, Type: "text"}, nil
}

func (f *fakeModel) EmitFn(fn func(ev sharejsmodel.Event)) { f.fn = fn }

// fakeDB is a ShareJsDB carrying a pre-populated appliedOps buffer.
func newFakeDB(applied []any) *sharejsdb.ShareJsDB {
	db := sharejsdb.New("p1", "d1", nil, 3)
	if applied != nil {
		db.AppliedOps["p1:d1"] = applied
	}
	return db
}

func newManager(t *testing.T, m *ShareJsUpdateManager, fake Model, db *sharejsdb.ShareJsDB) *ShareJsUpdateManager {
	if m == nil {
		m = &ShareJsUpdateManager{MaxDocLength: DefaultMaxDocLength}
	}
	m.NewModel = func(p, d string, l []string, v int) Handle { return Handle{Model: fake, DB: db} }
	return m
}

// --- applyUpdate: success (vendor: "successfully") --------------------------------

func TestApplyUpdateSuccess(t *testing.T) {
	lines := []string{"one", "two"}
	version := 34
	// Oracle-pinned hash of the post-apply snapshot 'onefoo\ntwo'
	// (vendored _computeHash: sha1('blob ' + len + '\x00' + content)).
	postHash := "a4f7f171191a149e8544235864c9b0e7fc6b6391"
	fake := &fakeModel{snapshot: "onefoo\ntwo", snapV: version}
	db := newFakeDB([]any{"mock-ops"})
	m := newManager(t, &ShareJsUpdateManager{MaxDocLength: DefaultMaxDocLength}, fake, db)

	update := &Update{
		Op:   []any{map[string]any{"p": 0, "t": "foo"}},
		V:    version,
		Meta: map[string]any{"source": "me"},
		Hash: &postHash,
	}
	gotLines, gotV, gotOps, err := m.ApplyUpdate("p1", "d1", update, lines, version)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(gotLines) != 2 || gotLines[0] != "onefoo" || gotLines[1] != "two" {
		t.Fatalf("lines: %v", gotLines)
	}
	if gotV != version {
		t.Fatalf("version: %d (want %d)", gotV, version)
	}
	if len(gotOps) != 1 || gotOps[0] != "mock-ops" {
		t.Fatalf("appliedOps: %v", gotOps)
	}
	if fake.applyKey != "p1:d1" {
		t.Fatalf("applyOp key: %s", fake.applyKey)
	}
	if fake.snapKey != "p1:d1" {
		t.Fatalf("getSnapshot key: %s", fake.snapKey)
	}
	od := fake.applyOd
	if !reflect.DeepEqual(od.Op, update.Op) || od.V == nil || *od.V != version {
		t.Fatalf("applyOp od: %+v", od)
	}
}

// --- applyUpdate: applyOp error (vendor: "when applyOp fails") ----------------------

func TestApplyUpdateApplyOpError(t *testing.T) {
	wantErr := errors.New("Something went wrong")
	fake := &fakeModel{applyErr: wantErr}
	m := newManager(t, nil, fake, newFakeDB(nil))
	_, _, _, err := m.ApplyUpdate("p1", "d1", &Update{V: 3}, []string{"one"}, 3)
	if err != wantErr {
		t.Fatalf("err: %v (want %v)", err, wantErr)
	}
}

// --- applyUpdate: getSnapshot error (vendor: "when getSnapshot fails") --------------

func TestApplyUpdateGetSnapshotError(t *testing.T) {
	fake := &fakeModel{snapErr: errors.New("Something went wrong")}
	m := newManager(t, nil, fake, newFakeDB(nil))
	_, _, _, err := m.ApplyUpdate("p1", "d1", &Update{V: 3}, []string{"one"}, 3)
	if err == nil || err.Error() != "Something went wrong" {
		t.Fatalf("err: %v", err)
	}
}

// --- applyUpdate: invalid hash (vendor: "with an invalid hash") ---------------------

func TestApplyUpdateInvalidHash(t *testing.T) {
	// Oracle hash of 'onefoo\ntwo'; the fake returns 'unexpected
	// content', so the check must fail.
	badHash := "a4f7f171191a149e8544235864c9b0e7fc6b6391"
	fake := &fakeModel{snapshot: "unexpected content", snapV: 3}
	db := newFakeDB([]any{"mock-ops"})
	m := newManager(t, nil, fake, db)
	_, _, _, err := m.ApplyUpdate("p1", "d1", &Update{Op: []any{}, V: 3, Hash: &badHash}, []string{"one"}, 3)
	var de *errorsx.DeleteMismatchError
	if errors.As(err, &de) {
		t.Fatalf("invalid hash is not a DeleteMismatch: %v", err)
	}
	if err == nil || err.Error() != "Invalid hash" {
		t.Fatalf("err: %v (want \"Invalid hash\")", err)
	}
}

// --- applyUpdate: 'Op already submitted' (vendor quirk: falls through) --------------

func TestApplyUpdateDupFallsThrough(t *testing.T) {
	// The dup op is rejected, so the model state and the db buffer are
	// pre-apply; the vendor code falls through to the post-apply read of
	// that pre-apply state.
	lines := []string{"one", "two"}
	version := 34
	// Oracle hash of the pre-apply snapshot 'one\ntwo' (dup op is NOT
	// applied, so the read-back is this pre-apply content).
	preHash := "9ed40b44250875c2c4532588b014ab45a1799a0f"
	fake := &fakeModel{
		applyErr: errors.New("Op already submitted"),
		snapshot: "one\ntwo",
		snapV:    version,
	}
	db := newFakeDB(nil)
	sent := []any{}
	m := &ShareJsUpdateManager{
		MaxDocLength: DefaultMaxDocLength,
		SendOps:      func(projectID, docID string, opData any) { sent = append(sent, opData) },
	}
	m.NewModel = func(p, d string, l []string, v int) Handle { return Handle{Model: fake, DB: db} }

	update := &Update{Op: []any{}, V: version, Meta: map[string]any{"source": "me"}, Hash: &preHash}
	gotLines, gotV, gotOps, err := m.ApplyUpdate("p1", "d1", update, lines, version)
	if err != nil {
		t.Fatalf("dup must fall through with no error, got %v", err)
	}
	if !update.Dup {
		t.Fatalf("update.Dup not set")
	}
	if len(sent) != 1 {
		t.Fatalf("SendOps not called: %v", sent)
	}
	// The re-emitted op is the wire update as the vendor forwards it.
	if payload, ok := sent[0].(map[string]any); !ok || payload["dup"] != true || payload["v"] != version {
		t.Fatalf("dup payload: %v", sent[0])
	}
	if len(gotLines) != 2 || gotLines[0] != "one" || gotLines[1] != "two" {
		t.Fatalf("lines: %v", gotLines)
	}
	if gotV != version {
		t.Fatalf("version: %d", gotV)
	}
	if len(gotOps) != 0 {
		t.Fatalf("appliedOps: %v", gotOps)
	}
}

// --- applyUpdate: delete mismatch (vendor: /^Delete component/) ---------------------

func TestApplyUpdateDeleteMismatch(t *testing.T) {
	fake := &fakeModel{applyErr: errors.New("Delete component 'z' does not match deleted text 'a'")}
	m := newManager(t, nil, fake, newFakeDB(nil))
	_, _, _, err := m.ApplyUpdate("p1", "d1", &Update{V: 3}, []string{"one"}, 3)
	var de *errorsx.DeleteMismatchError
	if !errors.As(err, &de) {
		t.Fatalf("want *DeleteMismatchError, got %v (type %T)", err, err)
	}
	if de.Message != "Delete component does not match" {
		t.Fatalf("message: %q", de.Message)
	}
}

// --- applyUpdate: doc too large ------------------------------------------------------

func TestApplyUpdateTooLarge(t *testing.T) {
	fake := &fakeModel{snapshot: "z", snapV: 3}
	for i := 0; i < 30; i++ {
		fake.snapshot += "z"
	}
	m := newManager(t, &ShareJsUpdateManager{MaxDocLength: 10}, fake, newFakeDB(nil))
	_, _, _, err := m.ApplyUpdate("p1", "d1", &Update{V: 3}, []string{"one"}, 3)
	var ft *errorsx.FileTooLargeError
	if !errors.As(err, &ft) {
		t.Fatalf("want *FileTooLargeError, got %v (type %T)", err, err)
	}
	if ft.Message != "Update takes doc over max doc size" {
		t.Fatalf("message: %q", ft.Message)
	}
}

// --- applyUpdate: hash is skipped when the version moved (vendor guard) -------------

func TestApplyUpdateHashSkippedOnVersionSkew(t *testing.T) {
	// incomingUpdateVersion != version => no hash check, even on mismatch.
	fake := &fakeModel{snapshot: "whatever", snapV: 5}
	badHash := "0000000000000000000000000000000000000000"
	m := newManager(t, nil, fake, newFakeDB(nil))
	_, v, ops, err := m.ApplyUpdate("p1", "d1", &Update{V: 3, Hash: &badHash}, []string{"one"}, 4)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 5 {
		t.Fatalf("version: %d", v)
	}
	if len(ops) != 0 {
		t.Fatalf("ops: %v", ops)
	}
}

// --- _listenForOps (vendor: publishes the op to redis) ------------------------------

func TestListenForOpsPublishes(t *testing.T) {
	sent := []string{}
	m := &ShareJsUpdateManager{SendOps: func(projectID, docID string, opData any) {
		sent = append(sent, projectID+":"+docID)
	}}
	fake := &fakeModel{}
	m.ListenOps(fake)
	if fake.fn == nil {
		t.Fatalf("EmitFn not installed")
	}
	// An applyOp emission on any document is forwarded under its own key.
	fake.fn(sharejsmodel.Event{Kind: "applyOp", DocName: "p9:d9", OpV: 7, Op: []any{map[string]any{"p": 1, "i": "x"}}})
	if len(sent) != 1 || sent[0] != "p9:d9" {
		t.Fatalf("sent: %v", sent)
	}
	// Non-applyOp events are ignored.
	fake.fn(sharejsmodel.Event{Kind: "create", DocName: "p1:d1"})
	fake.fn(sharejsmodel.Event{Kind: "applyOp", DocName: "no-colon-key", OpV: 1})
	if len(sent) != 1 {
		t.Fatalf("must ignore non-applyOp / non-key emissions: %v", sent)
	}
}

// --- production wiring: real ShareJsDB + sharejsmodel.Model --------------------------

func TestApplyUpdateProductionWiring(t *testing.T) {
	lines := []string{"one", "two"}
	version := 34
	// Oracle hash of the post-apply snapshot 'one\ntwofoo' (the op
	// inserts "foo" at the end of "one\ntwo").
	postHash := "b5b0422cfb1731a54cb1ad229b47f5a67b7fc676"
	sent := []any{}
	m := New()
	m.SendOps = func(projectID, docID string, opData any) { sent = append(sent, opData) }

	_, _, _, err := m.ApplyUpdate("p1", "d1", &Update{
		Op:   []any{map[string]any{"p": 7, "i": "foo"}},
		V:    version,
		Meta: map[string]any{"source": "me"},
		Hash: &postHash,
	}, lines, version)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(sent) != 1 {
		t.Fatalf("SendOps not called: %v", sent)
	}
	if payload, ok := sent[0].(map[string]any); !ok || payload["v"] != version {
		t.Fatalf("sent op: %v", sent[0])
	}
}

// --- _computeHash oracle values ------------------------------------------------------

func TestComputeHash(t *testing.T) {
	// Vendored: sha1('blob ' + <utf16 length> + '\x00' + utf8(content)).
	if got := computeHash("one\ntwo"); got != "9ed40b44250875c2c4532588b014ab45a1799a0f" {
		t.Fatalf("hash: %s", got)
	}
	if got := computeHash(""); got != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Fatalf("empty hash: %s", got)
	}
	// The length is the UTF-16 code-unit count (vendored JS .length).
	emoji := "🧪" // one rune, two UTF-16 units
	if got := computeHash(emoji); got != "8340100d809dead14597728e9de2c4c5e799414f" {
		t.Fatalf("utf16 hash: %s", got)
	}
	if computeHash("a") == computeHash("b") {
		t.Fatalf("hash collision for different inputs")
	}
}

// --- splitDoc oracle ------------------------------------------------------------------

func TestSplitDoc(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{""}},
		{"a\nb", []string{"a", "b"}},
		{"a\r\nb", []string{"a", "b"}},
		{"\r", []string{"", ""}},
		{"a\r", []string{"a", ""}},
		{"a\n\nb", []string{"a", "", "b"}},
		{"a\r\nb\r\n", []string{"a", "b", ""}},
	}
	for _, c := range cases {
		if got := splitDoc(c.in); len(got) != len(c.want) {
			t.Fatalf("splitDoc(%q): %v (want %v)", c.in, got, c.want)
		} else {
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("splitDoc(%q)[%d] = %q (want %q)", c.in, i, got[i], c.want[i])
				}
			}
		}
	}
}
