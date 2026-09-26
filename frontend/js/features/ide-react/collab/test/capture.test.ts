import { describe, expect, it } from "vitest";

import { spanBodies, type TrackedChangeBody } from "../capture";

// D40 (d5 pending piece → editor-side capture) — the span → create-surface
// body mapping is the contract surface (d5 pin, Go changesCreate):
//   insert ⇒ zero-width {content, start, end: start}
//   delete ⇒ {start, end} over the removed range
//   replace ⇒ BOTH, delete first (a D40 record is single-kinded)
describe("spanBodies (D40 tracked-change capture)", () => {
  it("maps a pure insert to a zero-width insert body", () => {
    expect(spanBodies([{ from: 4, to: 4, insert: "hello" }])).toEqual<
      TrackedChangeBody[]
    >([{ content: "hello", start: 4, end: 4 }]);
  });

  it("maps a pure delete to a range body with no content", () => {
    expect(spanBodies([{ from: 0, to: 5, insert: "" }])).toEqual([
      { start: 0, end: 5 },
    ]);
  });

  it("maps a replace to delete-then-insert (single-kinded records)", () => {
    expect(spanBodies([{ from: 2, to: 5, insert: "xyz" }])).toEqual([
      { start: 2, end: 5 },
      { content: "xyz", start: 2, end: 2 },
    ]);
  });

  it("drops no-op spans and keeps span order", () => {
    expect(
      spanBodies([
        { from: 1, to: 1, insert: "a" },
        { from: 2, to: 2, insert: "" }, // no-op span (empty insert, zero width)
        { from: 7, to: 9, insert: "" },
        { from: 12, to: 12, insert: "bc" },
      ])
    ).toEqual([
      { content: "a", start: 1, end: 1 },
      { start: 7, end: 9 },
      { content: "bc", start: 12, end: 12 },
    ]);
  });

  it("empty edits produce no bodies", () => {
    expect(spanBodies([])).toEqual([]);
    expect(spanBodies([{ from: 3, to: 3, insert: "" }])).toEqual([]);
  });
});
