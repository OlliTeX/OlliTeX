{
  "id": "48fcf6dc",
  "title": "009: filetree new SVG file doesn't appear — DONE (commit 0ba9140007; room-event parity: create/upload/delete + orphaned rename/move emit rerouted to realtime bus)",
  "tags": [
    "live-audit",
    "psintegr",
    "editor",
    "filetree",
    "file-create"
  ],
  "status": "done",
  "created_at": 1790773187711,
  "last_change": 1790791401211,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

Owner live audit. Editor filetree: newly created .svg file does not appear in the file tree (add succeeds? tree stale). Likely file metadata/refresh flow after create (frontend tree refresh + Go collab file op broadcast). Related to Issue 017b: "Add file -> Diagram (SVG)/TikZ diagram" OVERWROTE main.tex with the SVG content (wrong target path in the create-file flow — owner-reported: main.tex content replaced by <svg width=842...> Layer 1 stub). Investigate add-file path handling for diagram types.
