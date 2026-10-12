/**
 * e2e — Mendeley link cycle (ag-v57; 2026-10-12 credentials arc)
 * ------------------------------------------------------------------
 * Product surface pinned from the Go port
 * (go/services/web/features/mendeley/{mendeley,settings,client,cipher}.go):
 *
 *   GET  /user/mendeley/status        → {"configured":bool,"connected":bool}
 *   GET  /mendeley/groups             → 200 groups (linked) | 403
 *                                       {"error":"forbidden","message":"mendeley_groups_relink"}
 *   GET  /user/mendeley/oauth         → 302 https://api.mendeley.com/oauth/authorize
 *                                       ?client_id&redirect_uri(=MENDELEY_CALLBACK_URL verbatim)
 *                                       &response_type=code&scope=all&state=<32 hex, session-stored>
 *   GET  /user/mendeley/oauth/callback?code&state
 *                                       → server-side exchange (client id/secret) +
 *                                         encrypted store under users.refProviders.mendeley.encrypted
 *                                         → ALWAYS 302 /hub#mysettings.references (Node parity)
 *   POST /mendeley/unlink             → 200 ($unset refProviders.mendeley; idempotent;
 *                                       NO remote revoke — not in the port)
 *
 * WORLD CONFIG (e2e world, 2026-10-12): MENDELEY_CLIENT_ID/SECRET env +
 * MENDELEY_CALLBACK_URL = the console-registered
 * https://psintern.neuro.uni-bremen.de/user/mendeley/oauth/callback (Mendeley
 * requires byte-exact redirect_uri match); site_settings.global.mendeley section
 * written to the stored shape the READER requires (clientSecret in the section
 * — env never carries it; that seed-vs-reader asymmetry is PINNED here).
 *
 * LEGS (M1–M10)
 *  M1–M6  headless, unattended: status, groups-403, oauth-302 contract,
 *         forged callback (no cred may appear), unlink idempotent ×2.
 *  M7     EXTEND (headless, pure API): Mendeley REFRESH grant at
 *         https://api.mendeley.com/oauth/token (client id/secret Basic auth) —
 *         skips until config.mendeley.refreshToken exists.
 *  M8     OPERATOR-ASSISTED CONNECT (MENDELEY_MANUAL_LOGIN=1, --headed):
 *         Mendeley login (www.mendeley.com) is CLOUDFLARE-WALLED headless
 *         (verified 2026-10-12: "Attention Required!" challenge page — no
 *         bypass). Operator logs in + approves; the spec captures the
 *         callback URL (code+state) and drives /user/mendeley/oauth/callback
 *         on THIS world with the session state created on origin 7420.
 *         Known race: the console-registered callback host (psintern) is a
 *         different instance — if it is Mendeley-configured it may consume the
 *         one-shot code first; the spec re-authorizes (≤3 cycles).
 *  M9     EXTEND after connect — refresh grant issues a fresh access token.
 *  M10    world-agnostic linked/unlinked groups pin.
 *
 * Run (from tests/e2e):
 *   BASE_URL=http://127.0.0.1:7420 CI=1 npx playwright test specs/mendeley-link-cycle.test.e2e.ts
 *   MENDELEY_MANUAL_LOGIN=1 BASE_URL=http://127.0.0.1:7420 npx playwright test \
 *     specs/mendeley-link-cycle.test.e2e.ts --headed   # + operator connect
 */
import { test, expect } from '@playwright/test'
import { USER } from '../fixtures/credentials'
import { loginRobust } from '../helpers/auth'
import { api } from '../parity/harness'
import { getExternal } from '../credentials/external'

test.describe.configure({ mode: 'serial' })
test.setTimeout(300000) // manual connect cycles when armed

const MEN = (getExternal('mendeley') as any) || {}
const API = MEN.baseUrl || 'https://api.mendeley.com'
const HOST = 'http://127.0.0.1:7420'

function b64(s: string): string {
  return Buffer.from(s).toString('base64')
}

import fs from 'node:fs'
function cachedRefresh(name: string): string | null {
  try {
    const d = JSON.parse(fs.readFileSync(`credentials/.auth/${name}.json`, 'utf8'))
    return (d.meta && typeof d.meta.refreshToken === 'string' && (d.meta.refreshToken as string)) || null
  } catch { return null }
}

test('M1: status — configured (app seeded) + unlinked at run start', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const s = await api(page, 'GET', '/user/mendeley/status')
  const body = await s.json() as any
  expect(s.status()).toBe(200)
  expect(body.configured, 'MENDELEY_* world config must show configured').toBe(true)
  expect(body.connected).toBe(false)
})

test('M2: /mendeley/groups unlinked → pinned 403 relink envelope', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const st = (await (await api(page, 'GET', '/user/mendeley/status')).json()) as any
  const res = await api(page, 'GET', '/mendeley/groups')
  if (st.connected) {
    expect(res.status()).toBe(200)
    return
  }
  expect(res.status()).toBe(403)
  expect(await res.json()).toEqual({ error: 'forbidden', message: 'mendeley_groups_relink' })
})

test('M3: /oauth — 302 to api.mendeley.com authorize with the pinned exact param set', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const r = await page.request.get(HOST + '/user/mendeley/oauth', { maxRedirects: 0 })
  expect(r.status()).toBe(302)
  const u = new URL(r.headers()['location'])
  expect(u.origin + u.pathname).toBe('https://api.mendeley.com/oauth/authorize')
  expect(u.searchParams.get('client_id')).toBe(String(MEN.clientId || '26163'))
  expect(u.searchParams.get('redirect_uri')).toBe(MEN.callbackURL) // console-registered, verbatim
  expect(u.searchParams.get('response_type')).toBe('code')
  expect(u.searchParams.get('scope')).toBe('all')
  expect(u.searchParams.get('state')).toMatch(/^[0-9a-f]{32}$/) // session-stored state
})

test('M4: forged-code / wrong-state callback → 302 references, NO cred stored', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  // state deliberately does NOT match the session state → the exchange branch
  // must not run at all; the Node-parity terminal redirect still happens
  const s = await api(page, 'GET', '/user/mendeley/status')
  const pre = (await s.json()) as any
  const r = await page.request.get(
    HOST + '/user/mendeley/oauth/callback?code=forged-e2e-code&state=' + 'f'.repeat(32),
    { maxRedirects: 0 },
  )
  expect(r.status()).toBe(302)
  expect(r.headers()['location']).toBe('/hub#mysettings.references')
  const st = (await (await api(page, 'GET', '/user/mendeley/status')).json()) as any
  expect(st.connected, 'forged exchange must not store credentials').toBe(pre.connected)
})

test('M5: unlink (already unlinked) → 200 idempotent ($unset, no remote revoke)', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const res = await api(page, 'POST', '/mendeley/unlink')
  expect(res.status()).toBe(200)
  const st = (await (await api(page, 'GET', '/user/mendeley/status')).json()) as any
  expect(st.connected).toBe(false)
})

test('M6: unlink again → 200 (idempotent)', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  expect((await api(page, 'POST', '/mendeley/unlink')).status()).toBe(200)
})

test('M7: EXTEND — Mendeley refresh grant (headless, pure API)', async () => {
  const m7rt: string | null = MEN.refreshToken ? String(MEN.refreshToken) : cachedRefresh('mendeley')
  test.skip(!m7rt,
    'Mendeley: no refreshToken yet — complete one operator authorization ' +
    '(MENDELEY_MANUAL_LOGIN=1, --headed) or paste config.mendeley.refreshToken (+clientId/clientSecret)')
  const r = await fetch(API + '/oauth/token', {
    method: 'POST',
    headers: {
      Authorization: 'Basic ' + b64(`${MEN.clientId}:${MEN.clientSecret}`),
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    body: 'grant_type=refresh_token&refresh_token=' + encodeURIComponent(m7rt!),
    signal: AbortSignal.timeout(30000),
  }).then(x => x.json().catch(() => ({}) as any))
  expect(r.access_token, 'refresh grant must issue a fresh access token').toBeTruthy()
  console.log('  [M7] Mendeley token EXTENDED headlessly ✓ (refresh grant → new access token)')
})

test('M8: CONNECT (operator-assisted) — Mendeley login + approve, code capture, callback exchange here', async ({ page }) => {
  test.skip(!process.env.MENDELEY_MANUAL_LOGIN,
    'operator connect: run with MENDELEY_MANUAL_LOGIN=1 ... --headed from an operator-trusted ' +
    'machine. Mendeley login (www.mendeley.com) is Cloudflare-walled headless (verified — no bypass).')

  // fresh per-test context (Playwright default) → authenticate THIS session first
  await loginRobust(page, USER.email, USER.password)
  console.log('  [M8] ⏳ headed browser is opening Mendeley — complete the login + approve there; ' +
    'the callback URL (code+state) will be captured automatically and the connect executed on this world')
  const st = (await (await api(page, 'GET', '/user/mendeley/status')).json()) as any
  if (st.connected) return // already connected — nothing to prove

  for (let cycle = 1; cycle <= 3; cycle++) {
    console.log(`  [M8] authorize cycle ${cycle}/3`)
    // this-world session: create the state the exchange will verify
    const o = await page.request.get(HOST + '/user/mendeley/oauth', { maxRedirects: 0 })
    const authz = new URL(o.headers()['location'])
    const state = authz.searchParams.get('state')!
    // operator browser (headed) does the Cloudflare'd login + approve
    await page.goto(authz, { waitUntil: 'domcontentloaded', timeout: 300000 }).catch(() => {})
    const cb = await page
      .waitForURL(/code=[A-Za-z0-9._~-]+/, { timeout: 285000 })
      .then(() => page.url())
      .catch(() => null)
    if (!cb) { console.warn('  [M8] no callback URL captured this cycle (login not completed?)'); continue }
    const q = new URL(cb).searchParams
    const code = q.get('code')!
    console.log('  [M8] code captured (masked) …' + code.slice(-3))
    // same-origin exchange, state from THIS session
    await page.request.get(
      `${HOST}/user/mendeley/oauth/callback?code=${encodeURIComponent(code)}&state=${encodeURIComponent(state)}`,
      { maxRedirects: 0 },
    ).catch(() => {})
    for (let i = 0; i < 10; i++) {
      const s = (await (api(page, 'GET', '/user/mendeley/status').catch(() => null)) as any)?.json?.()
      const cur = await s
      if (cur?.connected) break
      await new Promise(res2 => setTimeout(res2, 1000))
      if (i === 9) console.warn('  [M8] not connected after exchange — possible one-shot code race (psintern instance)')
    }
    const fin = (await (await api(page, 'GET', '/user/mendeley/status')).json()) as any
    if (fin.connected) { console.log('  [M8] connected ✓ after cycle ' + cycle); return }
  }
  throw new Error('M8: could not reach connected state in 3 operator cycles')
})

test('M9: EXTEND after connect — refresh grant issues a fresh access token', async () => {
  test.skip(!process.env.MENDELEY_MANUAL_LOGIN && !(MEN.token || MEN.refreshToken || cachedRefresh('mendeley')),
    'M9: no world token to refresh — run the manual connect (M8) first, or set config.mendeley.refreshToken')
  const m9rt: string | null = MEN.refreshToken ? String(MEN.refreshToken) : cachedRefresh('mendeley')
  test.skip(!m9rt || !MEN.clientId || !MEN.clientSecret,
    'config.mendeley.refreshToken (or .auth/mendeley.json meta.refreshToken) + clientId + clientSecret all required for the refresh leg')
  const r = await fetch(API + '/oauth/token', {
    method: 'POST',
    headers: {
      Authorization: 'Basic ' + b64(`${MEN.clientId}:${MEN.clientSecret}`),
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    body: 'grant_type=refresh_token&refresh_token=' + encodeURIComponent(m9rt!),
    signal: AbortSignal.timeout(30000),
  }).then(x => x.json().catch(() => ({}) as any))
  expect(r.access_token, 'refresh grant must issue a fresh access token').toBeTruthy()
  console.log('  [M9] EXTEND after connect ✓ (headless refresh)')
})

test('M10: linked state — status + groups (world-agnostic pin)', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const st = (await (await api(page, 'GET', '/user/mendeley/status')).json()) as any
  const g = await api(page, 'GET', '/mendeley/groups')
  if (st.connected) {
    expect(g.status()).toBe(200)
    const body = await g.json()
    expect(Array.isArray(body) || Array.isArray((body as any).groups || (body as any).collection),
      'groups payload must be an array (or {groups|collection})').toBe(true)
  } else {
    expect(g.status()).toBe(403)
    expect(await g.json()).toEqual({ error: 'forbidden', message: 'mendeley_groups_relink' })
  }
})
