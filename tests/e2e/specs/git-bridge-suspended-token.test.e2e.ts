/**
 * git-bridge-suspended-token — suspend/unsuspend gates PAT auth at the
 * choke point the git bridge depends on (and at the bridge edge itself).
 *
 * TODO-edac5d9d (upstream-impact wave 2026-10-11, adopted; Go fix 6096fa82):
 * a personal access token (PAT) must be REJECTED while its owner is
 * SUSPENDED — at the token-validation choke point (/oauth/token/info — the
 * endpoint the bridge's oauth2 filter calls for EVERY git auth) — and the
 * rejection must fire at the bridge's auth layer for the git flow itself,
 * and must clear again once the user is re-enabled (same token restored;
 * suspension does not delete credentials).
 *
 * Wire facts pinned here (OlliTeX Go ports, verified 2026-11-11):
 *   - PAT create:  POST /git-bridge/personal-access-tokens (logged-in user)
 *                  → 200 {_id, accessToken, accessTokenPartial, createdAt,
 *                    expiresAt}   (cap 10 tokens/user → 403; "olp_"+36 alnum;
 *                  the raw token is returned ONCE, at creation).
 *   - token info:  GET /oauth/token/info with `Authorization: Bearer <pat>`
 *                  → 200 {user_id, scope} while healthy; 401 while suspended
 *                  (tokenResolved choke point — the single gate the bridge
 *                  oauth2 filter depends on; 429 only under rate limit).
 *   - git auth:    the bridge validates per request via /oauth/token/info
 *                  (oauth2.go, 1:1 Oauth2Filter port: Basic `git:<token>` —
 *                  the username MUST be the literal `git`; user-password
 *                  auth is disabled by config → anything else is 401 by
 *                  filter step 7). Auth rejection → git sees 401; the later
 *                  repo-materialisation stage is a DIFFERENT layer.
 *   - suspend:     POST /admin/user/:id/update {suspended: true|false}.
 *   - The bridge has no host-published port in the e2e stack (internal
 *     compose service), so the git edge is exercised from INSIDE the bridge
 *     container (which ships git) against `git-bridge:8000` — the exact
 *     client position a workstation would occupy on that network.
 *
 * KNOWN-SEPARATE-GAP (not this spec's contract, pinned as its own board
 * item): repo materialisation (bridge GetUpdatedRepo → project-history
 * LatestVersion) fails in BOTH stacks — the yops→history-chunk flush chain
 * has never completed (projectHistoryChunks = 0 stack-wide), so a healthy
 * git ls-remote currently dies at repo materialisation (403) BEFORE any
 * content. The suspension gate under test fires at the AUTH layer, which
 * runs first and is therefore assertable regardless of that gap:
 *   - suspended token  → bridge rejects with 401 (auth layer),
 *   - healthy token    → bridge accepts auth (reaches the later repo stage;
 *                        NOT 401 at auth) — status 403 in the current
 *                        stack is the known materialisation gap, not auth.
 *
 * Run alone: npx playwright test specs/git-bridge-suspended-token.test.e2e.ts
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'child_process'
import { loginRobust } from '../helpers/auth'
import { api, mongoEval } from '../parity/harness'
import { ADMIN, USER } from '../fixtures/credentials'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const BRIDGE = 'git-bridge:8000'

function bridgeContainer(): string {
  const names: string[] = execFileSync('docker', ['ps', '--format', '{{.Names}}'], {
    encoding: 'utf8',
  })
    .split('\n')
    .filter(Boolean)
  const hit = names.find(n => /-e2e-git-bridge-1$/.test(n))
  if (!hit) throw new Error('git-bridge container not found (expected *-e2e-git-bridge-1)')
  return hit
}

/** git ls-remote against the bridge from inside the bridge container.
 * Returns the bridge's HTTP status as seen by the git client. git renders
 * different bodies per status (401 = the Oauth2Filter challenge text;
 * 403 = grid/smart 403 bodies), so the code is parsed from the error text
 * (0 = transport/success, -1 = unparseable). */
function gitBridgeStatus(token: string, project: string): { code: number; out: string } {
  const args = [
    'git', 'ls-remote', '--heads',
    // the e2e bridge serves PLAIN HTTP on 8000 (no TLS in the disposable
    // stack config — gitbridge-runtime.json); username MUST be `git`
    // (Oauth2Filter step 6) — anything else is 401 at the auth layer.
    `http://git:${token}@${BRIDGE}/${project}`,
  ]
  try {
    const out = execFileSync('docker', ['exec', '--', bridgeContainer(), ...args], {
      encoding: 'utf8',
      timeout: 60_000,
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    return { code: 0, out }
  } catch (e: any) {
    const err = String(e?.stderr || e?.message || '')
    const m = err.match(/error: (\d{3})/) || err.match(/HTTP\/1\.[01] (\d{3})/)
    if (m) return { code: Number(m[1]), out: err.slice(0, 400) }
    if (/Enter your Git authentication token|authentication token has expired/i.test(err)) return { code: 401, out: err.slice(0, 400) }
    if (/Overleaf now only supports Git authentication tokens/i.test(err)) return { code: 403, out: err.slice(0, 400) }
    return { code: -1, out: err.slice(0, 400) }
  }
}

function tokenInfo(pat: string): Promise<number> {
  return fetch(`${BASE}/oauth/token/info`, {
    headers: { authorization: `Bearer ${pat}` },
  }).then(r => r.status)
}

test.describe.configure({ mode: 'serial' })

let adminPage: any = null
let userPage: any = null
let adminCtx: any = null
let userCtx: any = null
let userId: string = ''
let projectId: string = ''
let token: string = ''
let tokenId: string = ''
let suspended = false

test.beforeAll(async ({ browser }) => {
  const a = await browser.newContext()
  adminCtx = a
  adminPage = await a.newPage()
  await loginRobust(adminPage, ADMIN.email, ADMIN.password)

  const u = await browser.newContext()
  userCtx = u
  userPage = await u.newPage()
  await loginRobust(userPage, USER.email, USER.password)

  userId = String(mongoEval(`db.users.findOne({ email: '${USER.email}' })._id.toHexString()`)).trim()
  expect(userId, 'fixture user id present').toMatch(/^[0-9a-f]{24}$/)

  // SELF-HEAL: a previous aborted run can leave the fixture suspended (test
  // 2 suspends; a hard stop before test 3/afterAll would wedge it — a
  // suspended user cannot log in, which would break the whole stack's
  // fixture). Start from a known-ENABLED state every time.
  const wasSuspended = String(mongoEval(`db.users.findOne({ email: '${USER.email}' }).suspended`) || '').trim()
  if (wasSuspended === 'true' || wasSuspended === '1') {
    const up0 = await api(adminPage, 'POST', `/admin/user/${userId}/update`, { suspended: false })
    expect(up0.status(), 'self-heal: re-enable the fixture user').toBeLessThan(300)
  }

  // project fixture — the git-bridge repo name (the auth layer validates
  // the token BEFORE repo materialisation, so a bare API-created project
  // suffices for this spec's contract).
  const r = await api(userPage, 'POST', '/project/new', { projectName: 'e2e-git-suspended-token-fixture' })
  expect(r.status(), 'project create').toBeLessThan(300)
  const j = await r.json().catch(() => ({}))
  projectId = j?.project_id || j?._id || j?.project?._id
  expect(projectId, 'project id').toMatch(/^[0-9a-f]{24}$/)

  // PAT fixture (cap 10): clean stale tokens from earlier runs so the cap
  // can never trip this leg.
  const l0: any[] = await (await api(userPage, 'GET', '/git-bridge/personal-access-tokens'))
    .json()
    .catch(() => [])
  for (const t of l0 || []) {
    if (t?._id) {
      await api(userPage, 'DELETE', `/git-bridge/personal-access-tokens/${t._id}`).catch(() => {})
    }
  }
  const c = await api(userPage, 'POST', '/git-bridge/personal-access-tokens')
  expect(c.status(), 'PAT create').toBeLessThan(300)
  const cj = await c.json().catch(() => ({}))
  token = cj.accessToken
  tokenId = cj._id
  expect(token, 'accessToken returned once, at creation').toMatch(/^olp_[A-Za-z0-9]{36}$/)
})

test.afterAll(async () => {
  // SAFETY FIRST, unconditionally: suspend/unsuspend is a COMMITTING admin
  // call. A spec failure between the suspend POST and any bookkeeping must
  // never wedge the shared fixture suspended (a suspended user cannot log
  // in, which breaks every other spec). Always force the safe state, then
  // clean the token.
  if (userId && adminPage) {
    await api(adminPage, 'POST', `/admin/user/${userId}/update`, { suspended: false }).catch(() => {})
  }
  if (token && tokenId && userPage) {
    await api(userPage, 'DELETE', `/git-bridge/personal-access-tokens/${tokenId}`).catch(() => {})
  }
  if (userCtx) await userCtx.close().catch(() => {})
  if (adminCtx) await adminCtx.close().catch(() => {})
})

test('1. baseline: healthy PAT passes token-info; bridge auth layer accepts it', async () => {
  expect(await tokenInfo(token), 'token-info 200 while healthy').toBe(200)
  // The bridge auth layer must NOT reject a healthy token (whatever the
  // later repo-materialisation stage does — see KNOWN-SEPARATE-GAP): a 401
  // here would mean the suspension/validity gate is misfiring.
  const g = gitBridgeStatus(token, projectId)
  expect(g.code, 'bridge auth layer accepts the healthy token (no 401): ' + g.out).not.toBe(401)
})

test('2. suspended: token-info 401s; bridge rejects the token at the AUTH layer; token row intact', async () => {
  const up = await api(adminPage, 'POST', `/admin/user/${userId}/update`, { suspended: true })
  // mark committed BEFORE asserting: the suspend is already live
  // server-side, so the safety bookkeeping must hold regardless of this
  // assertion's outcome.
  suspended = true
  expect(up.status(), 'suspend user (admin)').toBeLessThan(300)

  expect(await tokenInfo(token), 'token-info 401 while suspended').toBe(401)

  const g = gitBridgeStatus(token, projectId)
  expect(g.code, 'bridge rejects the suspended token with 401 at auth:\n' + g.out).toBe(401)

  // the token row survives suspension (unsuspend must restore the SAME
  // credential — a deleted token would be a different bug class).
  const lj: any[] = (await (await api(userPage, 'GET', '/git-bridge/personal-access-tokens')).json().catch(() => [])) as any[]
  expect((lj || []).some(t => t?._id === tokenId), 'token still listed while owner suspended').toBe(true)
})

test('3. unsuspended: the same PAT passes token-info again; bridge auth accepts it again (round-trip, no re-issue)', async () => {
  const up = await api(adminPage, 'POST', `/admin/user/${userId}/update`, { suspended: false })
  expect(up.status(), 'unsuspend user (admin)').toBeLessThan(300)
  suspended = false

  expect(await tokenInfo(token), 'token-info 200 after unsuspend (same token)').toBe(200)

  const g = gitBridgeStatus(token, projectId)
  expect(g.code, 'bridge auth accepts the unsuspended token again (no 401):\n' + g.out).not.toBe(401)
})
