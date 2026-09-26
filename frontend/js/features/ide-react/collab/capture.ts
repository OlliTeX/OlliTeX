// D40 (d5 pending piece → P2 tail): editor-side tracked-change capture
// contract.
//
// A local edit (CM6 spans in OLD-space coordinates — the same
// `LocalChange` shape the D24 bridge consumes) drives the D40 d5 create
// surface (POST /project/:pid/doc/:doc/changes). This module owns the
// PURE span→body mapping; the host listener (source-editor/extensions/
// realtime.ts `trackedChangesCapture`) gates it on the session's
// track-changes state and performs the best-effort REST call.

import type { LocalChange } from "./sync";

// TrackedChangeBody — the d5 create-surface bodies (Go changesCreate):
//   insert ⇒ ZERO-WIDTH {content, start, end: start}
//   delete ⇒ {start, end} over the removed [start,end)
// A replace span (removed range + inserted text) records BOTH sides,
// delete first — the D40 change record is single-kinded (insert|delete),
// so a replace is two records (d2: server applies each, idempotent).
export type TrackedChangeBody =
  | { content: string; start: number; end: number }
  | { start: number; end: number };

// spanBodies — the capture payloads for a local edit's spans (pure;
// unit-tested in collab/test/capture.test.ts). Spans are in ascending
// document order per edit (LocalChange invariant); bodies keep that
// order, with delete-before-insert within a single span.
export function spanBodies(spans: LocalChange[]): TrackedChangeBody[] {
  const out: TrackedChangeBody[] = [];
  for (const s of spans) {
    if (s.to > s.from) {
      out.push({ start: s.from, end: s.to });
    }
    if (s.insert.length > 0) {
      out.push({ content: s.insert, start: s.from, end: s.from });
    }
  }
  return out;
}
