# RETIRED: services/web (Node web backend) — P7 step 4 (owner decision 2026-09-25: RETIRE; executed 2026-10-03 under owner deck instruction "1 → autonomously")

Moved `services/web/` → `junk/services-web/` (git mv, full history preserved).

## Why
The Go web (`go/services/web`, binary `go-services/web`) permanently replaced the
Node app on 2026-09-25 (P7 step 2, "permanent flip to bin/web + image + e2e").
The Node app is no longer exec'd by any runit unit in the image
(images/main-amd64/runit/web*-overleaf/* run the Go binary), is not imported by
the frontend, and is not part of the image build beyond comments. This move
retires its canonical home while keeping every file on disk for oracle
reference (Go parity docs cite these .mjs paths as the behavioral oracle).

## What was retargeted (functional refs, host-side only)
- root `package.json` workspaces: `services/web` + 2 script sub-workspaces removed
- `frontend/package.json` scripts: `../services/web/...` → `../junk/services-web/...`
- `.dockerignore` + `server-ce/.dockerignore`: ignore paths re-targeted
- `develop/docker-compose{,.dev}.yml`: dockerfile + host volume paths re-targeted
  (container-side `/overleaf/services/web/...` paths unchanged)
- `tools/restore-site-settings.mjs`: bib-editor cdp.mjs path re-targeted
- Inert (left as-is): Go doc comments citing oracle paths, frontend TS comments,
  historical raw captures (tools/capture-*-raw), plan/report .md docs.

## Oracle status
Go parity oracles for web behavior continue to point at these files
(now under junk/services-web/) — see the `go/services/web/...` doc headers.
Do NOT delete this tree; it is the behavioral reference for the Go port.

## Rollback
`git mv junk/services-web services/web` + revert the six path retargets
(each is a 1-file, few-line change; all are in this ticket's commit).
