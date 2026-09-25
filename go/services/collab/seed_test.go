package collab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/encoding"
	ysync "github.com/reearth/ygo/sync"
)

const seedText = "% OlliTeX — seeded\n\\begin{document}\n42\n\\end{document}\n"

func TestSeedHookDirect(t *testing.T) {
	auth := &fakeAuth{sessionUID: "ownerA", projRole: map[string]Role{"r1": ReadWrite}}
	svc, err := New(Options{
		Auth:    auth,
		DataDir: t.TempDir(),
		SeedFn:  func(ctx context.Context, room string) (string, error) { return seedText, nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	})
	if svc.Server.OnLoadDocument == nil {
		t.Fatal("OnLoadDocument hook not wired")
	}
	ctx := context.Background()
	doc := crdt.New()
	if err := svc.Server.OnLoadDocument(ctx, "r1", doc); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if got := doc.GetText(TextType).ToString(); got != seedText {
		t.Fatalf("hook seeded doc = %q, want %q", got, seedText)
	}
	got, err := TextAt(ctx, svc.Store(), "r1", 1)
	if err != nil {
		t.Fatalf("TextAt(1): %v", err)
	}
	if got != seedText {
		t.Fatalf("store version 1 = %q, want seed text", got)
	}
	// Second load: room has history now → hook must NOT double-seed.
	if err := svc.Server.OnLoadDocument(ctx, "r1", crdt.New()); err != nil {
		t.Fatalf("hook(2nd): %v", err)
	}
	metas, err := svc.Store().ListVersions(ctx, "r1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("versions after 2nd hook = %d, want 1 (no double seed)", len(metas))
	}
}

func TestSeedHookFailClosed(t *testing.T) {
	sentinel := errors.New("content source unavailable")
	svc, err := New(Options{
		Auth:    &fakeAuth{sessionUID: "u"},
		DataDir: t.TempDir(),
		SeedFn:  func(ctx context.Context, room string) (string, error) { return "", sentinel },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = svc.Shutdown(ctx)
	})
	if err := svc.Server.OnLoadDocument(context.Background(), "rX", crdt.New()); !errors.Is(err, sentinel) {
		t.Fatalf("hook err = %v, want seed failure to propagate (fail-closed room load)", err)
	}
}

// TestSeedOverWS — full protocol path: a peer connecting to an EMPTY room
// must receive the seed as part of the initial sync (outer frame:
// VarUint(msgSync=0) + raw sync message). A second peer gets identical
// content.
func TestSeedOverWS(t *testing.T) {
	auth := &fakeAuth{sessionUID: "ownerA", projRole: map[string]Role{"seedroom": ReadWrite}}
	svc, err := New(Options{
		Auth:    auth,
		DataDir: t.TempDir(),
		SeedFn:  func(ctx context.Context, room string) (string, error) { return seedText, nil },
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

	readSeed := func(t *testing.T) string {
		t.Helper()
		c, _, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL(ts.URL, "seedroom"), http.Header{})
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer c.Close()
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		readFrame := func() []byte {
			_, raw, err := c.ReadMessage()
			if err != nil {
				t.Fatalf("read frame: %v", err)
			}
			dec := encoding.NewDecoder(raw)
			msgType, err := dec.ReadVarUint() // outer: 0 = sync
			if err != nil || msgType != 0 {
				t.Fatalf("frame msgType = %d, %v; want sync(0)", msgType, err)
			}
			return dec.RemainingBytes()
		}
		doc := crdt.New()
		inner := readFrame()
		st, _, err := ysync.ReadSyncMessage(inner)
		if err != nil || st != ysync.MsgSyncStep1 {
			t.Fatalf("first sync = type %d, %v; want step1", st, err)
		}
		// y-protocol: reply to a step1 (the server's state vector) with OUR OWN
		// step1 (our state vector). The server then sends the step2 we lack.
		c.WriteMessage(websocket.BinaryMessage, encoding.EncodeBytes(func(enc *encoding.Encoder) {
			enc.WriteVarUint(0)
			enc.WriteRaw(ysync.EncodeSyncStep1(doc))
		}))
		// Server must now deliver what we were missing: the seeded content.
		for i := 0; i < 3; i++ {
			inner = readFrame()
			st, _, err := ysync.ReadSyncMessage(inner)
			if err != nil {
				t.Fatalf("sync frame %d: %v", i, err)
			}
			if st == ysync.MsgSyncStep1 {
				c.WriteMessage(websocket.BinaryMessage, encoding.EncodeBytes(func(enc *encoding.Encoder) {
					enc.WriteVarUint(0)
					enc.WriteRaw(ysync.EncodeSyncStep1(doc))
				}))
				continue
			}
			if _, err := ysync.ApplySyncMessage(doc, inner, "client-probe"); err != nil {
				t.Fatalf("apply %d: %v", st, err)
			}
			break
		}
		return doc.GetText(TextType).ToString()
	}

	if got := readSeed(t); got != seedText {
		t.Fatalf("first peer initial content = %q, want seed %q", got, seedText)
	}
	if got := readSeed(t); got != seedText {
		t.Fatalf("second peer initial content = %q, want seed %q", got, seedText)
	}
	// Server persisted exactly ONE version (the seed) — both peers'
	// connections produced no extra writes.
	metas, err := svc.Store().ListVersions(context.Background(), "seedroom")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("versions = %d, want 1 (single seed)", len(metas))
	}
}
