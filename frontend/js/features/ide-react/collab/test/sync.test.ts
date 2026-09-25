import * as Y from "yjs";
import { describe, expect, it } from "vitest";

import {
  LOCAL_ORIGIN,
  YTextSync,
  type LocalChange,
  type TextSink,
} from "../sync";
import { TEXT_TYPE } from "../text-type";

// A fake host editor (the TextSink contract).
function editor(initial = ""): TextSink & { writes: number } {
  let s = initial;
  return {
    read: () => s,
    write: (v) => {
      s = v;
    },
    writes: 0,
  };
}
function trackedEditor(initial = "") {
  const e = editor(initial);
  const base = e.write;
  e.write = (v: string) => {
    e.writes++;
    base(v);
  };
  return e;
}

function newRoom(seed?: string) {
  const doc = new Y.Doc();
  const text = doc.getText(TEXT_TYPE);
  if (seed !== undefined) {
    doc.transact(() => text.insert(0, seed), LOCAL_ORIGIN);
  }
  return { doc, text };
}

// peer = an EMPTY client (D19: clients never pre-fill) that has JOINED a
// server-seeded room: initial sync delivered the seed and the observer
// mirrored it into the host — exactly the S3 seed path. `room` is the
// server-side room object ({doc, text}) from newRoom().
function peer(room: { doc: Y.Doc }) {
  const { doc, text } = newRoom();
  const sink = trackedEditor();
  const sync = new YTextSync(doc, text, sink);
  Y.applyUpdate(doc, Y.encodeStateAsUpdate(room.doc));
  return { doc, text, sink, sync };
}

// relay = server round-trip: take A's full state, apply it to B (the ygo
// server's persistence/relay is exactly this update application, and the
// state vectors are what MongoStore versions).
function relay(a: Y.Doc, b: Y.Doc) {
  Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
}

// svHex — the doc's state vector as a stable hex string (convergence
// assertions must compare vectors, not just text).
function svHex(doc: Y.Doc): string {
  return Buffer.from(Y.encodeStateVector(doc)).toString("hex");
}

// hostEdit = the exact CodeMirror flow for a local edit: the dispatch
// applies the change to the editor FIRST (here: the sink string), and
// THEN the updateListener replays the same spans onto the Y.Text. The
// fake sink models the editor's document state.
function hostEdit(
  p: {
    sink: { read: () => string; write: (s: string) => void };
    sync: YTextSync;
  },
  spans: LocalChange[],
) {
  let s = p.sink.read();
  for (const sp of spans) {
    s = s.slice(0, sp.from) + sp.insert + s.slice(sp.to);
  }
  p.sink.write(s);
  p.sync.applyLocal(spans);
}

describe("collab/sync — D24 bridge semantics", () => {
  it("seed: an empty joining client mirrors the server-seeded room (S3 seed path)", () => {
    const server = newRoom("v1 content\n");
    // A client opens the room EMPTY (D19 — no prefill); the initial sync
    // from the server delivers the seed and the observer mirrors it into
    // the host editor.
    const { doc, text } = newRoom();
    const sink = trackedEditor();
    const sync = new YTextSync(doc, text, sink);
    relay(server.doc, doc);
    expect(sink.read()).toBe("v1 content\n");
    expect(text.toString()).toBe("v1 content\n");
    expect(sync).toBeTruthy(); // bound: remote changes would mirror into the host
  });

  it("local edit publishes granular ops; a second peer converges exactly", () => {
    const server = newRoom("hello\n");
    const a = peer(server);
    const b = peer(server);
    // A types " world" at the cursor (old-space "hello\n": insert at 5 —
    // exactly what CodeMirror's iterChanges yields for that edit):
    const spans: LocalChange[] = [{ from: 5, to: 5, insert: " world" }];
    hostEdit(a, spans);
    expect(a.sink.read()).toBe("hello world\n");
    relay(a.doc, b.doc);
    expect(b.sink.read()).toBe("hello world\n");
    // B types "!" before the newline ("hello world\n" index 11):
    hostEdit(b, [{ from: 11, to: 11, insert: "!" }]);
    expect(b.sink.read()).toBe("hello world!\n");
    relay(b.doc, a.doc);
    expect(a.sink.read()).toBe("hello world!\n");
    // Converged: identical text, identical state vectors.
    expect(svHex(a.doc)).toBe(svHex(b.doc));
  });

  it("concurrent disjoint edits merge — BOTH survive, NO duplication", () => {
    // One shared server seed (D19: the room content has ONE origin), both
    // clients join it, then edit concurrently against the same base:
    const server = newRoom("abc\n");
    const a = peer(server);
    const b = peer(server);
    hostEdit(a, [{ from: 4, to: 4, insert: "A-edit" }]);
    hostEdit(b, [{ from: 4, to: 4, insert: "B-edit" }]);
    relay(a.doc, b.doc);
    relay(b.doc, a.doc);
    const merged = a.sink.read();
    expect(merged).toContain("A-edit");
    expect(merged).toContain("B-edit");
    expect(merged).toContain("abc\n");
    // Convergence: one document on both sides.
    expect(a.sink.read()).toBe(b.sink.read());
    // No whole-document duplication — the shared base survives exactly once.
    expect(merged.match(/abc\n/g)?.length ?? 0).toBe(1);
  });

  it("concurrent whole-document rewrites CONVERGE (peers never diverge)", () => {
    // Pathological case: both peers rewrite the entire document against the
    // same shared base. CRDT sequence semantics may interleave the two
    // texts (any text CRDT does — the documented trade-off), but the
    // invariant that MUST hold is convergence: after full sync both peers
    // hold the SAME document, and the shared base appears exactly once.
    const server = newRoom("one\n");
    const a = peer(server);
    const b = peer(server);
    hostEdit(a, [{ from: 0, to: 4, insert: "alpha\n" }]);
    hostEdit(b, [{ from: 0, to: 4, insert: "beta\n" }]);
    relay(a.doc, b.doc);
    relay(b.doc, a.doc);
    expect(a.sink.read()).toBe(b.sink.read());
    expect(a.text.toString()).toBe(b.text.toString());
    // Both edits survive exactly once; the doubly-deleted base is gone.
    const merged = a.text.toString();
    expect(merged).toContain("alpha\n");
    expect(merged).toContain("beta\n");
    expect(merged.match(/one\n/g)?.length ?? 0).toBe(0);
    expect(merged.match(/alpha\n/g)?.length ?? 0).toBe(1);
    expect(merged.match(/beta\n/g)?.length ?? 0).toBe(1);
  });

  it("idempotent: a no-change local apply transacts NOTHING (no server version)", () => {
    const server = newRoom("x\n");
    const a = peer(server);
    let updates = 0;
    a.doc.on("update", () => updates++);
    a.sync.applyLocal([]); // zero spans = explicit no-op
    a.sync.applyLocal([{ from: 2, to: 2, insert: "" }]); // empty span = no-op
    expect(updates).toBe(0);
    expect(a.sink.read()).toBe("x\n");
  });

  it("remote mirroring of an unchanged doc is a no-op for the host", () => {
    const server = newRoom("fixed\n");
    const a = peer(server);
    const writesAfterFirst = a.sink.writes;
    Y.applyUpdate(a.doc, Y.encodeStateAsUpdate(server.doc)); // same state again
    expect(a.sink.writes).toBe(writesAfterFirst);
  });

  it("delete-local: removing text publishes a delete; peers converge empty", () => {
    const server = newRoom("drop me\n");
    const a = peer(server);
    const b = peer(server);
    hostEdit(a, [{ from: 0, to: 8, insert: "" }]); // user selects all + deletes
    expect(a.sink.read()).toBe("");
    relay(a.doc, b.doc);
    expect(b.sink.read()).toBe("");
    expect(a.text.toString()).toBe(b.text.toString());
  });
});
