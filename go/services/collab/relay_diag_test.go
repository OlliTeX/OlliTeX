package collab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/encoding"
	ysync "github.com/reearth/ygo/sync"
)

// TestRelay_TwoPeersEditAndPersist — live-contract hermitic pin (verified
// against the real stack 2026-09): two peers in one seeded room; peer A
// appends and the update must (1) relay to peer B and (2) persist as a new
// version. Modeled on ygo's own TestInteg suite (one goroutine per conn;
// no cross-goroutine doc access after Transact).
func TestRelay_TwoPeersEditAndPersist(t *testing.T) {
	auth := &fakeAuth{sessionUID: "ownerA", projRole: map[string]Role{"diagroom": ReadWrite}}
	svc, err := New(Options{
		Auth:    auth,
		DataDir: t.TempDir(),
		SeedFn:  func(ctx context.Context, room string) (string, error) { return "seed-line\n", nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(svc)
	defer ts.Close()

	// mkPeer: connect + complete the y-protocol handshake; returns the conn
	// and the peer's doc (fully seeded). All follow-up reads use a fresh
	// per-call deadline.
	mkPeer := func(label string) (*websocket.Conn, *crdt.Doc) {
		t.Helper()
		c, _, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL(ts.URL, "diagroom"), http.Header{})
		if err != nil {
			t.Fatalf("%s dial: %v", label, err)
		}
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		doc := crdt.New()
		handshook := true
		for handshook {
			_, raw, err := c.ReadMessage()
			if err != nil {
				t.Fatalf("%s handshake read: %v", label, err)
			}
			dec := encoding.NewDecoder(raw)
			outer, _ := dec.ReadVarUint()
			if outer != 0 {
				continue // awareness / other — skip
			}
			inner := dec.RemainingBytes()
			st, _, err := ysync.ReadSyncMessage(inner)
			if err != nil {
				t.Fatalf("%s sync parse: %v", label, err)
			}
			if st == ysync.MsgSyncStep1 {
				if err := c.WriteMessage(websocket.BinaryMessage, encoding.EncodeBytes(func(enc *encoding.Encoder) {
					enc.WriteVarUint(0)
					enc.WriteRaw(ysync.EncodeSyncStep1(doc))
				})); err != nil {
					t.Fatalf("%s reply step1: %v", label, err)
				}
				continue
			}
			if _, err := ysync.ApplySyncMessage(doc, inner, label); err != nil {
				t.Fatalf("%s apply %d: %v", label, st, err)
			}
			if st == ysync.MsgSyncStep2 {
				handshook = false
			}
		}
		c.SetReadDeadline(time.Time{})
		return c, doc
	}

	a, docA := mkPeer("A")
	defer a.Close()
	b, docB := mkPeer("B")
	defer b.Close()

	if got := docA.GetText(TextType).ToString(); got != "seed-line\n" {
		t.Fatalf("A initial = %q, want seed %q", got, "seed-line\n")
	}

	// A edits (append) and pushes the update (the ygo test-suite framing:
	// grab the Text handle OUTSIDE the txn — doc locks are not reentrant).
	txtA := docA.GetText(TextType)
	docA.Transact(func(txn *crdt.Transaction) {
		txtA.Insert(txn, 10, "% live-edit\n", nil)
	})
	if err := a.WriteMessage(websocket.BinaryMessage, encoding.EncodeBytes(func(enc *encoding.Encoder) {
		enc.WriteVarUint(0)
		enc.WriteRaw(ysync.EncodeUpdate(crdt.EncodeStateAsUpdateV1(docA, nil)))
	})); err != nil {
		t.Fatalf("A send update: %v", err)
	}

	// B waits for the relayed update (2s budget) and applies it.
	relayed := false
	deadline := time.Now().Add(2 * time.Second)
	for !relayed {
		b.SetReadDeadline(deadline)
		_, raw, err := b.ReadMessage()
		if err != nil {
			break
		}
		dec := encoding.NewDecoder(raw)
		outer, _ := dec.ReadVarUint()
		if outer != 0 {
			continue
		}
		full := dec.RemainingBytes()
		st, _, err := ysync.ReadSyncMessage(full)
		if err != nil {
			continue
		}
		if st == ysync.MsgUpdate {
			// ApplySyncMessage expects the FULL sync message (type + payload).
			if _, err := ysync.ApplySyncMessage(docB, full, "srv->B"); err != nil {
				t.Fatalf("B apply relay: %v", err)
			}
			relayed = true
		}
	}
	if !relayed {
		t.Fatalf("update NOT relayed to B (no frame within 2s)")
	}
	if got := docB.GetText(TextType).ToString(); got != "seed-line\n% live-edit\n" {
		t.Fatalf("B content = %q, want relayed edit", got)
	}

	// Both leave → room evicts + flushes → store must hold seed + edit.
	a.Close()
	b.Close()
	time.Sleep(300 * time.Millisecond)
	metas, err := svc.Store().ListVersions(context.Background(), "diagroom")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("versions = %d, want 2 (seed + edit)", len(metas))
	}
}
