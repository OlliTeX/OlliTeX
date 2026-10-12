/**
 * external-probe — live checks of the LOCAL external-service credentials
 * (tests/e2e/credentials/config.json).
 *
 * Every test SKIPS unless its service is present + `enabled: true` in the
 * local config — without config.json the whole file is skipped and the rest
 * of the suite is unaffected. These probes verify the CREDENTIALS (the ones
 * that will drive the real sync/integration flows), not the app.
 *
 * Run alone:   npx playwright test specs/external-probe.test.e2e.ts
 */
import { test, expect } from '@playwright/test'
import { externalEnabled, getExternal, getServiceToken, TokenExpiredError } from '../credentials/external'

const T = 20_000

async function req(url: string, init: RequestInit = {}, headers: Record<string, string> = {}, ms = T) {
  const res = await fetch(url, { ...init, headers: { ...headers }, signal: AbortSignal.timeout(ms), redirect: 'follow' })
  let body: unknown = null
  const text = await res.text().catch(() => null)
  if (text) { try { body = JSON.parse(text) } catch { /* keep text */ } }
  return { status: res.status, body, text }
}

test('git host credentials resolve to a user (github/forgejo/gitea/gitlab)', async () => {
  const names = (['github', 'forgejo', 'gitea', 'gitlab'] as const).filter(n => externalEnabled(n))
  test.skip(names.length === 0, 'no git host enabled — see credentials/README.md')
  for (const name of names) {
    const cfg = getExternal(name)!
    const base = String(cfg.baseUrl).replace(/\/$/, '')
    const user = String(cfg.username || '')
    const pat = String(cfg.pat || '')
    let out: { status: number; body: any }
    if (name === 'github') {
      // github.com → api.github.com + /user; GHE → <host>/api/v3/user
      const ghHost = new URL(base).hostname
      const ghUrl = ghHost === 'github.com' ? 'https://api.github.com/user' : `${base}/api/v3/user`
      out = await req(ghUrl, {}, { Authorization: `Bearer ${pat}`, 'user-agent': 'ollitex-e2e' })
      expect(out.status, 'github /api/v3/user').toBe(200)
      const who = out.body?.login ?? out.body?.login_name
      if (user) expect(who, 'github login').toBe(user)
    } else if (name === 'gitlab') {
      out = await req(`${base}/api/v4/user`, {}, { 'PRIVATE-TOKEN': pat })
      expect(out.status, 'gitlab /api/v4/user').toBe(200)
      // gitlab usernames may be slugified (email → handle), so: token valid + a user resolved
      expect(out.body?.username, 'gitlab resolved user').toBeTruthy()
      if (user) expect(out.body?.username, 'gitlab resolved user').toBeTruthy()
    } else {
      // gitea + forgejo share the /api/v1 surface
      out = await req(`${base}/api/v1/user`, {}, { Authorization: `token ${pat}` })
      expect(out.status, `${name} /api/v1/user`).toBe(200)
      const who = out.body?.login ?? out.body?.login_name
      if (user) expect(who, `${name} login`).toBe(user)
    }
  }
})

test('webdav credentials list the root collection', async () => {
  test.skip(!externalEnabled('webdav'), 'webdav not enabled — see credentials/README.md')
  const cfg = getExternal('webdav')!
  const base = String(cfg.baseUrl).replace(/\/$/, '')
  const auth = Buffer.from(`${cfg.username}:${cfg.password}`).toString('base64')
  const res = await fetch(base, {
    method: 'PROPFIND',
    headers: {
      Authorization: `Basic ${auth}`,
      Depth: '0',
      'Content-Type': 'application/xml; charset=utf-8',
    },
    body: '<?xml version="1.0" encoding="utf-8"?><propfind xmlns="DAV:"><prop><resourcetype/></prop></propfind>',
    signal: AbortSignal.timeout(T),
  })
  const status = res.status
  await res.arrayBuffer().catch(() => null)
  expect(status === 200 || status === 207, `webdav PROPFIND got ${status}`).toBeTruthy()
})

test('dropbox token is valid (cached or static)', async () => {
  test.skip(!externalEnabled('dropbox'), 'dropbox not enabled — see credentials/README.md')
  const cfg = getExternal('dropbox')!
  let token: string | null | undefined = cfg.token ? String(cfg.token) : undefined
  if (!token) token = getServiceToken('dropbox')?.token
  if (!token && cfg.refreshToken && cfg.appKey && cfg.appSecret) {
    // 2026-10-12: headless EXTEND — the refreshToken grant (stable app pair)
    // mints a fresh access token per the config _readme policy
    const r = await fetch('https://api.dropboxapi.com/oauth2/token', {
      method: 'POST',
      headers: { Authorization: 'Basic ' + Buffer.from(`${cfg.appKey}:${cfg.appSecret}`).toString('base64') },
      body: new URLSearchParams({ grant_type: 'refresh_token', refresh_token: String(cfg.refreshToken) }),
      signal: AbortSignal.timeout(T),
    }).then((x) => x.json().catch(() => ({})))
    if (r && r.access_token) token = r.access_token
  }
  test.skip(!token, 'dropbox: no token — run `node scripts/auth-capture.mjs --service dropbox --url <authorize-url>` (see credentials/README.md), or set §dropbox.token / refreshToken(+appKey/appSecret) in config.json')
  // Dropbox v2 API: GET → 400 "Use HTTP POST requests" (pinned 2026-10-12)
  const out = await req('https://api.dropboxapi.com/2/users/get_current_account', { method: 'POST' }, { Authorization: `Bearer ${token}` })
  expect([200, 201], 'dropbox account call').toContain(out.status)
  expect(out.body?.account_id, 'dropbox account_id').toBeTruthy()
})

test('zotero API key resolves the library', async () => {
  test.skip(!externalEnabled('zotero'), 'zotero not enabled — see credentials/README.md')
  const cfg = getExternal('zotero')!
  // 2026-10-12: this leg needs the CLASSIC ZOTERO_API_KEY pair (a distinct
  // credential from the OAuth account pair); skip cleanly when the key is
  // absent (the OAuth pair drives specs/zotero-link-cycle.test.e2e.ts).
  test.skip(!cfg.apiKey, 'zotero "apiKey" (classic) not set in credentials/config.json')
  const base = String(cfg.baseUrl).replace(/\/$/, '')
  // zotero.org API: /api/users/{id} (or /api/users/byUsername/{name}); key header suffices,
  // ZOTERO_USER header only needed for byUsername lookups
  const id = cfg.userId ? String(cfg.userId) : ''
  const username = cfg.username ? String(cfg.username) : ''
  const url = id ? `${base}/api/users/${id}` : username ? `${base}/api/users/byUsername/${username}` : null
  test.skip(!url, `zotero: needs "userId" or "username" in credentials/config.json`)
  const out = await req(url, {}, { ZOTERO_API_KEY: String(cfg.apiKey), ...(username ? { ZOTERO_USER: username } : {}) })
  expect(out.status, 'zotero /api/users/{id}').toBe(200)
})

test('LLM provider answers /models', async () => {
  test.skip(!externalEnabled('llm'), 'llm not enabled — see credentials/README.md')
  const cfg = getExternal('llm')!
  const base = String(cfg.baseUrl).replace(/\/$/, '')
  const out = await req(`${base}/models`, {}, { Authorization: `Bearer ${cfg.apiKey}` })
  expect(out.status, 'llm /models').toBe(200)
  const arr = Array.isArray(out.body) ? out.body : out.body?.data
  expect(Array.isArray(arr) && arr.length > 0, 'llm model list non-empty').toBeTruthy()
})

// TokenExpiredError is exported so future specs can catch it; referenced to
// keep the import honest.
test('token-cache contract: expired cache throws actionable error', async ({}) => {
  const fs = await import('node:fs')
  const path = await import('node:path')
  const { authDir, saveServiceToken } = await import('../credentials/external')
  const p = path.join(authDir(), 'probe-contract.json')
  try {
    saveServiceToken('probe-contract', 'x', { ttlMs: -1 }) // already expired
    expect(() => getServiceToken('probe-contract'), 'expired token must throw').toThrow(TokenExpiredError)
  } finally {
    fs.rmSync(p, { force: true })
  }
})
