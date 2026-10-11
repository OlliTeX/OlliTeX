{
  "id": "dbfe4d16",
  "title": "[II] Port ext-ce-llm (adds LLM) → 6.3.0  [commits 22a96d21..head]",
  "tags": [
    "port",
    "ext-ce-llm",
    "order-2",
    "depends-I"
  ],
  "status": "done",
  "created_at": 1788360827618,
  "last_change": 1788382995241,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Scope
Source: /data_1/image_mining/the_diff/ext-ce-llm, range 22a96d21a7 (LLM: Initial files) .. a478271fc6 (HEAD), 50 commits, ~129 files (most under services/web/modules/llm; ignore .pi/todos noise).

## Content
- llm module: app/src (13), app/test (8), frontend components (14), hooks (5), extensions (4)
- Recent commits: grammar checking (M5) infra+docs, blocked-rules live sync + case-insensitive filter, audit round 2 (harden grammar save inputs; fix stale save/announce state)

## How
Same protocol as [0]/[I]: diff the range, apply onto the_diff/overleaf, wire module into moduleImportSequence + overleafModuleImports keys the llm module declares (grep its frontend imports + index.mjs hooks), settings/env keys into settings.defaults.js, rebuild docker image GREEN before starting III.

## Deps
Needs [I] complete (nextcloud branch is built on top of llm — its diff will contain llm deltas that [II] must already cover, otherwise [III] carries them).
