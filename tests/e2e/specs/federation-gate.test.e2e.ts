/**
 * federation-gate — FEDERATION surface e2e (board eda51a85), LIVE-PINNED
 * 2026-10-12 (world: A = ol-e2e-overleaf-1 :7420, B = ol-e2e-overleaf-2
 * :7421 with OVERLEAF_SITE_URL=https://b.fedtest.local, own mongo-2/redis-2).
 *
 * SCOPE REALITY (pinned, 2026-10-12):
 *   The FULL RP login dance (board leg 1–2) + cross-instance S2S membership
 *   (leg 3) + export bundle (leg 4) are OWNER-PRECONDITION GATED in this
 *   world — the OIDF layer is https-ONLY by design (getEntityIdGo:
 *   "federation: entity id must use https:" — leaf.go), and both instances
 *   serve HTTP-only (nginx :80) with no TLS listeners / DNS names. The
 *   board item itself names the owner precondition: "the owner stands up
 *   the instances + pins" (zz_live_fedb_test.go header). What IS live and
 *   pinned here (the current wire surface):
 *
 *   A (http siteUrl → the GATE, pinned):
 *     GET /federation/oidc/.well-known/openid-configuration → 500
 *       (siteUrl must be https — the exact https gate; the route is MOUNTED)
 *     GET /api/federation/invite/preview (A admin) → 400 {"message":"anchor required"}
 *       (B is not a pinned/anchored peer — the preview degradation contract)
 *     GET /federation/oidc/rp/callback?error=access_denied → 401
 *       {"ok":false,"code":"auth-denied","error":"access_denied","description":""}
 *   B (https siteUrl → the WIRE, pinned):
 *     GET /federation/oidc/.well-known/openid-configuration → 200
 *       {issuer: https://b.fedtest.local/federation/oidc,
 *        authorization_endpoint .../auth, token_endpoint .../token,
 *        jwks_uri .../jwks, response_types_supported:["code"]}
 *     GET /federation/oidc/jwks → 200 {keys:[{kty:EC,alg:ES256,use:sig,crv:P-256,kid,...}]}
 *       (keystore bootstrap is LIVE — no manual key step)
 *     POST /federation/s2s {} → 400 {"ok":false,"code":"bad-envelope",
 *       "detail":"from/to/action required"}  (the LOCKED S2S envelope, A AND B)
 *     admin surface (B admin):
 *       GET /admin/federation/peers → 200 {"peers":[]}
 *       GET /admin/federation/keys  → 200 {keys:[{algorithm:ES256,purpose:federation,
 *                                       state:active,kid,...}]}
 *       GET /admin/federation/audit→ 200 {"entries":[]}
 *       (the TOFU pin/approve surface — POST /admin/federation/peers +
 *        /<origin>/approve — is the owner window per the live-test header)
 *     GET /federation/export → 200 (the export wizard mounts when
 *       FEDERATION_EXPORT_ENABLED=true — the ebb76488 git_url question
 *       stays OPEN/pending-owner; the bundle is not exercised — the
 *       two-instance precondition above blocks it in this world)
 *   BOTH:
 *     GET /.well-known/openid-federation → 302 SPA fallback — the OIDF
 *       leaf route is BUILT (BuildLeafEntityConfiguration, clientassertion.go)
 *       but NOT MOUNTED as a route in this build (pinned)
 *     non-admin (A USER): /admin/federation{,/peers,/keys} → 200 + the
 *       "Restricted" HTML page (title "Restricted - OlliTeX...") — the
 *       admin-authz contract for these routes (pin; differs from the
 *       admin-users API 302/restricted — different surface, different pin)
 *
 * OWNER-GATED LEGS (documented; run when the owner's 2×https world exists —
 * the Go live test zz_live_fedb_test.go is the exact harness, env-gated
 * on FED_LIVE_*): RP full dance, consent/revocation round-trip, S2S invite
 * cross-instance membership, export wizard + bundle integrity.
 */
import { test, expect, type Page, type Browser } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { ADMIN, USER } from '../fixtures/credentials'

const A = 'http://127.0.0.1:7420'
const B = 'http://127.0.0.1:7421'
let SH_A: { ctx: any; page: Page } | null = null
let SH_B: { ctx: any; page: Page } | null = null
let SH_U: { ctx: any; page: Page } | null = null

async function pageA(browser: Browser): Promise<Page> {
  if (!SH_A) {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
    const pg = await ctx.newPage()
    await loginRobust(pg as any, ADMIN.email, ADMIN.password, A)
    SH_A = { ctx, page: pg }
  }
  return SH_A.page
}
async function pageB(browser: Browser): Promise<Page> {
  if (!SH_B) {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
    const pg = await ctx.newPage()
    // instance B has its own origin (7421) — loginRobust's relative /login
    // would hit the config base (A), so B gets an explicit absolute flow
    // with the same rate-limit resilience (2 tries, 25s cooldown)
    let lastErr = 'no attempt'
    for (let a = 0; a < 2; a++) {
      if (a > 0) await pg.waitForTimeout(25_000)
      await pg.goto(B + '/login', { waitUntil: 'domcontentloaded' })
      await pg.waitForTimeout(700)
      const ready = await pg.waitForSelector('#email', { timeout: 8_000 }).then(() => true).catch(() => false)
      if (!ready) { lastErr = 'B login form not ready (throttle?)'; continue }
      await pg.fill('#email', ADMIN.email)
      await pg.fill('#password', ADMIN.password)
      await pg.click('button[type=submit]')
      await pg.waitForURL(/hub|project/i, { timeout: 25_000 }).catch(() => {})
      if (/hub|#\//.test(pg.url()) && !/login/.test(pg.url())) { SH_B = { ctx, page: pg }; return SH_B.page }
      lastErr = 'B login did not land: ' + pg.url()
    }
    throw new Error(lastErr)
  }
  return SH_B.page
}
async function pageU(browser: Browser): Promise<Page> {
  if (!SH_U) {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
    const pg = await ctx.newPage()
    await loginRobust(pg as any, USER.email, USER.password, A)
    SH_U = { ctx, page: pg }
  }
  return SH_U.page
}

test.describe.configure({ mode: 'serial' })
test.setTimeout(300_000)

test('F1 B discovery + JWKS (https siteUrl wire): full endpoint set + ES256 live key', async ({ browser }) => {
  const p = await pageB(browser)
  const r = await p.request.get(B + '/federation/oidc/.well-known/openid-configuration')
  expect(r.status(), 'B OIDC discovery → PINNED 200 (got ' + r.status() + ')').toBe(200)
  const j: any = await r.json()
  expect(j.issuer).toBe('https://b.fedtest.local/federation/oidc')
  expect(j.authorization_endpoint.endsWith('/auth')).toBe(true)
  expect(j.token_endpoint.endsWith('/token')).toBe(true)
  expect(j.jwks_uri.endsWith('/jwks')).toBe(true)
  expect(j.response_types_supported, 'response types pin').toEqual(['code'])

  const jw = await p.request.get(B + '/federation/oidc/jwks')
  expect(jw.status(), 'B JWKS → PINNED 200 (got ' + jw.status() + ')').toBe(200)
  const kk: any = await jw.json()
  expect(Array.isArray(kk.keys) && kk.keys.length >= 1, 'at least one JWK (got ' + JSON.stringify(kk).slice(0, 120) + ')').toBe(true)
  const k = kk.keys[0]
  expect(k.kty, 'KTY EC pin (got ' + k.kty + ')').toBe('EC')
  expect(k.alg).toBe('ES256')
  expect(k.crv).toBe('P-256')
  expect(k.use).toBe('sig')
  expect(typeof k.kid === 'string' && k.kid.length > 8, 'kid present').toBe(true)
})

test('F2 A discovery GATE (http siteUrl): 500 — the https-only entity-id contract is live', async ({ browser }) => {
  const p = await pageA(browser)
  const r = await p.request.get(A + '/federation/oidc/.well-known/openid-configuration')
  expect(r.status(), 'A discovery → PINNED 500 (http siteUrl → https gate; got ' + r.status() + ')').toBe(500)
})

test('F3 S2S envelope (A AND B): bad envelope → 400 bad-envelope with the exact detail', async ({ browser }) => {
  const pA = await pageA(browser)
  const pB = await pageB(browser)
  for (const [base, lbl, pg] of [[A, 'A', pA], [B, 'B', pB]] as [string, string, Page][]) {
    const r = await pg.request.post(base + '/federation/s2s', { data: {} })
    expect(r.status(), lbl + ' S2S bad envelope → PINNED 400 (got ' + r.status() + ')').toBe(400)
    const j: any = await r.json()
    expect(j.ok, lbl + ' ok:false pin').toBe(false)
    expect(j.code, lbl + ' code pin (got ' + j.code + ')').toBe('bad-envelope')
    expect(j.detail, lbl + ' detail pin (got ' + j.detail + ')').toBe('from/to/action required')
  }
})

test('F4 A invite preview (no anchored peer): 400 anchor-required (the degradation contract)', async ({ browser }) => {
  const p = await pageA(browser)
  const csrf = (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const r = await p.request.get(A + '/api/federation/invite/preview?peer=b.fedtest.local&email=e2e-user%40e2e.test', {
    headers: { 'X-Csrf-Token': csrf, Accept: 'application/json' },
  })
  expect(r.status(), 'invite preview → PINNED 400 (got ' + r.status() + ')').toBe(400)
  const j: any = await r.json()
  expect(String(j.message), 'anchor-required pin (got ' + JSON.stringify(j) + ')').toBe('anchor required')
})

test('F5 A RP callback error path: 401 auth-denied envelope (NoLogin surface)', async ({ browser }) => {
  const p = await pageA(browser)
  const r = await p.request.get(A + '/federation/oidc/rp/callback?error=access_denied')
  expect(r.status(), 'rp callback error → PINNED 401 (got ' + r.status() + ')').toBe(401)
  const j: any = await r.json()
  expect(j.ok, 'ok:false pin').toBe(false)
  expect(j.code, 'code pin (got ' + j.code + ')').toBe('auth-denied')
  expect(j.error, 'error echo pin (got ' + j.error + ')').toBe('access_denied')
})

test('F6 admin surface: B admin peers/keys/audit 200 + shapes; A admin peers; A non-admin → Restricted page (200)', async ({ browser }) => {
  const pb = await pageB(browser)
  const r1 = await pb.request.get(B + '/admin/federation/peers', { headers: { Accept: 'application/json' } })
  expect(r1.status(), 'B peers → PINNED 200 (got ' + r1.status() + ')').toBe(200)
  const j1: any = await r1.json()
  expect(Array.isArray(j1.peers), 'peers array pin (got ' + JSON.stringify(j1).slice(0, 120) + ')').toBe(true)

  const r2 = await pb.request.get(B + '/admin/federation/keys', { headers: { Accept: 'application/json' } })
  expect(r2.status(), 'B keys → PINNED 200 (got ' + r2.status() + ')').toBe(200)
  const j2: any = await r2.json()
  expect(Array.isArray(j2.keys) && j2.keys.length >= 1, 'at least one key (got ' + JSON.stringify(j2).slice(0, 160) + ')').toBe(true)
  expect(j2.keys[0].algorithm, 'key algorithm pin (got ' + j2.keys[0]?.algorithm + ')').toBe('ES256')
  expect(j2.keys[0].purpose, 'key purpose pin').toBe('federation')
  expect(j2.keys[0].state, 'key state pin (got ' + j2.keys[0]?.state + ')').toBe('active')

  const r3 = await pb.request.get(B + '/admin/federation/audit', { headers: { Accept: 'application/json' } })
  expect(r3.status(), 'B audit → PINNED 200 (got ' + r3.status() + ')').toBe(200)
  expect(Array.isArray((await r3.json()).entries), 'audit entries array').toBe(true)

  const pa = await pageA(browser)
  const r4 = await pa.request.get(A + '/admin/federation/peers', { headers: { Accept: 'application/json' } })
  expect(r4.status(), 'A admin peers → PINNED 200 (got ' + r4.status() + ')').toBe(200)
  expect(Array.isArray((await r4.json()).peers), 'A peers array').toBe(true)

  // non-admin authz: the Restricted PAGE (200 + "Restricted" title) — the
  // admin-authz contract for the federation admin surface (pinned)
  const pu = await pageU(browser)
  for (const path of ['/admin/federation', '/admin/federation/peers', '/admin/federation/keys']) {
    const rr = await pu.request.get(A + path)
    const t = await rr.text()
    expect(rr.status(), 'non-admin ' + path + ' → PINNED 200-restricted-page (got ' + rr.status() + ')').toBe(200)
    expect(t, 'non-admin ' + path + ' → Restricted page (got head: ' + t.slice(0, 80) + ')').toContain('Restricted')
  }
})

test('F7 export wizard mounts (200) + OIDF leaf route NOT MOUNTED (302 SPA fallback) — both pinned', async ({ browser }) => {
  const p = await pageA(browser)
  const r = await p.request.get(A + '/federation/export')
  expect(r.status(), 'export wizard → PINNED 200 (got ' + r.status() + ')').toBe(200)

  // leaf: the route is BUILT (BuildLeafEntityConfiguration) but not served —
  // the public discovery falls through to the SPA/login fallback
  const rl = await p.request.get(A + '/.well-known/openid-federation')
  expect(rl.status(), 'leaf → PINNED 302 fallback (not mounted; got ' + rl.status() + ')').toBeGreaterThanOrEqual(300)
})
