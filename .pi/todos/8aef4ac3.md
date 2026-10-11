{
  "id": "8aef4ac3",
  "title": "Batch 2: collab + pandoc + auth + history e2e",
  "tags": [
    "e2e",
    "batch2"
  ],
  "status": "done",
  "created_at": 1788658533978,
  "last_change": 1788782387812,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

Tier-A core (local): (6) multi-user collab: invite/accept, OT prop across two contexts, leave, ownership transfer (OwnershipTransferHandler is ported but untested), audit-log docs; (7) pandoc import docx/md -> .tex + media, export Word/MD/HTML with magic-byte checks; (8) registration + password change/reset (reset token pulled from mongo) + CE password-policy rejections; (9) history restore round-trip (3 edits -> docHistory, restore first, labels); (10) file ops + upload error contracts (422 invalid_filename/duplicate_file_name).
