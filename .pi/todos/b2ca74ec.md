{
  "id": "b2ca74ec",
  "title": "e2e coverage — editor file tree: folders + files-in-folder CRUD (create/rename/move/delete/upload-to-folder) at /editor",
  "tags": [
    "e2e",
    "coverage",
    "testing",
    "file-tree",
    "editor",
    "folders",
    "files",
    "tree-crud"
  ],
  "status": "open",
  "created_at": 1791684711414,
  "last_change": 1791684711414,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Scope
E2E Playwright spec for **folder + file management in the editor file tree (`/editor`)** — the tree panel CRUD that users do daily. Mirror the style/gate of the other coverage items (TODO-479d936a family): create project, drive the tree UI, assert state, clean up.

## Features to cover (editor file-tree surface)
- **Create folder** in the file tree (New folder at root and at depth ≥2).
- **Create new file** — in root, and **inside a folder** (both via the tree's New File flow with folder preselected).
- **Move/rename** a file and a folder (rename via the tree affordance; move via drag or the move action if present — check the current tree implementation for which affordance the fork actually ships).
- **Delete** a file and a folder (expect confirmation behavior matches current UI), verify both disappear from tree + Mongo docs.
- **Upload** an external file into a **subfolder** (e.g. one of `docs/ub-logo/logo*.svg`) — verify it lands in the chosen folder, not root.
- **Tree state assertions**: nested structure visible (folders collapsed/expanded), order, file icons/type indicators, the tree reflects API state after each op (no forced reload needed).
- **Negative/edge**: rename to an invalid/empty name is rejected or handled; deleting a folder that contains files behaves per current product (recursive vs protected — assert actual behavior and pin it).

## Gate
- Spec `specs/editor/file-tree.test.e2e.ts` (or similar) GREEN on the e2e stack; project + uploaded files cleaned up after run; no stray Mongo docs.
- Assertions against both UI state and the project docs API (ground truth), so the spec catches silent tree-state drift.
- Keep within the rate budget (single spec, sequential, shared fixtures e2e-admin/e2e-user).

## Open investigation items (before writing)
- Current file-tree implementation + exact affordance names/selectors in this fork (New File / New Folder buttons, rename, delete confirmation) — mirror the helper patterns from the existing `specs/editor/` chrome specs.
- Whether upload-to-folder uses the same `input[type=file]` path regardless of selection.
- Folder delete semantics (recursive?) — pin whatever the product actually does; if it's a gap, raise it as a product note rather than forcing a test around it.

## Evidence / pointers
- Editor file-tree component (features/ide-react file-tree / filebrowser module — grep for `NewFolder|newFolder|file-tree` in frontend/js).
- Reuse: project-creation + cleanup helpers from smoke/typst-t2 specs; existing `specs/editor/` tree/chrome specs for selectors.
- Related: TODO-479d936a (complex-edit suite also does file creation/upload — keep scopes distinct: this item is the tree CRUD contract itself).

