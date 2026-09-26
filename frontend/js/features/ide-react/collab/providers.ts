import { IndexeddbPersistence } from "y-indexeddb";
import { WebsocketProvider } from "y-websocket";
import type * as Y from "yjs";

// collabEndpoint — the server contract (D19): room = projectId, WS path
// `/collab/{projectId}` on the Go collab service. Cookie auth is
// same-origin: the browser sends the session cookie with the WS handshake
// automatically (no auth query params — the server rejects them).
export interface CollabEndpoint {
  // ws://host[:port] — the base for the WebsocketProvider (NO room).
  wsBaseUrl: string;
  // collab/<projectId> — the room the provider connects to. NOTE: NO
  // leading slash — y-websocket 3.x composes the final URL as
  // `serverUrl + '/' + roomname` (src/y-websocket.js:459), so a leading
  // slash here yields `ws://host//collab/…` which the reverse proxy does
  // NOT route to the collab service (observed 307 on the e2e stack).
  room: string;
}

export function collabEndpoint(
  projectId: string,
  base?: string,
): CollabEndpoint {
  const b =
    base ?? (typeof location !== "undefined" ? location.href : undefined);
  return {
    wsBaseUrl: wsBaseUrl(b),
    room: `collab/${encodeURIComponent(projectId)}`,
  };
}

// wsBaseUrl — derive the ws(s):// base from an http(s) page URL (or any
// ws(s) URL). Pure — unit-testable.
export function wsBaseUrl(raw: string | undefined): string {
  if (raw == null || raw === "") return "ws://127.0.0.1";
  const u = new URL(raw);
  const proto = u.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${u.host}`;
}

export interface Providers {
  ws: unknown | null;
  idb: unknown | null;
}

// attachProviders — wire the offline (IndexedDB) + network (WebSocket)
// providers to an existing Y.Doc. Browser-only (each provider is skipped
// when the browser absence is detected — note: modern Node exposes a native
// WebSocket global, so the gate is the browser window, not the substrate):
// in Node this returns {ws:null,idb:null} and the engine stays fully
// importable/testable headless.
//
// Order: IndexedDB first so a returning client sees its last known state
// immediately, then the WS provider syncs the delta (yjs merges both).
export function attachProviders(
  doc: Y.Doc,
  projectId: string,
  base?: string,
): Providers {
  let idb: unknown = null;
  if (typeof window !== "undefined" && typeof indexedDB !== "undefined") {
    idb = new IndexeddbPersistence(`ollitex-collab-${projectId}`, doc);
  }
  let ws: unknown = null;
  if (typeof window !== "undefined") {
    const ep = collabEndpoint(projectId, base);
    // `params: {}` — the server does not read query params; the session
    // cookie is the credential (SameSite; same-origin).
    ws = new WebsocketProvider(ep.wsBaseUrl, ep.room, doc, {
      connect: true,
      params: {},
    });
  }
  return { ws, idb };
}
