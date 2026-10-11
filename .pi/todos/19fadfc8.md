{
  "id": "19fadfc8",
  "title": "AD: Image/SVG editor modals (full size, two-stage) — fix + e2e",
  "tags": [
    "ad",
    "image-editor",
    "svg-editor",
    "modal",
    "e2e"
  ],
  "status": "done",
  "created_at": 1791383767870,
  "last_change": 1791506969718,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

DONE 2026-10-07: covered by the AG matrix agg3-ad **6/6** (image stage-1 + full stage-2 modal with toolbar+canvas, resize w/h live, Escape-close clean) + agg5-modes **10/10** (SVG editor svgedit iframe loads, zero 404s). Dirty-guard AD-4 fixed by undo-baseline (canvas changes do NOT mark the file dirty); seedBaseline scope bug fixed (was ReferenceError on the success path only — now covered by the "no page errors" assertion).
