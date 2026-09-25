package collab

// roomdoc.go — the shared ROOM-DOC domain operations on any
// persistence.VersionedPersistence. Both runtime surfaces of the D19 collab
// arc depend on these:
//
//   - the ygo WS service (S1/S2) syncs live Y-docs over the same store;
//   - the Go web history REST (S3) seeds, reads, and restores room content.
//
// Keeping the CRDT mechanics here (and NOT in the HTTP layer) means the web
// service never reimplements Yjs encodings — it calls these four ops.

import (
	"context"
	"errors"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
)

// TextType — the shared Y.Text name that holds the room's primary source
// content in the project's collab room. The browser client (Y.Doc with
// Y.Text "content") and every server-side op in this file agree on this
// name; a mismatch would silently yield an empty text.
const TextType = "content"

var (
	// ErrEmptyRoom — the room has no stored versions yet.
	ErrEmptyRoom = errors.New("collab: room has no versions")
	// ErrUnknownVersion — the requested version does not exist in the room log.
	ErrUnknownVersion = errors.New("collab: unknown version")
)

// newServerDoc — server-side transactions use a dedicated client id so
// server writes (seed, restore) carry a stable, recognizable origin distinct
// from any browser client.
func newServerDoc() *crdt.Doc { return crdt.New(crdt.WithClientID(1)) }

// ClientEdit — emulates ONE browser-client edit end-to-end (load head, rewrite
// content to `to`, append the delta as a single version). It is used by the
// test suites of BOTH surfaces (collab + web collabhistory) to produce
// multi-version histories; it mirrors exactly what the y-protocol client
// exchange does (step1/step2 with a delta update), so the histories it
// creates are the real shape the store sees in production.
func ClientEdit(ctx context.Context, store persistence.VersionedPersistence, room, from, to string) (persistence.Version, error) {
	lr, err := store.Load(ctx, room)
	if err != nil {
		return 0, err
	}
	d := crdt.New(crdt.WithClientID(7))
	if err := crdt.ApplyUpdateV1(d, lr.Update, nil); err != nil {
		return 0, err
	}
	if d.GetText(TextType).ToString() != from {
		return 0, errors.New("collab: ClientEdit: head content does not match 'from' parameter")
	}
	sv := d.StateVector().Clone()
	d.Transact(func(txn *crdt.Transaction) {
		t := txn.GetText(TextType)
		if t.Len() > 0 {
			t.Delete(txn, 0, t.Len())
		}
		t.Insert(txn, 0, to, nil)
	})
	return store.AppendUpdate(ctx, room, crdt.EncodeStateAsUpdateV1(d, sv))
}

// SeedTextContent — create the room's first version holding `text`. Used
// when a project opens in the Yjs world for the first time (the new Y doc is
// seeded from the current file content — D19: OT history is NOT portable).
// When the room already has versions this is a no-op returning the head.
func SeedTextContent(ctx context.Context, store persistence.VersionedPersistence, room, text string) (persistence.Version, error) {
	lr, err := store.Load(ctx, room)
	if err != nil {
		return 0, err
	}
	if lr.Version > 0 {
		return lr.Version, nil
	}
	d := newServerDoc()
	d.Transact(func(txn *crdt.Transaction) {
		txn.GetText(TextType).Insert(txn, 0, text, nil)
	})
	return store.AppendUpdate(ctx, room, d.EncodeStateAsUpdate())
}

// TextAt — the visible text content of the room's Y.Text at version v
// (MaterializeAt(v) replayed, then read). v must be >= 1 and present.
func TextAt(ctx context.Context, store persistence.VersionedPersistence, room string, v persistence.Version) (string, error) {
	if v == 0 {
		return "", ErrUnknownVersion
	}
	_, _, ok, err := store.GetUpdate(ctx, room, v)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrUnknownVersion
	}
	snap, err := store.MaterializeAt(ctx, room, v)
	if err != nil {
		if errors.Is(err, persistence.ErrRoomNotFound) {
			return "", ErrEmptyRoom
		}
		return "", err
	}
	d := crdt.New()
	if err := crdt.ApplyUpdateV1(d, snap, nil); err != nil {
		return "", err
	}
	return d.GetText(TextType).ToString(), nil
}

// HeadText — the room's current (head) text content + head version, read
// straight from the loaded state (no per-version merge). Empty room →
// ("", 0, nil).
func HeadText(ctx context.Context, store persistence.VersionedPersistence, room string) (string, persistence.Version, error) {
	lr, err := store.Load(ctx, room)
	if err != nil {
		return "", 0, err
	}
	if lr.Version == 0 {
		return "", 0, nil
	}
	d := newServerDoc()
	if err := crdt.ApplyUpdateV1(d, lr.Update, nil); err != nil {
		return "", 0, err
	}
	return d.GetText(TextType).ToString(), lr.Version, nil
}

// RestoreToVersion — CRDT-correct "restore version v into the present" (yhub
// semantics: restore creates a NEW version carrying v's content). Mechanism:
// load the head state, in ONE transaction delete the entire current text and
// insert content(v), encode only the delta since the pre-restore state vector,
// and append that as one fresh version. Every live client converges to
// content(v) through ordinary update delivery — no time-travel, no deletes
// the CRDT cannot express. When content(v) already equals the head content
// no new version is created (head returned, unchanged).
func RestoreToVersion(ctx context.Context, store persistence.VersionedPersistence, room string, v persistence.Version) (persistence.Version, error) {
	lr, err := store.Load(ctx, room)
	if err != nil {
		return 0, err
	}
	if lr.Version == 0 {
		return 0, ErrEmptyRoom
	}
	target, err := TextAt(ctx, store, room, v)
	if err != nil {
		return 0, err
	}
	d := newServerDoc()
	if err := crdt.ApplyUpdateV1(d, lr.Update, nil); err != nil {
		return 0, err
	}
	if d.GetText(TextType).ToString() == target {
		return lr.Version, nil // already at that content
	}
	svBefore := d.StateVector().Clone()
	d.Transact(func(txn *crdt.Transaction) {
		t := txn.GetText(TextType)
		if t.Len() > 0 {
			t.Delete(txn, 0, t.Len())
		}
		t.Insert(txn, 0, target, nil)
	})
	upd := crdt.EncodeStateAsUpdateV1(d, svBefore)
	if len(upd) == 0 {
		return lr.Version, nil
	}
	return store.AppendUpdate(ctx, room, upd)
}
