# BUG HUNT REPORT — ROUND 2 — 2026-10-14

**Mandate (owner):** intensive bug hunt, security + structural. Round 2 targets the planes round 1 only skimmed:
git-bridge/github-sync exec+URL surface, command runners, store persistors, archive sinks, token entropy,
concurrency (race detector), frontend XSS sinks, user enumeration.

## CONFIRMED FINDING (round 2)

### BH-4 — github-sync server-URL surface = unguarded SSRF/oracle dial — MED-HIGH (owner adjudication)
**Surface:** `ghsync` (web feature) → `githubinterface` (`git` via argv — no shell injection; `shellQuote`
askpass escaping verified correct). User-controllable dials with **scheme-only** vetting
(`gsAssertHttpServerUrl` — http/https only, no range/name/DNS checks):
`POST /user/git-servers` · `POST /user/git-pat/link` (**the link check itself dials**) ·
`POST /user/git-servers/test` (oracle by design) · `POST /project/new/github-sync` (`git clone`) ·
`Commits/BranchHead/CanPush`.
**Live proof (ag-67 world, self-cleaned):**
- `linkPAT` @ internal forgejo (172.20.0.18) → **the internal service's 401 was mapped into the
  response** ("Token invalid or expired. Re-link with a fresh PAT.") — live content/status oracle over
  internal git infrastructure.
- `linkPAT` @ `http://169.254.169.254` (cloud metadata) → **the request hung 30s+** (server dialed the
  metadata IP; on a real cloud VM that endpoint returns 200 with the payload — classic SSRF read).
- Cross-PROJECT clone via git-bridge is (accidentally) stopped by git-bridge's **per-repo token gate**
  (live 401 without the repo's own token) — defense by auth, **not** by URL guard; the oracle remains.
**Design nuance (why owner-gated rather than silently fixed):** the feature *legitimately* dials the
user's own self-hosted git instance (GHE/Forgejo — frequently on private LANs), so a full SSRF block
(the LLM/WebDAV treatment) would **break the feature**. Surgical recommendation: block ONLY the ranges
that are never a legitimate user git host — **127/8, 169.254/16, 0/8, ::** (loopback/link-local/metadata)
— while allowing RFC1918 + public + names; pin the rest. Node-parity check: the original had the same
scheme-only check → faithful legacy risk.
**Pin (permanent spec):** `tests/e2e/specs/ghsync-url-surface.test.e2e.ts` (1 test, GREEN, self-cleaning)
— header carries the ⚑ owner flag. Board: **TODO-<pending>** below.

## SECONDARY NOTES (recorded, no defect claimed / low)
- **R2-1 (code-verified):** password-reset `POST /user/password/reset` returns **three distinguishable
  outcomes** (primary 200 / known-secondary 404 "registered as a secondary" / unknown 404 "not
  registered") → **email enumeration**. Node-faithful legacy behavior (original Overleaf identical).
  Owner's call on parity vs. uniformization.
- **R2-2 (minor state bug, hit live):** `DELETE /user/git-servers/:id` pattern `[^/]+` **cannot address
  server URLs containing '/'** (e.g. `https://ghe.corp/git`) → such entries are un-unlinkable via the API
  (state cleanup required mongo). Likely Node-faithful; low impact (user's own state).
- **R2-3 (mitigated/recorded):** `faq-search` renders Algolia wiki hits via **unsanitized `innerHTML`**
  (sibling `contact-form/search.ts` uses DOMPurify ✓). Instance-owned wiki content; **CSP nonce blocks
  inline script/handlers; dormant here** (no Algolia config in this build). Low.
- **R2-4 (design, excluded from SSRF class):** wakatime heartbeats dial the *user's own* wakapi instance
  (`cr.APIURL`, user key, user data → user's server) — same-principal design; guarding it would break the
  feature. Not a finding.
- **R2-5 (cleared):** githubinterface askpass = `shellQuote` (`'\''`-escaped) — **no shell injection**;
  all git calls are argv (`exec.Command("git", args…)`); `file://` repo URLs are **defeated by the URL
  join** (`serverUrl/` + non-http name → mangled) — only http(s) internal dialing works (the BH-4
  oracle case).
- **R2-6 (cleared):** typst/latex compile command = `sh -c` with **quoted positional args** (`"$1"`) —
  user filenames carry no shell metachar expansion (verified against the builder). **No filename
  command injection.**
- **R2-7 (cleared):** historyv1 zip usage = **write-only** (version export) — no archive-extraction sink
  outside the already-guarded web-plane extractors.
- **R2-8 (cleared):** token/ID entropy — `crypto/rand` at every security-relevant site audited
  (accesstoken, core tokens, configstore, persistors, cachehandler crypted tokens); `math/rand`
  sightings = non-security use (cache jitter, VM lifespan spread — documented Node parity).
- **R2-9 (cleared):** `go test -race` **clean** on core / ghsync / gitbridge (the Q4 race-prone plane).
- **R2-10 (recorded):** persistor flat-FS key flattening (`a/b` ↔ `a_b`) remains a documented same-project
  collision quirk **in FSPersistor mode only** — this world runs the S3 persistor (keys verbatim, no
  collision). No cross-project keys (project prefix always present).
- **R2-11 (good finding, defense-in-depth):** git-bridge HTTP interface requires the **per-repo token**
  (live 401 without it) — the accidental-but-real defense behind BH-4's clone leg.

## NOT TARGETED (scope note)
Compiler-VM internals (clsitex scheduling internals beyond the audited exec sites), the JS IDE's
60k-line bundle beyond the XSS-sink sweep, the CE community template set. Round 1 cleared the web-plane
authz/CSRF/session/id/zip/crash surfaces; round 2 drilled the service/exec/URL/entropy/concurrency plane.

**World state:** ag-67 (unchanged — no product changes this round; BH-4 awaits the owner's call).
**Battery status:** 674/1-ext-dropbox/1-flaky/4-skipped (ag-67) + new `ghsync-url-surface` spec GREEN.
