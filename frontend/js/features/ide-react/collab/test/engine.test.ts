import * as Y from "yjs";
import { describe, expect, it } from "vitest";

import { collabEndpoint, wsBaseUrl, attachProviders } from "../providers";
import { createEngine } from "../engine";
import { TEXT_TYPE } from "../text-type";

describe("collab/providers — network contract", () => {
  it("waBaseUrl: https page → wss, http → ws, ports preserved", () => {
    expect(wsBaseUrl("https://latex.example.edu")).toBe(
      "wss://latex.example.edu",
    );
    expect(wsBaseUrl("http://latex.example.edu:3450")).toBe(
      "ws://latex.example.edu:3450",
    );
    expect(wsBaseUrl("ws://127.0.0.1:3450")).toBe("ws://127.0.0.1:3450");
    expect(wsBaseUrl(undefined)).toBe("ws://127.0.0.1");
  });

  it("collabEndpoint: room = collab/<projectId> (no leading slash — y-websocket 3.x appends `serverUrl + '/' + roomname`, so a leading slash would double-slash and the proxy would not route it)", () => {
    const pid = "652c8c2b9c1a0d4f5e6a7b8c";
    const ep = collabEndpoint(pid, "https://latex.example.edu");
    expect(ep.room).toBe(`collab/${pid}`);
    expect(ep.wsBaseUrl).toBe("wss://latex.example.edu");
  });

  it("collabEndpoint: encodes non-hex room ids", () => {
    const ep = collabEndpoint("a b/c", "http://localhost");
    expect(ep.room).toBe(`collab/${encodeURIComponent("a b/c")}`);
  });

  it("attachProviders: headless (no browser) → both null", () => {
    // There is no browser context in Node (the provider is gated on a
    // browser window, where its substrate exists); the guards must skip
    // cleanly — this is what keeps the engine importable + testable headless.
    const doc = new Y.Doc();
    const p = attachProviders(doc, "pid-1", "ws://127.0.0.1");
    expect(p.ws).toBeNull();
    expect(p.idb).toBeNull();
  });
});

describe("collab/engine — facade (offline/headless)", () => {
  it("wires doc + text (content) + undo + sync + cmBridge; destroy is safe", () => {
    const sink = {
      s: "",
      read: () => sink.s,
      write: (v: string) => {
        sink.s = v;
      },
    };
    const eng = createEngine("pid-1", sink, { offline: true });
    expect(eng.text).toBeTruthy();
    expect(eng.doc.getText(TEXT_TYPE)).toBe(eng.text);
    expect(eng.undo).toBeTruthy();
    expect(eng.sync).toBeTruthy();
    expect(eng.providers).toEqual({ ws: null, idb: null });
    expect(eng.cmBridge).toBeTruthy();
    eng.destroy();
    eng.destroy(); // idempotent
  });

  it("local edit via the sync core lands in the Y.Text", () => {
    const sink = { s: "" };
    const eng = createEngine(
      "pid-1",
      {
        read: () => sink.s,
        write: (v: string) => {
          sink.s = v;
        },
      },
      { offline: true },
    );
    // Server seed arrives (D19: server is the single content source):
    eng.doc.transact(() => eng.text.insert(0, "base\n"), "server");
    expect(sink.s).toBe("base\n"); // observer mirrored the seed
    // Local edit at the cursor: "base\n" = b(0) a(1) s(2) e(3) \n(4) —
    // typing "X" before the newline inserts at index 4. CM6 flow: the
    // editor applies its own change first (sink), then the listener
    // replays the span onto the Y.Text.
    sink.s = "baseX\n";
    eng.sync.applyLocal([{ from: 4, to: 4, insert: "X" }]);
    expect(eng.text.toString()).toBe("baseX\n");
    expect(sink.s).toBe("baseX\n");
    // Programmatic undo seam (y-undo): revert the local edit — the mirror
    // also re-derives the host (undo runs as a remote-origin transaction).
    eng.undo.undo();
    expect(eng.text.toString()).toBe("base\n");
    expect(sink.s).toBe("base\n");
    eng.destroy();
  });
});
