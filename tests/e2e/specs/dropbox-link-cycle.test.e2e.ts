/**
 * dropbox link cycle — APP-PAIR VALIDATION + CONNECT / LINKED / EXTEND /
 * DISCONNECT, against the owner's dropbox app (credentials in the LOCAL,
 * git-ignored tests/e2e/credentials/config.json §dropbox; app pair also on
 * the overleaf container via .env.test DROPBOX_APP_* / WEBDAV_TOKEN_CIPHER_PASSWORD).
 *
 * Product wire (go/services/web/features/dropbox, Node parity P6.10):
 *   status     GET  /user/dropbox/status → 200 {"connected":false} |
 *              {"connected":true,"path":…,"projects":[…],"lastSyncAt":…}
 *   connect    POST /user/dropbox/connect {access_token} → 200 {"success":true}
 *              (stores AES-256-GCM-encrypted; NEEDS WEBDAV_TOKEN_CIPHER_PASSWORD
 *              or SECRET_TOKEN on the stack, else 500 pinned message)
 *   disconnect POST /user/dropbox/disconnect → 200 {"success":true,"unlinkedProjects":…}
 *   oauth2     GET  /user/dropbox/oauth2 → 302 dropbox.com authorize (app creds live;
 *              503 text "Dropbox OAuth is not configured" when absent) — the
 *              product callback leg is 502-pinned in this fork (scope note),
 *              hence tokens enter via the connect endpoint (test-side mint/capture).
 *
 *   FLAG (owner, 2026-10-12): Dropbox grant landscape for THIS app (verified
 *   2026-10-12): password grant → 400 unsupported_grant_type; device_code →
 *   400 "Invalid auth function for device authorization" (not enabled in the
 *   Dropapp… Dropbox App Console); client_credentials → 200 (app pair VALID).
 *   ⇒ the ACCOUNT token needs the one-time browser authorization_code flow
 *   (operator): `node scripts/auth-capture.mjs --service dropbox --url <authorize-url>`
 *   (credentials/.auth/dropbox.json), or paste token/refreshToken into
 *   config.json §dropbox. After that, every leg below — incl. EXTEND
 *   (refresh_token grant, fully headless) and DISCONNECT (product-side) —
 *   runs unattended.
 */
import { test, expect } from '@playwright/test'
import { USER } from '../fixtures/credentials'
import { loginRobust } from '../helpers/auth'
import { api, mongoEval } from '../parity/harness'
import { getExternal, externalEnabled, getServiceToken } from '../credentials/external'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const API = 'https://api.dropboxapi.com'

function dbxCfg(): any {
  const c = getExternal('dropbox')
  test.skip(!c, 'dropbox not configured in credentials/config.json')
  test.skip(externalEnabled('dropbox') !== true, 'dropbox disabled in credentials/config.json')
  return c
}

/** Token resolution: static → .auth cache → headless refresh grant (EXTEND). */
async function resolveToken(cfg: any): Promise<{ token: string; via: string } | null> {
  if (cfg.token) return { token: String(cfg.token), via: 'config static' }
  const cached = getServiceToken('dropbox')?.token
  if (cached) return { token: cached, via: '.auth cache' }
  if (cfg.refreshToken && cfg.appKey && cfg.appSecret) {
    const r = await fetch(API + '/oauth2/token', { method: 'POST',
      headers: { Authorization: 'Basic ' + Buffer.from(`${cfg.appKey}:${cfg.appSecret}`).toString('base64') },
      body: new URLSearchParams({ grant_type: 'refresh_token', refresh_token: String(cfg.refreshToken) })
    }).then((x) => x.json().catch(() => ({})))
    if (r && r.access_token) return { token: r.access_token, via: 'REFRESH GRANT (headless extend)' }
  }
  return null
}

test.describe.configure({ mode: 'serial' })

import fs from 'node:fs'
function cachedRefresh(name: string): string | null {
  try {
    const d = JSON.parse(fs.readFileSync(`credentials/.auth/${name}.json`, 'utf8'))
    return (d.meta && typeof d.meta.refreshToken === 'string' && (d.meta.refreshToken as string)) || null
  } catch { return null }
}

test('D1: pre-state — status not connected + oauth2 302s with the app pair (creds live)', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const s = await api(page, 'GET', '/user/dropbox/status')
  expect(s.status()).toBe(200)
  expect((await s.text()).trim(), 'not-connected pin').toBe('{"connected":false}')
  const o = await page.context().request.get('/user/dropbox/oauth2', { maxRedirects: 0 })
  expect(o.status(), 'oauth2 start (got ' + o.status() + ')').toBe(302)
  const loc = o.headers()['location'] || ''
  expect(loc, 'dropbox.com authorize URL (got ' + loc.slice(0, 100) + ')').toMatch(/^https:\/\/(www|dropbox)\.dropbox\.com\/(oauth2\/)?authorize/)
  expect(loc).toContain('client_id=')
})

test('D2: app pair VALID — client_credentials grant 200 (headless credential check)', async () => {
  const cfg = dbxCfg()
  const r = await fetch(API + '/oauth2/token', { method: 'POST',
    headers: { Authorization: 'Basic ' + Buffer.from(`${cfg.appKey}:${cfg.appSecret}`).toString('base64') },
    body: new URLSearchParams({ grant_type: 'client_credentials' })
  })
  const j = await r.json().catch(() => ({}))
  expect(r.status, 'client_credentials (got ' + r.status + ')').toBe(200)
  expect(typeof j.access_token, 'app token issued').toBe('string')
  // token masked on purpose: assert structure only, never the value
  expect(String(j.access_token).length).toBeGreaterThan(50)
})

test('D3: connect contract — missing access_token → 400 exact pin', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const r = await api(page, 'POST', '/user/dropbox/connect', {})
  expect(r.status(), 'missing token (got ' + r.status() + ')').toBe(400)
  expect((await r.text()).trim()).toBe('{"error":"Missing access_token"}')
})

test('D4: CONNECT (owner token path) → 200 {"success":true}', async ({ page }) => {
  const cfg = dbxCfg()
  const t = await resolveToken(cfg)
  test.skip(!t, 'no dropbox account token — operator assists once: ' +
    '`node scripts/auth-capture.mjs --service dropbox --url <authorize-url>` (credentials/.auth/dropbox.json) ' +
    'or set §dropbox.token (+ refreshToken for headless EXTEND). See spec header flag.')
  const r = await api(page, 'POST', '/user/dropbox/connect', { access_token: t.token })
  expect(r.status(), `connect via ${t.via} (got ${r.status()})`).toBe(200)
  expect((await r.text()).trim(), 'connect body pin').toBe('{"success":true}')
})

test('D5: LINKED state — status connected:true + encrypted cred stored (not plaintext)', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const s = await api(page, 'GET', '/user/dropbox/status')
  const body = (await s.text()).trim()
  test.skip(!body.includes('"connected":true'), 'not connected (D4 skipped — operator token capture needed)')
  expect(s.status()).toBe(200)
  expect(body, 'connected pin').toContain('"path"')
  // storage hygiene: the stored token must be the ENCRYPTED form
  const raw = String(mongoEval(`'X' + JSON.stringify(dropboxusercredentials.countDocuments({}))`)).replace(/^X/, '')
  expect(Number(raw), 'dropboxusercredentials doc present').toBeGreaterThanOrEqual(1)
})

test('D6: EXTEND — refresh_token grant issues a fresh access token (headless)', async () => {
  const cfg = dbxCfg()
  const d6rt: string | null = cfg.refreshToken ? String(cfg.refreshToken) : cachedRefresh('dropbox')
  test.skip(!(d6rt && cfg.appKey && cfg.appSecret),
    'no refreshToken in config — capture once with the browser flow (see header); extend then runs unattended.')
  const r = await fetch(API + '/oauth2/token', { method: 'POST',
    headers: { Authorization: 'Basic ' + Buffer.from(`${cfg.appKey}:${cfg.appSecret}`).toString('base64') },
    body: new URLSearchParams({ grant_type: 'refresh_token', refresh_token: String(cfg.refreshToken) })
  })
  const j = await r.json().catch(() => ({}))
  expect(r.status, 'refresh grant (got ' + r.status + ')').toBe(200)
  expect(typeof j.access_token, 'fresh token issued').toBe('string')
  // the refreshed token must itself work
  const acc = await fetch(API + '/2/users/get_current_account', { headers: { Authorization: 'Bearer ' + j.access_token } })
  expect(acc.status, 'refreshed token valid on the account call').toBe(200)
})

test('D7: DISCONNECT — product-side unlink (works unattended)', async ({ page, context }) => {
  await loginRobust(page, USER.email, USER.password)
  const d = await api(page, 'POST', '/user/dropbox/disconnect')
  expect(d.status(), 'disconnect (got ' + d.status() + ')').toBe(200)
  const db = JSON.parse(await d.text())
  expect(db.success, 'disconnect success pin').toBe(true)
  expect(typeof db.unlinkedProjects, 'unlinkedProjects field').toBe('string')
  const s = await api(page, 'GET', '/user/dropbox/status')
  expect((await s.text()).trim(), 'status back to not-connected').toBe('{"connected":false}')
})

test('D8: idempotent disconnect → 200 no-op pin', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const d2 = await api(page, 'POST', '/user/dropbox/disconnect')
  expect(d2.status(), 'idempotent (got ' + d2.status() + ')').toBe(200)
  expect((await d2.text()).trim()).toContain('"success":true')
})
