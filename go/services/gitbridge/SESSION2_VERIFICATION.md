# git-bridge.go — Session 2 verification record (2026-09-20)

A/B verification of the Go rewrite against the Java reference, with a mock
Overleaf snapshot API as the referee.

## Harness
- Mock web-api (`/tmp/gbtest/mock.mjs`, port 44101): token mode, docs +
  saved_vers, POST /docs/:id push capture, async postback fetch, binary file.
- Java reference: port 44102, root /tmp/gbroot-java (JGit, single JVM).
- Go under test: port 44103, root /tmp/gbroot-go (`git_bridge serve`).
- Battery: 46 HTTP cases (grid errors, 401/403 auth, smart-HTTP, postback
  contract, file URL, options, 404s), `/tmp/gbtest/battery.mjs`.
- Push round-trip script: clone → commit → push → verify ref + re-clone,
  on BOTH stacks (`/tmp/gbtest/pushtest.sh`).

## Bugs found & fixed
1. **FATAL (clone): Go shadowing in `UrlResourceCache.Get`**
   (`internal/resource/resource.go`). `contents, err := c.fetch(...)` inside
   `if !known { }` shadowed the outer `contents` → attachment files served as
   0 bytes → Go clones had empty files. Java has no `:=` shadowing.
   Fixed with plain assignment. Verification: byte-identical clones
   (blob sha256 match) on both stacks.
2. **FATAL (push): proc-receive hook never installed in production**
   (`internal/repo/project.go` `InstallProcReceiveHook` was test-only).
   Without it, pushes were plain ref moves — no Overleaf write-back.
   Fixed: `Bridge.SetProcReceiveHook(fn)` (duck-typed installer),
   `ensureHook()` in `getUpdatedRepoCritical` (fatal on failure), wired in
   `cmd/git_bridge` to `exec <absExe> -hook-proc-receive <absCfg>`.
3. **FATAL (push): cross-process postback promise.** Push logic runs in the
   hook process; the postback POST + file-fetch GETs land in the serve
   process. In-memory `PostbackManager` is per-process → the hook waiter
   hung forever; Overleaf file fetches 404'd. Java (one JVM) never hit this.
   Fixed with shared sqlite state (`postback_store` table in `.wlgb/wlgb.db`):
   `PostbackStore` interface + `SetStore` (duck-typed wiring in NewBridge so
   unit fakes stay in-memory); `Post{VersionID,Exception}ForProject` resolve
   via the store when no local promise (matching key → resolve; mismatched
   key → silent per Java; no row → `UnexpectedPostbackException` 409);
   `WaitProjectForVersionIdOrThrow` polls BOTH the local promise and the
   store (50ms tick, 360s bound = Java `PostbackPromise.TIMEOUT_SECONDS`,
   timeout → `PostbackTimeoutException`); `CheckPostbackKey` accepts a
   pending store row with a matching key. Exception payloads
   (invalidFiles/invalidProject error lists) are body-encoded across the
   process boundary via `ExceptionFromCode`/`exceptionCode`/`exceptionBody`.

## Smaller parity fixes
- Grid error bodies (403/401/404): Jetty sends them with NO Content-Type;
  Go's `net/http` default-injects `text/plain; charset=utf-8`. Fixed with the
  `grid()` helper (`w.Header()["Content-Type"] = nil`) at all four sites
  (file.go, oauth2.go ×2, shared.go).
- `gitServe` 200 responses: added the Jetty `NoCacheFilter` headers
  (Expires=1980, Pragma, Cache-Control).
- Documented deviation: Java sends `Server: Jetty(12.1.6)`; Go does not fake
  it (git clients never read the Server header).

## Results
- Battery: **46 cases, 2 diffs — both acceptable** (JGit vs `git` capability
  advertisement lines in `info/refs`; both standard, clients adapt).
  All 403/401/404 grid cells, auth failures, postback contract
  (upToDate/bogus/error/outOfDate incl. 409 unexpectedPostback), file-URL
  404s, OPTIONS, and 404s match.
- Clone: byte-identical working trees + blob hashes on both stacks.
- **Push round-trip on BOTH stacks: green** — Overleaf push body shape
  identical (`{latestVerId, files:[{name,url?}], postbackUrl}`), postback
  round-trip completes, ref moves, re-clone shows the new file with the
  stack-specific marker.
- Cross-process atts file serving: pending store key → 200 with byte-exact
  512B binary; unknown key → 404.
- `go build` + `go vet ./...`: clean. `go test ./...`: all packages pass
  except two KNOWN ENVIRONMENTAL failures (independent of these changes):
  `filestore TestWriteFailsUnreadableDir` (asserts non-root; this machine
  runs root, which bypasses chmod 000) and `server TestGitBodyBytesParity`
  (requires `/tmp/jwire/cap/*.body` fixtures from the author's machine).

## Known follow-ups (not done in this session)
- Wire this service into `overleaf/go/services/` (target per plan).
- Node web-module git-bridge routes (`/api/v0/pat/*`, `/oauth/token/info`,
  `/api/v0/docs/:id`) are a separate Go-web feature to flip later
  (P6.19 scope).
- `receive.procreceiverefs` is hard-coded `adm:refs/heads/` in both Java and
  Go (parity); configurable later.
- 360s postback timeout, 130-bit keys, sqlite store: all parity-pinned.
