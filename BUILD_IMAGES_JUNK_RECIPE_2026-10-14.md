# build-images/ junk — DELETION-READY RECEIPT (2026-10-14, comment-first)

Status: **PENDING OWNER DELETION GATE** (develop-stack deletions stay owner-gated —
the current directive green-lit runtime zombie/bucket cleanup, not repo files).
Everything below is prepared and verified at HEAD `77ce9f89`; execute section 4
when the gate is lifted.

## 1. Scope (8 candidates, ~332K, all present, zero live consumers)

| # | Path | Size | Disposition evidence (re-scan 2026-10-14) |
|---|------|------|-------------------------------------------|
| 1 | `build-images/test/` | 280K | Legacy Cypress+Playwright image suite (own compose + specs + Dockerfile). 0 refs in Makefile/CI/develop/images. Superseded by `tests/e2e` (124+ specs, the CI e2e job). |
| 2 | `build-images/eslint.config.mjs` | 4K | eslint config for the Cypress plugin in #1 — orphaned with it. 0 refs. |
| 3 | `build-images/bin/rename-env-vars-5-0.sh` | 4K | 5.0-era env migration one-off. Only appearance repo-wide = user-facing error *hint strings* in `images/main-amd64/init_scripts/000_check_for_old_env_vars_5.sh` (docs, not a call). |
| 4 | `build-images/bin/import_pr_patch.sh` | 4K | One-off PR patch importer. 0 consumers. |
| 5 | `build-images/lib/.dropbox-secrets` | 30B | Comment-placeholder only (not a live secret); **tracked and NOT gitignored** — rename-invite for leak-sweep false alarms. 0 consumers. |
| 6 | `build-images/e2e/notifications-smoke.sh` | 8K | Standalone smoke; functionally covered by `notifications` + `notifications-full` arcs. Self-reference only. |
| 7 | `build-images/cloudbuild.public.yaml` | 4K | gCloud builder config; no Makefile/CI references; only relevant if publishing to GCR (owner process call — included but flagged separately). |
| 8 | `build-images/config/custom-environment-variables.json` | 4K | Legacy Node env mapping; only mention = doc comment in `go/services/historyv1/historyv1.go:53` (env names kept 1:1 — the comment stays; it documents the mapping, not the file). |

## 2. Keep (live — confirmed by the same scans)
`grafana/`, `prometheus/`, `alertmanager/` (monitoring plane; admin Grafana bridge
arc), `edge/` (haproxy edge bake), `bin/shared/`, `scripts/rotate-sync-credential-keys.mjs`
(credential rotation ops), `WAKAPI.md`, `config/` remainder (legacy env mappings).

## 3. Pre-flight (already verified at HEAD 77ce9f89)
- All 8 paths present (`du -sh` as in table).
- `grep -rn "build-images/test"` over Makefile/.github/develop/images/go/scripts → 0.
- `rename-env-vars-5-0` → hint strings only (2 lines, error text).
- `custom-environment-variables` → 1 doc comment.
- Others → 0 refs. No md5-identical twins in `images/main-amd64/` (audit scan).
- Working tree clean (commits `45ab08a7`, `105a1c6a`, `f66fa8dc`, `77ce9f89` in
  sequence; no staged junk).

## 4. Execute (single command once the gate is lifted)
```
git rm build-images/eslint.config.mjs \
  build-images/bin/rename-env-vars-5-0.sh \
  build-images/bin/import_pr_patch.sh \
  build-images/lib/.dropbox-secrets \
  build-images/e2e/notifications-smoke.sh \
  build-images/cloudbuild.public.yaml \
  build-images/config/custom-environment-variables.json
git rm -r build-images/test
git commit -m "build-images junk retirement (owner gate 2026-10-14): legacy image-test suite + orphaned one-offs (receipt BUILD_IMAGES_JUNK_RECIPE_2026-10-14.md)"
```
Post-check:
```
git status --short | wc -l            # expect 0
grep -rn "build-images/test\|import_pr_patch\|.dropbox-secrets\|cloudbuild.public" Makefile .github/ images/ develop/ go/ 2>/dev/null | wc -l   # expect 0
CI=1 npx playwright test specs/sso-ldap-live.test.e2e.ts --workers=1 --retries=0   # spot: world still green (3.5m)
```

## 5. Residual questions for the owner
- #7 `cloudbuild.public.yaml`: delete with the rest, or keep for a GCR publish plan?
- #8: comment stays in `historyv1.go` (documents the mapping) — approve deleting
  the JSON and keeping the comment?
- Anything in `build-images/test/` worth salvaging (e.g. the compose network
  topology) before removal? — `tests/e2e/stack-up.sh` supersedes it.
