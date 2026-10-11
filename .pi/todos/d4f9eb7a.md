{
  "id": "d4f9eb7a",
  "title": "e2e: INTENSIVE comments feature suite (2-user live exchange, UI edit/delete cycles, multi-file anchors — gap beyond the R-legs)",
  "tags": [
    "e2e",
    "comments",
    "review-panel",
    "coverage",
    "two-user"
  ],
  "status": "open",
  "created_at": 1791735213471,
  "last_change": 1791735213471,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Intensive e2e COMMENTS feature suite — CREATE

Owner request 2026-10-11 (intensive connected-feature coverage). This item = the COMMENTS leg.

### Current coverage (gap assessment, 2026-10-11)
GOOD (server/wire, single-user):
- `review-panel.test.e2e.ts` (525 L): R2 CREATE (panel UI → GET /project/:pid/threads record shape + ranges), R3 REPLY, R4 RESOLVE/REOPEN (panel options → record.resolved/resolved_by_* + relay), R9 anchor-follow-after-edit.
- `review-edited-at.test.e2e.ts`: edited_at wire (review.go messageEdit relay + GET /threads record; commit 4bc6ac34 + faf21e3e).
- `review-overview-completeness.test.e2e.ts`: comment on never-opened .bibleg present in wire with doc+ranges; /ranges 200.
- `review-fileview-500.test.e2e.ts`: fresh-client open without 5xx; main.tex stays usable.
- `zotero.test.e2e.ts`: linked-file data survives clone (comment-adjacent data plane).

GAPS (the "intensive, all connected" holes):
1. **2-user comments**: A starts thread in a shared project → B's panel shows it LIVE (realtime relay both directions); B replies → A sees it; NO spec covers two concurrent commenters today (collab-two-user covers text, not comments).
2. **UI edit + delete cycles**: R-legs cover create/reply/resolve; UI-driven EDIT (existing text is pinned only via wire in the edited-at spec) and DELETE (only wire) — need the panel options → edit → save → panel re-render round-trip, and delete → thread gone from record + UI list.
3. **Multi-file comments**: thread on .tex (pinned) + thread on .bib/other reader file (overview-completeness pins the .bib WIRe; the .bib reader surface UI leg is untested) — comment on a never-opened reader file, open it, ranges highlight.
4. **Range/anchor robustness battery**: R9 pins ONE anchor-follow case; add: comment on a list item, delete a preceding line, reopen → range stays on the right node; comment near end-of-buffer; comment spanning two lines edited to one.
5. **Resolved-state round-trip + reopen by second user**, and resolved-thread visibility rules (hidden/section-collapsed per product behavior — pin actual behavior, discover first).

### Standing facts
- Panel mount gate: `features?.trackChangesVisible` (codemirror-editor.tsx:132) — wire pins are panel-mount independent (wave decision).
- Comment wire: POST /project/:pid/message/new-comment, /message/edit-message (4th arg editedAt), DELETE path in go/services/web/features/review/review.go; panel REST = GET /project/:pid/threads Record<threadId,Thread>.
- File tree: SINGLE click = select, DOUBLE click = open (pinned everywhere).
- Rate-limit ~20 logins/min; 2-user spec: stagger + serial.
- `x-csrf-token` from meta `ol-csrfToken`; harness api() status() is a METHOD.
- E2E image ag-v46 current; base http://127.0.0.1:7420.

### Exit criteria
- Green suite (split ok: comments-two-user.test.e2e.ts + comments-ui-full.test.e2e.ts + comments-multifile.test.e2e.ts) in tests/e2e/specs/, committed; existing review-* specs re-run green; board closed with evidence.
