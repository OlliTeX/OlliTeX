package collab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
	ysync "github.com/reearth/ygo/sync"
	"go.mongodb.org/mongo-driver/bson"
)

// ---------------------------------------------------------------------------
// fakes
// ---------------------------------------------------------------------------

type fakeMongo struct {
	mu       sync.Mutex
	users    map[string]bson.D
	projects map[string]bson.D
	err      error
}

func (f *fakeMongo) UserByID(ctx context.Context, id string) (bson.D, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	d, ok := f.users[id]
	if !ok {
		return nil, nil
	}
	return d, nil
}

func (f *fakeMongo) ProjectByID(ctx context.Context, id string) (bson.D, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	d, ok := f.projects[id]
	if !ok {
		return nil, nil
	}
	return d, nil
}

type fakeAuth struct {
	sessionUID string
	projRole   map[string]Role
	projErr    error
}

func (f *fakeAuth) Identity(r *http.Request) (string, error) {
	return f.sessionUID, nil
}

func (f *fakeAuth) ProjectRole(uid, projectID string) (Role, error) {
	if f.projErr != nil {
		return Deny, f.projErr
	}
	return f.projRole[projectID], nil
}

func newTestService(t *testing.T, auth AuthSource) *Service {
	t.Helper()
	svc, err := New(Options{Auth: auth, DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	})
	return svc
}

func wsURL(base, room string) string {
	u := strings.Replace(base, "http://", "ws://", 1)
	return u + "/collab/" + path.Join(room)
}

// ---------------------------------------------------------------------------
// authorization gate (the only OlliTeX policy surface)
// ---------------------------------------------------------------------------

func TestAuthorizeRejectsAnonymous(t *testing.T) {
	auth := &fakeAuth{sessionUID: ""} // no session
	svc := newTestService(t, auth)
	ts := httptest.NewServer(svc)
	defer ts.Close()

	_, resp, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL(ts.URL, "proj1"), http.Header{})
	assertRejected := func(t *testing.T, resp *http.Response, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("dial SUCCEEDED — must be rejected")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			if resp == nil {
				t.Fatalf("want 401 handshake rejection, got error without response: %v", err)
			}
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	}
	assertRejected(t, resp, err)
}

func TestAuthorizeRejectsUnrelatedUser(t *testing.T) {
	auth := &fakeAuth{
		sessionUID: "user-x",
		projRole:   map[string]Role{}, // authenticated, but no role anywhere
	}
	svc := newTestService(t, auth)
	ts := httptest.NewServer(svc)
	defer ts.Close()

	_, resp, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL(ts.URL, "proj1"), http.Header{})
	if err == nil {
		t.Fatal("unrelated-user dial SUCCEEDED — must be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %v (resp %+v)", err, resp)
	}
}

func TestAuthorizeAcceptsOwner(t *testing.T) {
	auth := &fakeAuth{
		sessionUID: "owner-1",
		projRole:   map[string]Role{"proj1": ReadWrite},
	}
	svc := newTestService(t, auth)
	ts := httptest.NewServer(svc)
	defer ts.Close()

	c, _, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL(ts.URL, "proj1"), http.Header{})
	if err != nil {
		t.Fatalf("owner dial failed: %v", err)
	}
	c.Close()
}

func TestAuthorizeAcceptsReadOnlyViewer(t *testing.T) {
	auth := &fakeAuth{
		sessionUID: "viewer-1",
		projRole:   map[string]Role{"proj1": ReadOnly},
	}
	svc := newTestService(t, auth)
	ts := httptest.NewServer(svc)
	defer ts.Close()

	// Read-only peers are ACCEPTED at the handshake (their writes are dropped
	// server-side per ygo ConnectionConfig.ReadOnly semantics).
	c, _, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL(ts.URL, "proj1"), http.Header{})
	if err != nil {
		t.Fatalf("read-only viewer dial failed: %v", err)
	}
	c.Close()
}

func TestAuthorizeDeniesWhenRoleLookupFails(t *testing.T) {
	auth := &fakeAuth{
		sessionUID: "owner-1",
		projRole:   map[string]Role{"proj1": ReadWrite},
		projErr:    errors.New("mongo down"), // ProjectRole hard-fails
	}
	svc := newTestService(t, auth)
	ts := httptest.NewServer(svc)
	defer ts.Close()

	_, resp, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL(ts.URL, "proj1"), http.Header{})
	if err == nil {
		t.Fatal("dial SUCCEEDED despite role-store failure — fail-closed violated")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want fail-closed 401, got %v (resp %+v)", err, resp)
	}
}

// ---------------------------------------------------------------------------
// SessionAuth — session doc -> uid -> frozen check (production logic)
// ---------------------------------------------------------------------------

func TestSessionAuthIdentityFingerprint(t *testing.T) {
	mk := func(uid string, active string, exists bool) *fakeMongo {
		f := &fakeMongo{users: map[string]bson.D{}}
		if exists {
			f.users[uid] = bson.D{{Key: "_id", Value: uid}, {Key: "activeStatus", Value: active}}
		}
		return f
	}
	sess := func(uid string) func(context.Context, *http.Request) (map[string]any, error) {
		return func(ctx context.Context, r *http.Request) (map[string]any, error) {
			if uid == "" {
				return nil, nil
			}
			return map[string]any{"passport": map[string]any{"user": map[string]any{"_id": uid}}}, nil
		}
	}

	cases := []struct {
		name    string
		m       *fakeMongo
		uid     string
		wantUID string
		wantErr bool
	}{
		{"logged-in-active", mk("u1", "active", true), "u1", "u1", false},
		{"frozen-denied", mk("u2", "frozen", true), "u2", "", false},
		{"no-user-denied", mk("", "active", false), "u3", "", false},
		{"anonymous", mk("", "active", false), "", "", false},
		{"store-error-fail-closed", &fakeMongo{users: map[string]bson.D{"u4": {}}, err: errors.New("redis down")}, "u4", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &SessionAuth{SessionDoc: sess(tc.uid), M: tc.m}
			uid, err := a.Identity(&http.Request{})
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if uid != tc.wantUID {
				t.Fatalf("uid = %q, want %q", uid, tc.wantUID)
			}
		})
	}
}

func TestSessionAuthProjectRoles(t *testing.T) {
	f := &fakeMongo{projects: map[string]bson.D{
		"AABBCC": bson.D{
			{Key: "owner_ref", Value: "ownerX"},
			{Key: "collaborator_refs", Value: bson.A{"collabY"}},
			{Key: "readOnly_refs", Value: bson.A{"viewerZ"}},
		},
	}}
	cases := []struct {
		uid  string
		want Role
	}{
		{"ownerX", ReadWrite},
		{"collabY", ReadWrite},
		{"viewerZ", ReadOnly},
		{"strangerW", Deny},
	}
	a := &SessionAuth{M: f}
	for _, tc := range cases {
		got, err := a.ProjectRole(tc.uid, "AABBCC")
		if err != nil {
			t.Fatalf("%s: %v", tc.uid, err)
		}
		if got != tc.want {
			t.Errorf("role(%s) = %v, want %v", tc.uid, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// CRDT substrate (the thing we are pivoting ON — ygo convergence)
// ---------------------------------------------------------------------------

// TestConvergenceInProcess — two independent documents, concurrent edits,
// y-protocol handshake => identical content. This is the property that
// REPLACES OT transform: no server meshing, no ordering authority.
func TestConvergenceInProcess(t *testing.T) {
	alice := crdt.New(crdt.WithClientID(1))
	bob := crdt.New(crdt.WithClientID(2))

	// concurrent divergent edits (both "offline")
	alice.Transact(func(txn *crdt.Transaction) {
		txn.GetText("content").Insert(txn, 0, "Hello from Alice!", nil)
	})
	bob.Transact(func(txn *crdt.Transaction) {
		txn.GetText("content").Insert(txn, 0, "Hello from Bob!", nil)
		txn.GetMap("meta").Set(txn, "topic", "greeting")
	})

	// handshake (peer-sync pattern: step1 out, apply reply, both ways)
	send := func(from, to *crdt.Doc, fromName string) {
		sv := ysync.EncodeSyncStep1(from)
		reply, err := ysync.ApplySyncMessage(to, sv, fromName)
		if err != nil {
			t.Fatalf("apply step1: %v", err)
		}
		if len(reply) > 0 {
			if r2, err := ysync.ApplySyncMessage(from, reply, "server"); err == nil && len(r2) > 0 {
				if _, err := ysync.ApplySyncMessage(to, r2, "server"); err != nil {
					t.Fatalf("apply r2: %v", err)
				}
			}
		}
	}
	send(alice, bob, "alice")
	send(bob, alice, "bob")

	ca := alice.GetText("content").ToString()
	cb := bob.GetText("content").ToString()
	if ca != cb {
		t.Fatalf("diverged after sync:\n alice %q\n bob   %q", ca, cb)
	}
	for _, want := range []string{"Alice", "Bob"} {
		if !strings.Contains(ca, want) {
			t.Fatalf("converged content %q missing %q (both edits must survive)", ca, want)
		}
	}
}

// TestConvergenceUnderLossyMerge — apply each side's full state in swapped
// order (worst-case interleaving); content must still be identical.
func TestConvergenceUnderLossyMerge(t *testing.T) {
	a := crdt.New(crdt.WithClientID(1))
	b := crdt.New(crdt.WithClientID(2))
	a.Transact(func(t *crdt.Transaction) { t.GetText("c").Insert(t, 0, "AAA", nil) })
	b.Transact(func(t *crdt.Transaction) { t.GetText("c").Insert(t, 0, "BBB", nil) })

	ua := a.EncodeStateAsUpdate()
	ub := b.EncodeStateAsUpdate()
	_ = b.ApplyUpdate(ua) // a -> b
	_ = a.ApplyUpdate(ub) // b -> a (opposite order on the other side)

	if ca, cb := a.GetText("c").ToString(), b.GetText("c").ToString(); ca != cb {
		t.Fatalf("diverged under swapped merge order: %q vs %q", ca, cb)
	}
}

// ---------------------------------------------------------------------------
// persistence (the new history model)
// ---------------------------------------------------------------------------

func TestFilePersistenceRoundtrip(t *testing.T) {
	dir := t.TempDir()
	p1, err := persistence.NewFilePersistence(dir)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	doc := crdt.New()
	doc.Transact(func(t *crdt.Transaction) { t.GetText("content").Insert(t, 0, "first ", nil) })
	// StoreUpdate takes incremental updates; the doc's full state is a valid
	// V1 update for the first write, and the tail-delta pattern below is what
	// a live room produces — use EncodeStateAsUpdate deltas.
	ad1 := persistence.NewLegacyAdapter(p1)
	if err := ad1.StoreUpdate("proj-persist", doc.EncodeStateAsUpdate()); err != nil {
		t.Fatalf("store1: %v", err)
	}
	doc.Transact(func(t *crdt.Transaction) { t.GetText("content").Insert(t, 6, "second", nil) })
	if err := ad1.StoreUpdate("proj-persist", doc.EncodeStateAsUpdate()); err != nil {
		t.Fatalf("store2: %v", err)
	}
	original := doc.GetText("content").ToString()
	if original != "first second" {
		t.Fatalf("fixture drift: %q", original)
	}

	// "process restart": reopen the same directory
	p2, err := persistence.NewFilePersistence(dir)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	stored, err := persistence.NewLegacyAdapter(p2).LoadDoc("proj-persist")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(stored) == 0 {
		t.Fatal("stored state empty after reopen")
	}
	restored := crdt.New()
	if err := restored.ApplyUpdate(stored); err != nil {
		t.Fatalf("apply stored: %v", err)
	}
	if rc := restored.GetText("content").ToString(); rc != original {
		t.Fatalf("restored %q != original %q", rc, original)
	}
}

func TestFilePersistenceUnknownRoom(t *testing.T) {
	p, err := persistence.NewFilePersistence(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	stored, err := persistence.NewLegacyAdapter(p).LoadDoc("no-such-room")
	if err != nil {
		t.Fatalf("load unknown: %v", err)
	}
	if stored != nil {
		t.Fatalf("expected nil state, got %d bytes", len(stored))
	}
}

// TestRoomIsolation — one project's updates must not leak into another room.
func TestRoomIsolation(t *testing.T) {
	dir := t.TempDir()
	p, err := persistence.NewFilePersistence(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	dA := crdt.New()
	dA.Transact(func(t *crdt.Transaction) { t.GetText("content").Insert(t, 0, "project A secret", nil) })
	adA := persistence.NewLegacyAdapter(p)
	if err := adA.StoreUpdate("roomA", dA.EncodeStateAsUpdate()); err != nil {
		t.Fatalf("store: %v", err)
	}

	p2, err := persistence.NewFilePersistence(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	ad2 := persistence.NewLegacyAdapter(p2)
	if leak, err := ad2.LoadDoc("roomB"); err != nil {
		t.Fatalf("load roomB: %v", err)
	} else if leak != nil {
		dB := crdt.New()
		_ = dB.ApplyUpdate(leak)
		if strings.Contains(dB.GetText("content").ToString(), "project A secret") {
			t.Fatal("room isolation violated: roomB loaded roomA content")
		}
	}
}
