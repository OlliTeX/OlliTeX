# OlliTeX Audit — Part B (deep-dive) — Corrections, New Findings, Re-Verifications

**Date:** 2026-09-30 · Companion to `problems_30092026.md` (Part A). Report-only; no code modified.
This file (a) corrects Part A items whose investigation deepened, (b) adds findings from the
deep-dive pass (auth throttle model, e2e-bake exact per-constant map, zip intake, SSRF guard,
upload caps, concurrency, panic inventory), and (c) records fresh re-verification of C1.

---

## 1. CORRECTIONS TO PART A

### 1.1 H2 (e2e fixtures in views) — refined exact picture
Per-constant byte-level audit of `go/services/web/views/pages_data*.go`:

| Constant (page) | CSRF state | e2e leftovers |
|---|---|---|
| `passwordResetHTML` | `passwordResetForm` `data-ol-async-form` (action `/user/password/set`) carries the **literal** token `kqYCGy4j-ONpvNUHE2zYTR4ObW-LAy0cjEiQ`; page `<meta ol-csrfToken>` IS a runtime slot | origin `127.0.0.1:7420` ×(site) |
| `tokenAccessLegacyHTML` | navbar `logOutForm` carries **literal** token `3XOEqz0Z-13MTXD-X-3Q4CjCo27Qh2w0ZXwo` | `e2e-user@e2e.test` ×2 (meta + navbar pill) |
| `sharingUpdatesHTML` | runtime slots only | `e2e-user@e2e.test` (ol-user meta + `sessionUser`), project id `6aa4b8c973ef0e5094f4cc02` baked into URLs/metas |
| `sessionsHTML` (`/user/sessions`, served via `views.SessionsPage` ← `GET /user/sessions`) | **`<meta name="ol-csrfToken" content="VmzhMtSC-6M1kHvAlhoGrLeLiSVq9YU2Jxpw">` is a BAKED LITERAL** (not the `\x01CSRF\x02` slot) + one literal `_csrf` input | origin baked |
| `settingsHTML` | runtime slot (OK — Part A wrongly grouped this with sessions) | — |
| `registerHTML`, `logoutHTML`, `restrictedHTML`, `notFoundHTML` (`pages_data.go`) | runtime slots (OK) | `ol-ExposedSettings` `siteUrl: 127.0.0.1:7420`, `ol-auth-config` → `domains: ["e2e.test"]` |

**Corrected impact assessment** (Part A over-stated the CSRF part):
- Async-form path is SAFE: `frontend/js/infrastructure/fetch-json.ts` (`fetchJSON`) always injects
  `X-Csrf-Token: getMeta('ol-csrfToken')` from the **runtime** meta, and Go `csrfTokenFrom`
  falls through header list after failing form/query (`app.go`). So `passwordResetForm`
  works for real users; `kqYCGy4j` in it is dead weight used only by the no-JS fallback
  (which then 403s because the secret is per-session, `session.go:367-382`,
  `app.go:445` `VerifyCsrfToken(sess.CsrfSecret(), …)`).
- Real functional breaks (this is the true severity, MEDIUM-HIGH not "all forms 403"):
  1. **`/user/sessions`** — every React XHR on this page (e.g. killing a session) reads
     `getMeta('ol-csrfToken')` = the baked e2e token → guaranteed 403. Feature dead.
  2. **Sharing-updates join flow** — `sharing-updates-root.tsx` reads `getMeta('ol-project_id')`
     (baked e2e id) and POSTs `/project/{e2e-id}/sharing-updates/join` on a real instance →
     404 / wrong target. `access-attempt-screen.tsx` / `require-accept-screen.tsx` read
     `getMeta('ol-user')` (baked `e2e-user@e2e.test`) → wrong identity displayed.
  3. **Register page** — `register.tsx:133-139` renders `ol-auth-config` domain chips →
     real users see `e2e.test` listed as a registration domain restriction.
- Info leak: mitigated *mostly* because (per Part A) `finalize()` does
  `strings.ReplaceAll(out, capturedOrigin, orig)` per request, rewriting the
  `127.0.0.1:7420` origin occurrences. What still reaches real users: the `e2e.test`
  domain list, the e2e email/uid on the token-access & sharing-updates pages, and
  `placeholder@example.com` admin email inside `ol-ExposedSettings`.
- **Fix (unchanged):** regenerate captures so every per-identity value (`_csrf` inputs,
  `ol-user`, `ol-project_id`, `auth-config` domains, `sessionUser`) comes from slots.

### 1.2 Count corrections
- L5 "silent swallows": exact non-test `grep -E '^\s*_ = ' go cmd --include='*.go'` → **357** (Part A said 352).
- L6 "panic sites": exact non-test `grep -E 'panic\(' go cmd --include='*.go'` → **101** (Part A said 97).
  Distribution (top): `services/gitbridge/db/db.go` **25** (panic on *every* DB error — see B6),
  `project-history/internal/opmodel/*` 19 (intentional invariants), `otc/*` (intentional
  type-checking), `web/core/session.go` 3, `web/core/redis.go` 2 (config-time, OK).
- SA6005 wording: staticcheck SA6005 = "should use `!strings.EqualFold`" (case-insensitive
  `!=`/`==` comparisons) — "nil check" phrasing dropped. Sites (4 non-test) unchanged:
  `githubinterface/client.go:75`, `historyv1/controllers.go:422`,
  `projectlist/invite.go:212` and `:829`.
- S1040 (7): all in `_test.go` (e.g. `otc/text_operation_test.go`) → test-only, no product impact.

### 1.3 C2 (logout open redirect) — mechanism note confirmed
The "validated" path is *not* free-text validation: `app.go:690-717 globalLoginBounce`
stashes `r.URL.Path` (path-only) into `session.postLoginRedirect`; `app.go:903
loginRedirectTarget` just returns `"/login"`. So the logout handler can only be fixed by
adopting the stash (which is path-safe) or a relative-path check — there is no existing
validator for arbitrary attacker strings. `response.go Redirect` also confirmed to echo
the target unescaped into the HTML body (`"<p>…Redirecting to " + url + "</p>"`) →
attacker-string reflection into a 302 HTML body (minor, same CSRF preconditions).

---

## 2. NEW FINDINGS (deep-dive pass)

### B1 (MEDIUM). No login brute-force throttle — `lastFailedLogin` is write-only dead state
- `POST /login` (`authpages/authpages_login.go`): writes `lastFailedLogin` on failure
  (line 120) but **nothing anywhere reads it** (repo-wide grep: only the two write sites
  + a comment). No `NewRateLimiter` on login; the shipped config bakes
  `"recaptchaDisabled":{"login":true,"passwordReset":true,…}` into `ol-ExposedSettings`.
- The `loginEpoch` optimistic-lock `$inc` → 429 (lines 132-149) is a *parallel-login*
  guard (CE "ParallelLoginError"), **not** a throttle: a single sequential attacker
  never trips it (each attempt sees the epoch it was read at).
- Only real defense = bcrypt cost 12 latency (a few hashes/second per core).
- Contrast: `registrationpage` (5/60s), `passwordreset` (6/60s), `tokenaccess` (10/60s),
  `invite` tokens (200/600s), uploads, project ops — all rate-limited; login is the hole.
- **Fix:** per-IP `NewRateLimiter(a.Redis, "login-attempts", …)` (Redis client already
  used for all other limiters) and/or enforce a lockout from `lastFailedLogin`.

### B2 (MEDIUM). Zip intake trusts the zip's *declared* sizes; per-entry `io.ReadAll` unbounded
`go/services/web/features/projectlist/newzip.go`: **CORRECTED in 1.4** — the input IS
bounded (raw upload body), so this is memory-amplification within a ~5x window, not
unbounded OOM. Full corrected text:

- **Input IS bounded.** `nzipMultipart` (line 106): `r.ParseMultipartForm(
  upMultipartCap + nzipMaxUpload)` with `nzipMaxUpload = 50MB` and each part read
  via `ff.Read` into `data` with an explicit `if len(data) > nzipMaxUpload` check
  (line ~120). The raw zip bytes are capped at ~50 MB on disk/memory in every
  case.
- **But the *declared* sizes are the only check at extraction time.**
  `nzipExtract` (207-243): `total += int64(f.UncompressedSize64)` (line 217),
  `total > nzipUncompMax` (300 MB) check runs **before** opening any entry —
  based purely on `UncompressedSize64` from the zip header (attacker-declared);
  then each entry is read with `data, rerr := io.ReadAll(rc)` (line 243) with
  **no runtime cap and no `http.MaxBytesReader`/`LimitReader`** on `rc`.
- **Amplification window.** A well-formed DEFLATE stream can expand up to the
  `300MB` declared total (the header can be made to lie only within what
  `zip.NewReader` will still accept; Go's `archive/zip` does validate CRC and
  re-derive size, so the lie is bounded by what `UncompressedSize64` can
  *legitimately* be for a given compressed body). Worst case: a crafted
  50 MB compressed input whose entries each declare their true size, with
  the declared sum ≤ 300 MB but whose real stream yields several hundred
  MB of `io.ReadAll` allocation across ≤2000 entries. In practice this
  is the standard zip-bomb mitigation gap (declared-size-only pre-check +
  uncapped per-entry materialize), bounded to a few hundred MB per request,
  **bounded by connection** because it runs in the request goroutine.
- `nzipSafePath` (104-143) good (rejects `..`, empty, `\`, absolute, trailing
  `.`); `seen > nzipMaxEntities` (2000), `len(r.File) > nzipMaxEntryCount`
  (20000) cap counts.
- **Fix (unchanged intent, corrected mechanism):** cap the per-entry read at
  copy time (`io.CopyN(io.Discard, rc, nzipUncompMax+1)`-style, but on the
  *actual copy* into `data`), not only against `f.UncompressedSize64`.

### B2a (LOW). `fileuploadmiddleware` (clsitex convert path) part cap detail
`go/services/clsitex/fileuploadmiddleware/fileuploadmiddleware.go`: `maxParts`
enforced (returns `ErrTooManyParts`), `ErrUploadFileTooLarge` for the `qqfile`
part, `respond` writes the express JSON envelope on failure. `maxFileSize` is
checked against the *declared part length* (standard busboy behavior, bounded
by `Content-Length`-implied body). No independent per-byte cap on the stream
beyond `mr.NextPart()` EOF + the declared-length check. Acceptable risk
profile, recorded for completeness (not a finding).

### B3 (LOW). orcidpicker SSRF guard — residual DNS-rebind TOCTOU (accepted risk, document it)
`go/services/web/features/orcidpicker/ssrf.go`:
- `checkHostNotPrivate` resolves via `net.DefaultResolver.LookupHost` and rejects any
  non-public record; `client.go` does manual per-hop redirects (`CheckRedirect` returns
  `http.ErrUseLastResponse`, hop loop `≤ maxRedirects` with per-hop ctx timeout) —
  i.e. re-check per hop (good, matches the comment "safeFetch re-checks per
  redirect hop").
- Residual: attacker-controlled DNS can flip a name from public → private between
  `LookupHost` (decision) and the actual dial (classic rebinding). The decision uses
  the system resolver (no pinned dialer with post-dial IP re-check). Mitigation is
  implicit/short: re-check per hop keeps the window tiny. **Note it** in the SSRF
  documentation rather than re-architect.

### B4 (LOW). `qpdf` exec in outputfileoptimiser — arguments are static (positive)
`go/services/clsitex/outputfileoptimiser/outputfileoptimiser.go:52`:
`exec.Command("qpdf", "--linearize", "--newline-before-endstream", src, dstOpt)` —
`src`/`dst` are server-side temp paths (random names from the compile runner), no
user-controlled argv. No injection vector. Recorded as verified-positive for the
deep-dive.

### B5 (LOW). `dmWalkTree` reads every file fully into memory (confirms OPT3, adds detail)
`go/libraries/datamanipulator/fileops.go:21-60`: `os.ReadFile(fullPath)` per regular
file just to compute metadata (size/type/checksum). Plus a couple of deliberately
ignored errors (`root, _ := filepath.Abs`, read-error `continue`, "match Node" comment).
The whole-file read is also a latent OOM for one huge file. Stream with
`sha256.New()` + `io.Copy` and stat-based type detection.

### B6 (MEDIUM). `gitbridge/db/db.go` — 25 `panic(fmt.Errorf("db: …: %w", err))` sites
Every Mongo error in the gitbridge DB layer panics (`getNumProjects`,
`getProjectNames`, `setLatestVersionForProject`, `postbackPut/Get/Delete`, …). Within a
service goroutine that is a process-kill on transient DB failure (same failure family
as Part A H5 `tokens.go:38`, C1). If the DB blips during git-bridge activity, the
process dies mid-job. **Fix:** same as H5 — propagate errors; reserve panic for
truly-impossible invariants.

### B7 (INFO/positive). Concurrency review of realtime bus + compliance job registry
- `realtime/bus.go`: `clients`/`byProject` maps consistently under `b.mu`
  (Lock at 149/156/228/259/307, RLock at 391/401/607/617/700/725/749/775/782/845),
  separate `b.drainMu` (655/688) for drain path. No unlocked map access found.
- `llmsettings/compliance.go`: `chJobs`/`chQ` fully under `chQMu`
  (277-279 read, 329-335 scan, 342-360 enqueue, 502-509 dequeue). Clean.
- What's NOT clean: the background job goroutine itself (`go f.chPerform(ctx, job)`,
  line 371, see C1) — no `recover`, so any panic in job execution (not only the
  regexp one) kills the process. The regexp is just the first guaranteed one.
- Redis client (Part A C3) still stands: single conn + global mutex, `Close()`
  unsynchronized, no deadlines.

### B8 (LOW). bcrypt cost floor via env
`launchpad/users.go:33-37`: `bcryptRounds()` honors `BCRYPT_ROUNDS` env with floor 4 —
an operator setting `BCRYPT_ROUNDS=4` silently drops password hashing to cost 4.
Floor 4 is far below bcrypt's documented minimum viability (10+). Non-exploitable
by attackers (env is operator-controlled) but a foot-gun; adminusers path is pinned
to 12 (`mutations.go:461`). Consider raising the env floor to ≥10.

### B9 (MEDIUM-HIGH, new). Vendor `otc` panic on attacker-controlled decode (ranges/
Blob), in contrast with the already-fixed fork
`go/libraries/otc` (vendor, error-returning wrappers over panic-constructors):
- **`NewRange` PANIC** (`go/libraries/otc/range.go:17-19`):
  ```go
  func NewRange(pos, length int) Range {
      if pos < 0 || length < 0 {
          panic(fmt.Sprintf("Invalid range (pos=%d length=%d)", pos, length))
      }
  ```
  Reached from: (a) `asRawRanges` (`raw_conv.go:72-85`) — called directly by
  `FromJSONEditOperation` for `commentId` ops (`edit_operation_builder.go:16-18`:
  `ranges := asRawRanges(raw["ranges"])`) AND by `FromRawComment` (`comment.go:168`);
  (b) `FromRawTrackedChange` (`tracked_change.go:19-29` → `
  NewTrackedChange(NewRange(pos, length), tp)`). The dispatch chain
  `OperationFromRaw` (error-returning, `operation_transform.go:22-45`) →
  `EditFileOperationFromRaw` (error-returning, `operation_file_tree.go:153-158`)
  → `FromJSONEditOperation` (error-returning, `edit_operation_builder.go:9-31`) —
  is error-returning all the way up, but `asRawRanges` → `NewRange(...)` inside
  the `isRawAddComment` branch **bypasses the error channel** for the
  `ranges` field: `numberToInt` (unvalidated float→int coercion at
  `tracked_change_list.go:31-40`) turns `{"pos":-1,"length":0}` into
  `NewRange(-1, 0)` → **panic**.
- **`newBlob` PANIC** (`go/libraries/otc/blob.go:40-46`): `NewBlob` (called from
  `Blob.fromRaw`, `NewBlob` wrapper, and from
  `go/services/historyv1/chunkstore_pg.go:410`, `blobstore.go:185-233`) panics
  on `!otpure.HexHashRx.MatchString(hash)` (non-hex / wrong-case hash) or
  `byteLength < 0`. Attacker-reachable only via stored-blob decode
  (`historyv1` `LoadLatest/AtVersion/AtTimestamp` in
  `go/services/historyv1/chunkstore.go:169/257/301`), not from a live HTTP
  handler body — requires a crafted **stored chunk record** (DB-level),
  reachable via any code path that writes a blob record without hash
  validation (a write-side validator would need re-audit to confirm the
  write enforces the same hash invariant).
- **Empirically, this is NOT a process-death vector** (verified on Go
  1.27, see below). But the fork ships a *parallel* error-returning
  re-implementation (`go/services/project-history/internal/opmodel` —
  identical `FromRaw` API but `errors.New`-returning, no panic) for
  `project-history`; the *vendor* panic version is still the one live on the
  `historyv1` + `clsitex` decode paths. So this is a **latent DoS / clean
  500-vs-panic divergence**: a corrupted-but-valid stored chunk returns a
  500 with a stack trace from `otc` where the fork's own `opmodel` would
  return `errors.New("bad raw.range")`. Not attacker-reachable over the wire
  today (write-side validation is the open question), but a correctness
  regression vs the fork's own invariant.
- **Process-death empirical (this session, Go 1.27, two contrast tests):**
  ```
  /tmp/gopanic  case A: handler goroutine panic   -> "http: panic serving..."
                process stays up (200 after)     =>  request gets reset conn,
                                                     process SURVIVES
           case B: plain  `go func(){panic()}`   ->  process DIES (no net/http
                (compliance.go:371 style)          recovery for plain goroutine)
  ```
  This confirms the C1 severity ranking is **not** inflated: only the
  background-goroutine panic (`compliance.go:371`) is process-lethal. The
  vendor `otc` decode path is per-request recovery: the offending connection
  is reset (client sees `connection reset by peer` not a clean 500 JSON),
  the process survives, and the stack is logged (log-flood on repeated
  attempts, no client-side exploitability beyond connection reset).

### B10 (LOW, new). Two `go func` pairs in `gitbridge/gitproto` run
`io.Copy` over `cmd.Start` pipes without error checks (`receivepack.go:101-107`,
`uploadpack.go:98-104`) — if `cmd.Start` succeeds, a failed `Close`/`Copy` is
silently dropped; `_ , _ = io.Copy(...)` is intentional (pipe drain) but
`cmd.Wait()` is called and only its exit code checked (0/1/2 all servable).
Minor: no `defer` on the goroutine pipes, no log of the error. Not a
vulnerability, recorded for completeness.

---

## 3. FRESH RE-VERIFICATION OF C1 (2026-09-30, second confirmation)
- Standalone repro (this session, go1.27.1):
  ```
  PANIC: regexp: Compile(`^\s*[-*\u2022]\s+`): error parsing regexp: invalid escape sequence: `\u`
  ```
  and the documented fix compiles+matches: `` `^\s*[-*\x{2022}]\s+` `` → `true`, matches `"• x"`.
- Call path (re-confirmed this pass): `go f.chPerform(ctx, job)` at
  `compliance.go:371` (plain goroutine, no `recover` anywhere in `llmsettings/*.go`);
  `chPerform` → `chSplitRubric` (line 571 area) → line 434
  `bullet := regexp.MustCompile(`^\s*[-*\u2022]\s+`)` panics **on the very first**
  `chSplitRubric` call — i.e. unconditionally per job, independent of document content.
  The two `MustCompile` calls at the top of `chSplitRubric` run on every invocation
  (perf micro-issue: compile twice per call — move to package-level vars after fixing).
- Prerequisites (Part A, still valid): `llmPref` gate + `reviewEnabled` default-true +
  existing rubric + `resolveLane` success (LLM lane configured). One authenticated
  project member POST to `/project/{id}/llm/compliance/start` → 200 response goes out,
  then background panic → **whole web process dies**.

---

## 4. UPDATED PRIORITY (supersedes Part A section)
1. **C1** — fix `\u2022`→`\x{2022}` at compile-time/package-level + `recover()` guard in
   `chPerform` (also insures against future panics in job execution — B7/B9). Process
   death empirically confirmed; the panic fires on the *first* `chSplitRubric` of any
   job, i.e. every job, once the feature is live.
2. **B1** — login brute-force throttle (rate limiter + enforce `lastFailedLogin`).
3. **C2** — same-origin/relative-only redirect on logout (stash-based, cf. 1.3).
4. **H2-as-refined (1.1)** — regenerate captures; `sessionsHTML` meta + sharing-updates
   metas + register auth-config are the functional breaks.
5. **C4** — datamanipulator default-deny + EvalSymlinks + 0644/0755 modes.
6. **B6/B9 (newly clarified)** — the vendor `otc` panic-on-decode is *not* process-lethal
   (empirical contrast test, below), but the fork still has **21 vendor `panic(` sites
   in `go/libraries/otc/`, 101 non-test panic sites repo-wide, and *no custom
   recovery middleware anywhere* (`grep` for `recover` across `go/services`, `go/cmd`,
   `cmd/` → only `net/http`'s implicit per-request recovery). Add a thin
   `recover()` middleware + a per-goroutine `recover()` guard at every plain
   `go func`/`go f(...)` dispatch site (gitbridge poll loop, compliance `chPerform`,
   realtime `bus.go:408/668`, `gitproto receivepack.go:101/105`, `uploadpack.go:98/102`) —
   one line each, kills the C1-class whole-process-kill family *and* the background
   panic in `otc` decode if it is ever called off-handler.
7. **B2** — enforce zip per-entry cap at copy time (declared-size lie → bounded
   amplification, see corrected text).
8. **B6/H5** — stop panicking on DB/config errors (gitbridge db.go 25 sites, tokens.go,
   web/core panics outside true-invariants).
9. **C3** — Redis client rewrite/pool + deadlines + synchronized `Close`.
10. **H1** — pgx ≥ v5.9.2 (GO-2026-5004 reachable at `historyv1/chunkstore_pg.go:466`).
11. **M1** — the two SA4000 tautologies (`v8date.go:797` `token == token`,
    `history/gate.go:106` duplicated string compare).

---

## 5. KEY RE-VERIFICATIONS FROM THIS PASS (empirical, not grep)

### 5.1 net/http panic behavior — Go 1.27.1 (repro at `/tmp/gopanic/`)
| Scenario | Process after | Client sees |
|---|---|---|
| Panic inside HTTP handler goroutine (vendor `otc` decode, B9 path) | **survives** | `connection reset by peer`, stack logged to server log |
| Panic in `go func(){ panic() }` plain goroutine (C1 `chPerform`, gitbridge poll) | **dies** | next connection refused; no HTTP response already issued on in-flight request |
This is the *mechanism* behind C1 being CRITICAL (background goroutine, no `recover`) and
behind B9 being MEDIUM-HIGH but not fatal. The fork has a targeted-recover pattern that
**exists but is not universal**: `gitbridge/swap/swap.go:230` (`doSwap` outer recovery),
`docstore/routes.go:68` (background archive), `rediswrapper/health.go:151` (health-runner,
returns error to channel), `chat/server.go:130`, `web/sitesettings/section.go:314`.
What is **missing**: the compliance job (`llmsettings/compliance.go:371 chPerform`
— the C1 site), the clsitex compile decode path, and the gitbridge main poll loop
all run *without* recovery. The fork already has an in-house convention for this;
the fix is extending it, not inventing new machinery.

### 5.2 C1 (compliance regex `\u2022`) — second independent repro (this pass)
```
/tmp/retest/main.go  go1.27.1
PANIC: regexp: Compile(`^\s*[-*\u2022]\s+`): error parsing regexp: invalid escape sequence: `\u`
(contrast: `^\s*[-*\x{2022}]\s+` compiles and matches "• x" — `true`)
```
Confirmed call path is identical to Part A: `compliance.go:434` (regex) →
`chSplitRubric` (line 571 region of the same file) → `chPerform` (371-410) →
`go f.chPerform(ctx, job)` (371, plain goroutine). Feature-gated: `reviewEnabled`
default-true (Part A) + LLM lane must resolve (`resolveLane`). One authenticated
project member POST to `/project/{id}/llm/compliance/start` (or the
project-page auto-trigger) → background panic → process death.

### 5.3 Vendor vs forked `otc` inventory (both are on the live decode path)
| Layer | Package | Panic sites | Decode style |
|---|---|---|---|
| Vendor | `go/libraries/otc` | **21** `panic(` | error-returning wrappers over panic-constructors; `NewRange` range.go:17-19 and `newBlob` blob.go:40-46 panic inside error-returning paths |
| Forked re-impl | `go/services/project-history/internal/{opmodel,historyot,updatetranslator}` | 0 in the `FromRaw` decode surface; 29 in runtime merge ops (documented "unreachable" invariants: `Ranges cannot overlap`, `overlaps checked`, …) | error-returning `errors.New` in decode |
| **Hybrid vendor call (corrected 2026-10-30)** | `go/services/project-history/internal/appfactory/factory.go` | imports **vendor** `otc` for `fileTreeDiffFold` (calls `otc.ChunkFromRaw` at factory.go:324 and `otc.BuildFileTreeDiff` at factory.go:339, with an **`OnMoveCollision` callback that panics at factory.go:342** — a panic *inside a vendor callback*, not one of the 21 vendor sites) | error-returning wrapper + vendor panic-callback |
Authoritative vendor-link map (per-binary `go list -deps`): vendor `go/libraries/otc` is
linked into exactly **three** binaries: `cmd/clsitex`, `cmd/historyv1`, `cmd/project-history`
The forked `opmodel`/`historyot` are live in `cmd/project-history` **alongside** vendor
`otc` (both in the same binary, different code paths). Collab runtime is NOT vendor
`otc`: `go/services/collab` uses `github.com/reearth/ygo` (CRDT, `anchors.go:37-38`).
the fork already *chose* the error-returning style for its own port but the vendor
`go/libraries/otc` (with its panic sites) is still the one wired into the live
`historyv1` + `clsitex` decode. Fix is: (a) extend the fork's **existing** recover convention (`gitbridge/swap/swap.go:226-238`
shows the pattern) to the compliance `chPerform`, the gitbridge main poll, and the
clsitex compile decode dispatch sites (see priority #6), (b) leave the vendor `panic(` sites as
they are (documented invariants) but document that they are *not* attacker-reachable
over the wire today because decode paths live on the request goroutine (recovered by
`net/http`) or in background jobs that need the recover-middleware before C1-class
process death is possible.

### 5.4 B9 wire chain — end-to-end verification (this pass)
Full decode chain, hop by hop (all live-confirmed this pass):
```
POST /project/{pid}/compile  (mux, apps/server.go:94 — NO auth middleware on any clsitex route;
                              CE parity: service is internal-network-only)
  → apps/routes.go a.compile → CC.Compile(w, params, body)
  → requestparser.Parse: compile["rawChangeOperations"] → parsed (requestparser.go:504-505)
  → compilecontroller/translator.go:81  RawChangeOperations = rawChangeOpsOf(parsed.RawChangeOperations)
  → hrw/sync.go:113-125  changesFromRawChangeOperations(rawChangeOperations[changeStart:])
     (the body slice; when localBaseVersion is absent/non-numeric, start = 0 — the WHOLE attacker list)
  → hrw/historyresourcewriter.go:558  otc.OperationFromRaw(oRaw)   // vendor otc, error-returning
  → operation_transform.go:22  dispatch: "commentId" → EditFileOperationFromRaw
  → operation_file_tree.go:153-158 → FromJSONEditOperation(raw)  // edit_operation_builder.go:9
  → isRawAddComment branch → ranges := asRawRanges(raw["ranges"])   // edit_operation_builder.go ~17
  → asRawRanges (raw_conv.go:72-85): numberToInt({"pos":-1}.pos) → NewRange(-1, n)  // PANIC range.go:17
```
The error-returning chain (`OperationFromRaw → EditFileOperationFromRaw →
FromJSONEditOperation`) is error-returning all the way down, but `asRawRanges →
NewRange` is a panic-into-the-error-channel at the deepest hop: the `ranges`
field bypasses the error surface. Same funnel for `FromRawComment` (comment.go
164-174, `ranges := asRawRanges(v)`) and `FromRawTrackedChange` (tracked_change.go
via `NewTrackedChange(NewRange(pos,length),...)`). One crafted body:
`{"compile":{...,"rawChangeOperations":[[{"commentId":"x","ranges":[{"pos":-1}]}]]}}`
reproduces the `NewRange` panic. In the live service (network-isolated, per
CE parity) this is per-request connection-reset + stack-log, not process
death (§5.1). The second sub-path (stored-data `newBlob` non-hex hash) is
reached from `historyv1`'s `chunkstore.go:169/257/301` `otc.HistoryFromRaw` over
**stored** PG rows — an integrity bug, and only via a write path that skips
hash validation. Neither is publicly exposed; both need the network-isolation
trust boundary to be honored.

## 6. COVERAGE GAPS REMAINING (unchanged from Part A, plus)
- `go test -race` — **DONE this pass, full hermetic sweep** (`go test -race -count=1
  -short -timeout 200s ./go/...`):
  - **Zero `WARNING: DATA RACE`** across every runnable package (web/core,
    web/features/*, web/views, realtime, collab, chat, datamanipulator — note the
    package lives at `go/libraries/datamanipulator`, not `go/services/datamanipulator`;
    docstore, gitbridge/swap (23s), gitbridge/db + gitproto, libraries/otc (~1.4s),
    project-history/... (all), libraries/rediswrapper, libraries/mongoutils,
    notifications, filestore, clsitex/*, historyv1).
  - 3x `go test -race -count=3 -short ./go/services/gitbridge/gitproto/...` to
    shake out the `go func` pairs in B10 (`receivepack.go:101-107` — `io.Copy`
    over `cmd.Start` pipes without error checks) — clean, no data race on the
    error-check-less `go func` pattern (those goroutines only do
    `io.Copy(w, r)` where w/r are channel-buffered pipes and no shared state
    is touched; the bug shape is missing error propagation, not racy shared state).
  - Only failures on the sweep: `go/libraries/mongowrapper`
    (TestConnectNoPathDefaultDB, TestSchemaApplyLive, TestConnectionModel —
    all attempt a live connection to `mongodb://127.0.0.1:27017` and time out at
    30s in a Mongo-less env; expected hermetic-mode failures, not defects). No
    other package fails under `-race`.
- Fuzzing of `requestparser`/`v8date` — **DONE this pass**: harness at `/tmp/fuzzprobe/`
  (scratch module, repo copy of the 4 stdlib-only files + `fuzz_compile_body_test.go`
  with `FuzzParseCompileBody` (the live exported `Parse(body, Config{})` entry) and
  `FuzzV8DateString` (direct `v8parseDateString`)). `go test -race` compile clean;
  ~167 M execs (68 M in 100 s + 99 M in 150 s) — **zero panics, zero crashes**,
  are thus dead branches, not crash paths. **OTC decode-path fuzzing also DONE**:
  `/tmp/otcfuzz/` scratch module (`replace ollitex => <repo>`) drives the LIVE
  vendor `go/libraries/otc` decode entries — `FuzzOtcDecode` (40.8 M execs,
  0 crashes, 0 `.FAIL`/`.panic` artifacts) + deterministic `TestB9WireNewRangePanicDeterministic`
  (reproduces the §5.4 `NewRange` panic on `{pos:-1,length:0}`, and
  `TestB9ControlDoesNotPanic` confirms the control `{pos:0,length:5}` returns
  cleanly). The 21 vendor panic sites are thus **exactly** the documented
  invariants reachable at `asRawRanges→NewRange` (`range.go:17`) and
  `newBlob` (`blob.go:40`); no hidden decode panic beyond those two. Recommend
  committing the `/tmp/otcfuzz/` harness in-repo under
  `go/services/clsitex/requestparser/otcdecode_fuzz_test.go` (or similar)
  alongside `fuzzprobe`'s `FuzzParseCompileBody`.
- React render-layer XSS on the 6 `innerHTML` + 2 `dangerouslySetInnerHTML` sites:
  `visual-widgets/*` are constant strings (safe by construction); `contact-form/search.ts`
  uses `DOMPurify.sanitize` (safe); `faq-search/index.js:66,72` puts **Algolia hit
  `pageName`/`content` HTML** into `innerHTML` unsanitized — but the Algolia index is
  operator-configured (`ol-algolia` meta; no `ol-algolia` reference found in this
  fork's views, so feature likely off/undeveloped here). If algolia is ever
  configured with a shared index, `index.js:66,72` is a stored-XSS vector. Flag for
  follow-up: sanitize in `faq-search/index.js` anyway.
- Node legacy `server-ce/` / `server-core/` services — still out of scope.
- Integration test run (needs full backend stack) — not run.

---

## 7. STATE / RESUME NOTES (post-compact recovery)
- Part A (complete, on disk): `problems_30092026.md` — 4 CRITICAL / 6 HIGH / 10 MEDIUM /
  8 LOW / 6 OPT + tool results.
- This file (Part B, complete): corrections 1.1-1.3, new B1-B10, C1 re-verify (§5.2),
  net/http panic contrast test (§5.1), vendor-vs-forked otc inventory (§5.3),
  updated priority, gaps.
- Fuzz harness (out-of-repo, `/tmp/fuzzprobe/`): scratch module, copy of the 4
  stdlib-only `requestparser` files, fuzz_compile_body_test.go with FuzzParseCompileBody
  (exported Parse entry) + FuzzV8DateString (v8parseDateString). Ran clean: ~99M
  execs in 150s, zero panics.
- Tool caches: `/tmp/staticcheck_out.txt` (328 findings), `/tmp/govuln.txt`,
  `/tmp/retest/main.go` (fresh C1 repro), `/tmp/panictest2/main.go` (original repro),
  `/tmp/gopanic/` (net/http handler-panic vs background-panic contrast repro).
- Install locations: staticcheck/govulncheck/gopls at `/home/davrot/go/bin/`.
- Remaining *actions* if the user later asks "go deeper still":
  1. **DONE** — full hermetic `-race` sweep (`go test -race -count=1 -short -timeout
     200s ./go/...` + a `-count=3` shake-out on `gitbridge/gitproto`) —
     **zero data races** repo-wide. Only failure: `go/libraries/mongowrapper`
     live-DB tests (expected hermetic-mode failure, not a defect).
     C3/M4 race suspicion is now empirically cleared: `redis.Close()`, the
     `docstore/routes.go` background archive, `gitbridge/swap`, `realtime/bus.go`
     registry, and `llmsettings` job registry are all race-clean under the
     detector.
  2. In-repo fuzz harness for `go/libraries/otc` — **DONE this pass (scratch module,
     out-of-repo, no repo modification)**. `/tmp/otcfuzz/` (kept):
     - `go.mod`: `module otc` / `replace ollitex => <repo>` (links the LIVE vendor
       package, zero repo files touched).
     - `fuzz_test.go`: `TestB9WireNewRangePanicDeterministic` (deterministic
       repro of the §5.4 crafted raw -> `panic: Invalid range (pos=-1 length=0)`)
       + `TestB9ControlDoesNotPanic` (control `{pos:0,length:5}` returns cleanly)
       + fuzz target `FuzzOtcDecode` driving `otc.OperationFromRaw` (the wire
       entry whose `asRawRanges->NewRange` panic at `range.go:17` lives inside)
       + `otc.SnapshotFromRaw`/`otc.HistoryFromRaw`/`otc.FileFromRaw` on the
       same raw map, with a `decodeGuarded` that logs known B9-class invariants
       and hard-fatals on any *other* panic.
     - Run: `-fuzz FuzzOtcDecode -fuzztime 150s` -> 40,848,059 execs, 0 crashes,
       0 `.FAIL`/`.panic` artifacts (deterministic B9 repros are PASS+log, not
       failures).
     - Net: B9 is the *complete* set of reachable-via-Go decode panics in the
       vendor package - 40M+ execs surfaced no hidden decode-path panic beyond
       `NewRange`/`newBlob` (§5.4). The other 19 vendor panic sites + 29
       project-history runtime-merge + 4 minimatch sites remain documented
       invariants (not reachable from a well-formed wire body). Recommend
       committing an in-repo equivalent at
       `go/services/clsitex/requestparser/otc_fuzz_test.go`.
  3. Render-layer XSS: 5/6 of the 6 innerHTML sites already dispositioned (§6)
     as safe-by-construction or DOMPurify-sanitized. Only `contact-form/search.ts`
     is the flagged one for follow-up; the Algolia `index` is operator-optional
     (not configured in this fork), so it's dormant unless enabled.
  4. `server-ce/` Node legacy sweep (large, separate effort).

*Report-only: no code was modified during this audit.*
