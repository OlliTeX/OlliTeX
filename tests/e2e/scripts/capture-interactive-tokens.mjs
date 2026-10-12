#!/usr/bin/env node
/**
 * capture-interactive-tokens.mjs — one-time OPERATOR TOKEN CAPTURE
 * ================================================================
 * Captures account OAuth tokens for services whose provider login is
 * walled against headless automation (Cloudflare/Turnstile — verified
 * 2026-10-12 for mendeley/zotero; dropbox excluded), and EXTENDS them
 * headlessly thereafter (refresh grants).
 *
 * Why this exists (vs doing it inside e2e specs): the spec-browser flow
 * is operator-in-the-loop by design; this tool isolates that single human
 * step, captures the code at the BROWSER level (route-abort — the callback
 * request never reaches ANY server instance, so one-shot codes cannot be
 * consumed by a different OlliTeX deployment), exchanges it straight
 * server-side (product parity wires), and lands the tokens in the
 * credentials store — where ALL specs (link-cycles, external-probe)
 * re-arm automatically.
 *
 * SERVICES
 *  mendeley : api.mendeley.com  (app 'Ollitex'; authorize → code →
 *               /oauth/token with client Basic; refresh_token kept for
 *               headless EXTEND — same pattern as dropbox)
 *  dropbox  : www.dropbox.com   (authorize w/ token_access_type=offline →
 *               code → api.dropboxapi.com/oauth2/token; refresh kept)
 *  zotero   : zotero.org 1.0a  (request-token → authorize → callback
 *               carries oauth_token+oauth_verifier → exchange → LONG-LIVED
 *               apiKey (+secret, userId, username) — written to
 *               credentials/config.json §zotero.{userId,apiKey}, which
 *               re-arms the external-probe zotero leg instantly)
 *
 * OUTPUT (never printed — files only, 0600):
 *  credentials/.auth/<service>.json  {token, obtainedAt, expiresAt?, meta}
 *  credentials/config.json §zotero   {userId, apiKey, apiKeySecret, username}
 *  (meta carries refreshToken / tokenSecret etc. — read by the link-cycle
 *   specs' EXTEND legs)
 *
 * USAGE (from tests/e2e, on an operator-trusted machine WITH a display):
 *   node scripts/capture-interactive-tokens.mjs --service mendeley
 *   node scripts/capture-interactive-tokens.mjs --service dropbox zotero
 *   node scripts/capture-interactive-tokens.mjs --all
 *   node scripts/capture-interactive-tokens.mjs --service mendeley --dry-run
 * The headed browser opens; log in + approve when prompted. Each callback
 * capture is aborted in-browser at up to 300s wait.
 */
import fs from 'node:fs'
import path from 'node:path'
import { randomBytes } from 'node:crypto'
import { chromium } from 'playwright'

const cfgAll = JSON.parse(fs.readFileSync(path.join(process.cwd(), 'credentials/config.json'), 'utf8'))
const b64 = s => Buffer.from(s).toString('base64')
const mask = t => (t && String(t).length > 8) ? String(t).slice(0, 4) + '…' + String(t).slice(-4) : '∅'
const { createHmac, randomBytes: rb } = await import('node:crypto')
// ---------- OAuth 1.0a (HMAC-SHA1, RFC 5849) — parity with go/.../zotero/oauth.go ----------
function pe(str) {   // percentEncode: unreserved pass-through, else %XX (uppercase)
  let out = ''
  for (let i = 0; i < str.length; i++) {
    const c = str.charCodeAt(i)
    const ch = str[i]
    if ((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
        || ch === '-' || ch === '_' || ch === '.' || ch === '~') out += ch
    else for (const b of Buffer.from(ch, 'utf8')) out += '%' + b.toString(16).toUpperCase().padStart(2, '0')
  }
  return out
}
function oauth1Header(method, rawURL, ck, cs, tokKey, tokSec, data) {
  const params = {
    oauth_nonce: rb(16).toString('base64url'),
    oauth_timestamp: String(Math.floor(Date.now() / 1000)),
    oauth_signature_method: 'HMAC-SHA1',
    oauth_consumer_key: ck,
    oauth_version: '1.0',
    ...(!tokKey ? {} : { oauth_token: tokKey }),
    ...data,
  }
  const keys = Object.keys(params).sort((a, b) => (pe(a) === pe(b) ? (pe(params[a]) < pe(params[b]) ? -1 : 1) : (pe(a) < pe(b) ? -1 : 1)))
  const canon = keys.map(k => pe(k) + '=' + pe(params[k])).join('&')
  const base = method + '&' + pe(rawURL) + '&' + pe(canon)
  const signKey = pe(cs) + '&' + (tokSec ? pe(tokSec) : '')
  const sig = createHmac('sha1', signKey).update(base, 'utf8').digest('base64')
  params.oauth_signature = sig
  const names = ['oauth_nonce', 'oauth_timestamp', 'oauth_signature_method', 'oauth_consumer_key', 'oauth_version'].filter(n => n in params)
  if (tokKey) names.push('oauth_token')
  names.push(...Object.keys(data))
  const parts = names.map(n => n + '="' + pe(params[n]) + '"')
  parts.push('oauth_signature="' + pe(sig) + '"')
  return 'OAuth ' + parts.join(', ')
}
async function oauth1Call(method, url, ck, cs, tokKey, tokSec, data) {
  const r = await fetch(url, {
    method,
    headers: { Authorization: oauth1Header(method, url, ck, cs, tokKey, tokSec, data), 'User-Agent': 'Overleaf-CEP-Zotero' },
    signal: AbortSignal.timeout(30_000),
  })
  const text = await r.text()
  try { return Object.fromEntries(new URLSearchParams(text)) }
  catch { return { _error: r.status + ': ' + text.slice(0, 160) } }
}

const AUTH_DIR = path.join(process.cwd(), 'credentials', '.auth')
const WAIT_MS = 300_000

function saveAuth(name, token, meta = {}, expiresInSec) {
  fs.mkdirSync(AUTH_DIR, { recursive: true })
  const doc = { token, obtainedAt: Date.now() }
  if (expiresInSec) doc.expiresAt = Date.now() + expiresInSec * 1000
  if (meta) doc.meta = meta
  const p = path.join(AUTH_DIR, `${name}.json`)
  fs.writeFileSync(p, JSON.stringify(doc, null, 2), { mode: 0o600 })
  console.log(`  ✓ ${name}: token = ${mask(token)} → ${p}`)
  return p
}

async function postForm(url, form, headers = {}) {
  const r = await fetch(url, {
    method: 'POST',
    headers: { ...headers, 'Content-Type': 'application/x-www-form-urlencoded' },
    body: typeof form === 'string' ? form : new URLSearchParams(form).toString(),
    signal: AbortSignal.timeout(30_000),
  })
  const text = await r.text()
  let body
  try { body = JSON.parse(text) } catch { body = Object.fromEntries(new URLSearchParams(text)) }
  if (!r.ok) throw new Error(`${url} → ${r.status}: ${text.slice(0, 160)}`)
  return body
}

/**
 * Navigate to the provider authorize URL, wait for the REDIRECT-CALLBACK
 * request (the URL carrying code/state/verifier), abort it in the browser
 * (so no server instance ever sees/consumes the one-shot code), and return
 * the parsed request URL.
 */
async function captureCallback(page, authzUrl, callbackHint) {
  console.log('  ⏳ browser at provider login/approval — complete it manually…')
  await page.goto(authzUrl, { waitUntil: 'domcontentloaded', timeout: 30_000 }).catch(() => {})
  // intercept the callback (whatever host it targets — registered redirect)
  await page.route('**/*', route => {
    const u = route.request().url()
    if (callbackHint && callbackHint.test(u)) return route.abort('blockedbyclient')
    return route.continue()
  }).catch(() => {})
  const req = await page.waitForRequest(r => callbackHint.test(r.url()), { timeout: WAIT_MS })
    .catch(e => { console.warn('  ✗ callback not captured within 300s — provider login/approval incomplete?'); throw e })
  const u = new URL(req.url())
  console.log('  ✓ callback captured (aborted in-browser; code never left the machine): ' + u.origin + u.pathname)
  return u
}

const ARGS = process.argv.slice(2)
const DRY = ARGS.includes('--dry-run')
let SERVICES = []
if (ARGS.includes('--all')) SERVICES = ['mendeley', 'dropbox', 'zotero']
else {
  const i = ARGS.indexOf('--service')
  if (i > -1) SERVICES = ARGS.slice(i + 1).filter(a => !a.startsWith('--'))
  else throw new Error('usage: --service mendeley dropbox zotero | --all | --dry-run')
}

const browser = DRY ? null : await chromium.launch({ headless: false })
const contexts = []

for (const service of SERVICES) {
  console.log(`\n== ${service.toUpperCase()} ==`)
  const cfg = cfgAll[service]

  // ---------- MENDELEY ----------
  if (service === 'mendeley') {
    const need = cfg?.clientId && cfg?.clientSecret && cfg?.callbackURL
    if (!need) { console.log('  ✗ config.mendeley.{clientId,clientSecret,callbackURL} required'); continue }
    const state = randomBytes(16).toString('hex')
    const authz = new URL('https://api.mendeley.com/oauth/authorize')
    for (const [k, v] of Object.entries({ client_id: cfg.clientId, redirect_uri: cfg.callbackURL, response_type: 'code', scope: 'all', state })) authz.searchParams.set(k, v)
    console.log(`  authorize: ${authz.origin + authz.pathname} (state ${state.slice(0, 6)}…)`)
    if (DRY) { continue }
    const ctx = await browser.newContext(); contexts.push(ctx)
    const page = await ctx.newPage()
    const cbUrl = new URL(cfg.callbackURL)
    const cbHint = /^(https?:)?\/\/[^/]+\/user\/mendeley\/oauth\/callback/i
    const u = await captureCallback(page, authz.toString(), cbHint)
    const code = u.searchParams.get('code')
    if (!code) throw new Error('no code in callback URL: ' + u.search)
    const tok = await postForm('https://api.mendeley.com/oauth/token', {
      grant_type: 'authorization_code', code, redirect_uri: cfg.callbackURL,
    }, { Authorization: 'Basic ' + b64(`${cfg.clientId}:${cfg.clientSecret}`) })
    if (!tok.access_token) throw new Error('mendeley exchange failed: ' + JSON.stringify(tok).slice(0, 120))
    saveAuth('mendeley', tok.access_token, {
      refreshToken: tok.refresh_token || null,
      clientId: cfg.clientId, clientSecret: cfg.clientSecret,
      user: cfg.username || null, scope: 'all',
    }, tok.expires_in)
    console.log(`  ✓ Mendeley ${tok.refresh_token ? 'access+refresh' : 'access'} captured (scope=all)` +
      (tok.refresh_token ? ' — headless EXTEND now available' : ' ⚠ no refresh_token (token_access=web?)'))
  }

  // ---------- DROPBOX ----------
  if (service === 'dropbox') {
    const need = cfg?.appKey && cfg?.appSecret
    const cb = cfg.callbackURL || 'https://psintern.neuro.uni-bremen.de/user/dropbox/oauth/callback'
    if (!need) { console.log('  ✗ config.dropbox.{appKey,appSecret} required'); continue }
    const authz = new URL('https://www.dropbox.com/oauth2/authorize')
    for (const [k, v] of Object.entries({ client_id: cfg.appKey, response_type: 'code', redirect_uri: cb, token_access_type: 'offline' })) authz.searchParams.set(k, v)
    console.log(`  authorize: ${authz.origin + authz.pathname} (offline/refresh)`)
    console.log('  NOTE: if Dropbox reports "Redirect URI not in App Settings", add EXACTLY\n' +
      `        ${cb}\n        to the app's OAuth2 settings (developers.dropbox.com) and retry.`)
    if (DRY) { continue }
    const ctx = await browser.newContext(); contexts.push(ctx)
    const page = await ctx.newPage()
    const cbHint = /^(https?:)?\/\/[^/]+\/user\/dropbox\/oauth\/callback/i
    const u = await captureCallback(page, authz.toString(), cbHint)
    const code = u.searchParams.get('code')
    if (!code) throw new Error('no code in callback URL: ' + u.search)
    const tok = await postForm('https://api.dropboxapi.com/oauth2/token', {
      code, grant_type: 'authorization_code', redirect_uri: cb,
    }, { Authorization: 'Basic ' + b64(`${cfg.appKey}:${cfg.appSecret}`) })
    if (!tok.access_token) throw new Error('dropbox exchange failed: ' + JSON.stringify(tok).slice(0, 120))
    saveAuth('dropbox', tok.access_token, {
      refreshToken: tok.refresh_token || null,
      appKey: cfg.appKey, appSecret: cfg.appSecret,
      user: cfg.username || null, account: tok.account_id || null,
    }, tok.expires_in)
    console.log(`  ✓ Dropbox ${tok.refresh_token ? 'access+refresh (offline)' : 'access'} captured — link-cycle D4/D6 + external-probe re-arm`)
  }

  // ---------- ZOTERO (OAuth 1.0a HMAC-SHA1, product parity) ----------
  if (service === 'zotero') {
    const cbz = cfg?.callbackURL || 'http://127.0.0.1:7420/user/zotero/oauth/callback'
    if (!(cfg?.clientKey && cfg?.clientSecret)) { console.log('  \u2717 config.zotero.{clientKey,clientSecret} required'); continue }
    console.log(`  oauth_callback (sent to zotero): ${cbz}`)
    // (a) request-token  POST https://www.zotero.org/oauth/request
    const reqTok = await oauth1Call('POST', 'https://www.zotero.org/oauth/request',
      cfg.clientKey, cfg.clientSecret, '', '', { oauth_callback: cbz })
    if (reqTok._error || !reqTok.oauth_token) throw new Error('zotero request-token failed: ' + JSON.stringify(reqTok).slice(0, 160))
    const ot = reqTok.oauth_token, ots = reqTok.oauth_token_secret
    // (b) authorize (parity: library_access=1&all_groups=read)
    const authz = 'https://www.zotero.org/oauth/authorize?oauth_token=' + encodeURIComponent(ot) +
      '&library_access=1&all_groups=read'
    console.log(`  request-token ok (${mask(ot)}\u2026) \u2014 browser opens zotero login/approval`)
    if (DRY) { continue }
    const ctx = await browser.newContext(); contexts.push(ctx)
    const page = await ctx.newPage()
    const cbHint = /^(https?:)?\/\/[^/]+\/user\/zotero\/oauth\/callback/i
    const u = await captureCallback(page, authz, cbHint)
    const verifier = u.searchParams.get('oauth_verifier')
    if (!verifier) throw new Error('no oauth_verifier in callback: ' + u.search)
    console.log('  \u2713 verifier captured \u2014 exchanging for the LIFETIME api key')
    // (c) exchange  POST https://www.zotero.org/oauth/access
    const ex = await oauth1Call('POST', 'https://www.zotero.org/oauth/access',
      cfg.clientKey, cfg.clientSecret, u.searchParams.get('oauth_token') || ot, ots, { oauth_verifier: verifier })
    const apiKey = ex.oauth_token
    if (!apiKey) throw new Error('zotero exchange failed: ' + JSON.stringify(ex).slice(0, 160))
    saveAuth('zotero', apiKey, {
      apiKeySecret: ex.oauth_token_secret || null,
      userId: ex.userID || null, username: ex.username || null,
      clientKey: cfg.clientKey, clientSecret: cfg.clientSecret,
    })
    // persistent key into config \u00a7zotero \u2192 external-probe zotero leg re-arms
    const c2 = JSON.parse(fs.readFileSync(path.join(process.cwd(), 'credentials/config.json'), 'utf8'))
    if (ex.userID) c2.zotero.userId = ex.userID
    if (ex.oauth_token_secret) c2.zotero.apiKeySecret = ex.oauth_token_secret
    if (ex.username) c2.zotero.username = ex.username
    c2.zotero.apiKey = apiKey
    c2.zotero._capture = 'captured ' + new Date().toISOString() + ' via scripts/capture-interactive-tokens.mjs'
    fs.writeFileSync(path.join(process.cwd(), 'credentials/config.json'), JSON.stringify(c2, null, 2) + '\n', { mode: 0o600 })
    console.log(`  \u2713 Zotero LIFETIME apiKey = ${mask(apiKey)} (userId ${ex.userID || '?'}, user ${ex.username || '?'}) \u2192 config \u00a7zotero + .auth \u2014 external-probe zotero leg re-arms`)
  }
}

for (const c of contexts) await c.close().catch(() => {})
if (browser) await browser.close().catch(() => {})
console.log('\ncapture complete — specs reading credentials/.auth and §zotero keys will use these; masked values only, nothing printed in full.')
