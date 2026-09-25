package collab

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// clientEdit — simulates a browser client: loads room state, applies an
// append as a distinct client, appends the resulting delta as one version.
func clientEdit(t *testing.T, ctx context.Context, store persistence.VersionedPersistence, room, insert string) persistence.Version {
	t.Helper()
	lr, err := store.Load(ctx, room)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d := crdt.New(crdt.WithClientID(7))
	if err := crdt.ApplyUpdateV1(d, lr.Update, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	sv := d.StateVector().Clone()
	d.Transact(func(txn *crdt.Transaction) {
		txt := txn.GetText(TextType)
		txt.Insert(txn, txt.Len(), insert, nil)
	})
	v, err := store.AppendUpdate(ctx, room, crdt.EncodeStateAsUpdateV1(d, sv))
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	return v
}

const rdInitial = "\\documentclass{article}\n\\begin{document}\nHi.\n\\end{document}\n"

// runRoomDocScenario — the full seed/edit/read/restore scenario; identical
// semantics must hold over any VersionedPersistence (File or Mongo).
func runRoomDocScenario(t *testing.T, ctx context.Context, store persistence.VersionedPersistence) {
	t.Helper()
	const room = "proj-rd"
	v1, err := SeedTextContent(ctx, store, room, rdInitial)
	if err != nil || v1 != 1 {
		t.Fatalf("seed = (%d, %v), want (1, nil)", v1, err)
	}
	if again, err := SeedTextContent(ctx, store, room, "IGNORED"); err != nil || again != 1 {
		t.Fatalf("re-seed = (%d, %v), want (1, nil)", again, err)
	}
	v2 := clientEdit(t, ctx, store, room, " [edit2]")
	v3 := clientEdit(t, ctx, store, room, " [edit3]")
	if v2 != 2 || v3 != 3 {
		t.Fatalf("edit versions = %d/%d, want 2/3", v2, v3)
	}
	for v, want := range map[persistence.Version]string{
		1: rdInitial,
		2: rdInitial + " [edit2]",
		3: rdInitial + " [edit2] [edit3]",
	} {
		got, err := TextAt(ctx, store, room, v)
		if err != nil {
			t.Fatalf("TextAt(%d) err: %v", v, err)
		}
		if got != want {
			t.Fatalf("TextAt(%d) = %q, want %q", v, got, want)
		}
	}
	if _, err := TextAt(ctx, store, room, 99); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("TextAt(99) err = %v, want ErrUnknownVersion", err)
	}
	if _, err := TextAt(ctx, store, "proj-never-opened", 1); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("TextAt(unknown room) err = %v, want ErrUnknownVersion", err)
	}

	// Restore-to-v1 appends a fresh version carrying v1's content; it does
	// NOT prune (older versions stay readable).
	v4, err := RestoreToVersion(ctx, store, room, 1)
	if err != nil || v4 != 4 {
		t.Fatalf("restore-to-v1 = (%d, %v), want (4, nil)", v4, err)
	}
	if got, err := TextAt(ctx, store, room, 4); err != nil || got != rdInitial {
		t.Fatalf("head after restore = %q, %v; want v1 content", got, err)
	}
	if got, err := TextAt(ctx, store, room, 3); err != nil || got != rdInitial+" [edit2] [edit3]" {
		t.Fatalf("v3 after restore = %q, %v (restore must not rewrite the log)", got, err)
	}
	// Restore-to-head-content = no new version.
	if v, err := RestoreToVersion(ctx, store, room, 4); err != nil || v != 4 {
		t.Fatalf("restore-to-head = (%d, %v), want (4, nil)", v, err)
	}
	// Restore-to-v2 from the restored head = back to v2 content, new version.
	v5, err := RestoreToVersion(ctx, store, room, 2)
	if err != nil || v5 != 5 {
		t.Fatalf("restore-to-v2 = (%d, %v), want (5, nil)", v5, err)
	}
	if got, err := TextAt(ctx, store, room, 5); err != nil || got != rdInitial+" [edit2]" {
		t.Fatalf("head after restore-to-v2 = %q, %v", got, err)
	}
	// Live-doc convergence: a client holding the HEAD state, applying the
	// restore update, must see exactly the restored content.
	lr, err := store.Load(ctx, room)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	live := crdt.New(crdt.WithClientID(9))
	if err := crdt.ApplyUpdateV1(live, lr.Update, nil); err != nil {
		t.Fatalf("client apply: %v", err)
	}
	if got := live.GetText(TextType).ToString(); got != rdInitial+" [edit2]" {
		t.Fatalf("live head content = %q, want v2 content", got)
	}
}

// TestTextTypeContract — pins the cross-language wire contract for the room's
// content type name. The CLIENT pins the same constant in
// frontend/js/features/ide-react/collab/text-type.test.ts (TEXT_TYPE ===
// "content"). The server seeds/versions/restores doc.getText(TextType); the
// client mirrors doc.getText(TEXT_TYPE). Rename only in BOTH files in the
// same commit — a one-sided rename is a silent content-loss bug (the room
// would read/write a different type and always appear empty).
func TestTextTypeContract(t *testing.T) {
	if TextType != "content" {
		t.Fatalf("TextType = %q, want \"content\" (wire contract; client pins the same string)", TextType)
	}
}

func TestRoomDocFilePersistence(t *testing.T) {
	ctx := context.Background()
	store, err := persistence.NewFilePersistence(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	runRoomDocScenario(t, ctx, store)
}

func TestRoomDocRestoreEmptyRoom(t *testing.T) {
	ctx := context.Background()
	store, err := persistence.NewFilePersistence(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := RestoreToVersion(ctx, store, "proj-none", 1); !errors.Is(err, ErrEmptyRoom) {
		t.Fatalf("restore on empty room err = %v, want ErrEmptyRoom", err)
	}
}

// TestRoomDocMongoStore — same scenario over the PRODUCTION store, live Mongo.
func TestRoomDocMongoStore(t *testing.T) {
	uri := os.Getenv("TEST_MONGO_URL")
	if uri == "" {
		uri = os.Getenv("MONGO_CONNECTION_STRING")
	}
	if uri == "" {
		uri = os.Getenv("OVERLEAF_MONGO_URL")
	}
	if uri == "" {
		t.Skip("set TEST_MONGO_URL (or MONGO_CONNECTION_STRING/OVERLEAF_MONGO_URL) to run the Mongo room-doc suite")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := c.Ping(ctx, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	dbName := "test-collab-" + os.Getenv("USER")
	if dbName == "test-collab-" {
		dbName = "test-collab"
	}
	db := c.Database(dbName)
	defer func() { _ = db.Drop(ctx) }()
	st, err := NewMongoStore(ctx, db)
	if err != nil {
		t.Fatalf("NewMongoStore: %v", err)
	}
	runRoomDocScenario(t, ctx, st)
}
