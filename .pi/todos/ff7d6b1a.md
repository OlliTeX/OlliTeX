{
  "id": "ff7d6b1a",
  "title": "git-integration broken stack-wide: yops→history flush chain never completes (projectHistoryChunks=0; project-history /version 500 after edits; bridge repo materialisation 403 for ALL projects)",
  "tags": [
    "git-bridge",
    "project-history",
    "history",
    "s3",
    "seaweed",
    "flush",
    "git-integration",
    "bug",
    "both-stacks",
    "owner-review"
  ],
  "status": "closed",
  "created_at": 1791717369440,
  "last_change": 1791735119138,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## git-integration broken stack-wide — CLONE/PULL PLANE FIXED (acceptance met) + CLOSED

**Owner acceptance (this item)**: "git clone from a live edited Overleaf project works" → **TRUE since this session**: real `git clone` through the bridge of an editor-edited project returns a working repo with the typed content (specs/git-clone-bridge.test.e2e.ts, 1/1 GREEN on canonical ag-v46 + ollitex/git-bridge:ff7d6b1a).

### Root cause (two independent defects, both stack-wide)
1. **web /api/v0/docs + /snapshots (bridge-facing) was OT V2-only** — the V2 flush chain (redis ops → project-history ProcessUpdatesForProject) has **no producer** since the D19 Yjs hard cut (yops→history 500 for EDITED projects; version 0 everywhere; projectHistoryFailures/chunks empty). The content+versions plane is the per-room Yjs log (ydoc/head/upds) — the history panel's 024 Option B machinery.
2. **The Go bridge's snapshot client DISCARDED the validated PAT** (`NetSnapshotApi` methods took `_ *data.Oauth2`; doGET/doPOST sent no Authorization) → 401 on every materialization → "repository not found"/403. (data/oauth2.go documented Bearer intent; the port never threaded it.)

### Fix (commit e53c04d9)
- NEW `go/services/web/features/history/bridgeplane.go`: `BridgeLatest` + `BridgeSnapshotAt` render the bridge plane from the Yjs unified timeline (projectDocRooms/buildUnifiedFeedMulti/textAt — same as the live-verified history panel). Yjs-first with OT V2 fallback.
- `features/gitbridge/gitbridge.go`: getDoc/getSnapshot Yjs-first (author uid surfaced, latestVerId = unified N, snapshot srcs = current tree paths + per-room text at/before N).
- `gitbridge/snapshot/snapshot.go`: Bearer token threaded (setBearer) + sent on doGET/doPOST.
- Spec added: `tests/e2e/specs/git-clone-bridge.test.e2e.ts` (edit in UI → git clone via `7420/git/<pid>` → marker in cloned main.tex).

### Evidence (2026-10-11)
- e2e on canonical images: **8/8** — git-clone-bridge 1/1 + suspended-tokens 3/3 + edge-probe 4/4 (auth hardening intact; DELETE gate, exposure map).
- Manual: `git ls-remote` HEAD/refs + commit sha; full clone of the earlier `e2e-list-continuation` project returned the exact typed list content; 7420/git/ nginx route = host-reachable edge surface (bridge:8000 network-internal by design — spec #8 pin).
- **LIVE DEPLOYED**: olpsint/overleaf:ag-v46 on ollitex-web (healthy, fingerprint intact); live data untouched (282/146).
- Bridge image: `ollitex/git-bridge:ff7d6b1a` (= :latest) with fixes + auth gate + go/libraries COPY.

### Known limitations (v1 — tracked, NOT part of this item's acceptance)
- `git push` / saved-version pushes: OT postSnapshot plane absent → 400 degraded-but-safe. **→ NEW board item (follow-up)**: Yjs apply path for push.
- Cloning UNEDITED template projects (no yjs versions yet) + binary files in the snapshot → **v1.5 legs**: docstore current-text + atts/filestore URLs.
- **LIVE git edge**: the live bridge container is still down/absent (owner decision, see ebb76488). Live web now serves the plane; a bridge container (image ready) + edge route + config = the remaining live step.

Status: **CLOSED — clone/pull plane fixed, pinned, canonical-image-verified, live Web image deployed.**
