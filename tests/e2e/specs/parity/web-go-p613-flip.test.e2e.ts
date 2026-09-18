/**
 * P6.13 flip gate — template gallery module surface (Node → OlliTeX Go web).
 *
 * 4-leg contract-parity gate (same harness family as P6.5…P6.12):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a … P6.13) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Pinned Node behaviors (oracle, live-captured 2026-09-18, /tmp/p613_oracle3.mjs):
 *  - global chain: anon GET+accept-json → 401 text/plain "Unauthorized";
 *    anon GET bare → 302 /login; anon non-GET → 403 "Forbidden".
 *  - logged-in non-privileged mgmt → 403 {"message":"restricted"} (json) /
 *    the rendered restricted view (html).
 *  - gallery reads: /api/templates (totalSize + templates[]), by/order
 *    validation (by ∉ {lastUpdated,name} or order ∉ {asc,desc} or repeated
 *    key → the rendered 500 page); /api/template?key=… single item (miss →
 *    OError → 500 page); /api/template/categories → array.
 *  - bundle: /template/:id/bundle → 200 zip (status/content-type/
 *    content-disposition pinned; the zip BYTES are re-compressed per stack —
 *    semantic parity of template.json/source.zip/output.pdf was verified
 *    entry-wise out-of-band, both stacks proxy the same filestore bytes).
 *    ghost id → 500; bad hex → 500 `Cast to ObjectId failed for value "xyz"…`.
 *  - import/import-url: {} → 400; import-url SSRF (127.0.0.1) → 422;
 *    bad scheme → 422. (Node CRASHES on invalid base64 import — a pinned
 *    Node bug; deliberately NOT exercised through the Node leg.)
 *  - edit {} → 200 {"lastUpdated":…} (timestamp normalized); ghost/bad-hex
 *    edit → 500; delete ghost → 200; new/:ghost → 400 (project missing).
 *  - legacy 301s: /template/:id, /templates*, /templates/manage → hub leaves.
 *  - /templates/a/b → the rendered 404 page (admin nav + per-user
 *    ExposedSettings.canManageTemplatesMenu — both stacks render the
 *    same page, parity-verified at ratio 1.0 before flipping).
 *
 * Declared routes covered (services/web/modules/template-gallery, 16):
 *   GET    /api/template
 *   GET    /api/template/categories
 *   GET    /api/templates
 *   GET    /api/templates/admin-list
 *   POST   /template/new/:id
 *   POST   /template/bundle/import
 *   POST   /template/bundle/import-url
 *   GET    /template/:id
 *   GET    /template/:id/bundle
 *   POST   /template/:id/edit
 *   DELETE /template/:id/delete
 *   GET    /template/:id/preview
 *   GET    /templates
 *   GET    /templates/
 *   GET    /templates/manage
 *   GET    /templates/:category
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync, readFileSync, existsSync } from 'node:fs'

const LEG1_PATH = '/tmp/web-go-p613-leg1.json'

const overleafC = 'ol-e2e-overleaf-1'
// Cumulative through P6.13.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf', 'web-p611.conf', 'web-p612.conf', 'web-p613.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const UA = 'p613-gate'
const TPL = '6aa4b8d673ef0e5094f4cc2b' // e2e fixture template (Parity Fixture Template)
const GHOST = '6aa4b8d673ef0e5094f4ccee' // valid 24-hex, no template (delete 200 / edit 500)

type Leg = Record<string, { status: number; ct: string; loc: string; body: string }>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

// curl exit≠0 (drain window) → the status rides on e.stdout.
function probe(url: string): string {
  let out = ''
  try {
    out = execFileSync('docker', ['exec', overleafC, 'sh', '-c', `curl -s -m 3 -o /dev/null -w "%{http_code}" ${url}`], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    })
  } catch (e: any) {
    out = e && e.stdout ? e.stdout.toString() : ''
  }
  return (out || '').trim().split('\n')[0]
}

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    if (probe('http://127.0.0.1:4010/status') === '200') return
    await sleep(500)
  }
  throw new Error('Go :4010 never came up')
}

function flipCount(conf: string): number {
  return Number(dexeStrict(overleafC, `sh -c 'grep -c "overleaf-flips/${conf}" /etc/nginx/sites-enabled/overleaf.conf || true'`).trim()) || 0
}

async function flip(mode: 'apply' | 'strip'): Promise<void> {
  if (mode === 'strip') {
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`)
    dexeStrict(overleafC, 'nginx -t && nginx -s reload')
    const r: any = await fetch(BASE + '/status', { headers: { 'user-agent': UA } })
    if (r.status !== 200) throw new Error('strip failed (status not 200)')
    await sleep(1200)
    for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
    return
  }
  dexeStrict(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && mkdir -p /etc/nginx/overleaf-flips')
  for (const conf of FLIPCONFS) {
    execFileSync('docker', ['cp', `${FLIPSRC}/${conf}`, `${overleafC}:/usr/local/share/overleaf-flips/${conf}`], { timeout: 30000 })
    dexeStrict(overleafC, `cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf}`)
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${conf};'+String.fromCharCode(10);if(!s.includes('overleaf-flips/${conf}')){if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)}else{throw new Error('anchor not found')}};fs.writeFileSync(p,s)"`)
  }
  dexeStrict(overleafC, 'nginx -t && nginx -s reload')
  await sleep(1200)
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(1)
}

function normBody(b: string): string {
  return b
    .replace(/overleaf\.sid=s%3A[^;,\s"\\]+/g, 'overleaf.sid=SID.')
    .replace(/"sid":"[^"]*"/g, '"sid":"SID"')
    .replace(/Expires=[^;,\s]+/g, 'Expires=EX')
    .replace(
      /\b(20\d{2}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\b/g,
      'TS',
    )
    .replace(/nonce-[A-Za-z0-9+/=~]{16,}/g, 'nonce-NONCE')
    .replace(/nonce="[^"]+"/g, 'nonce="N"')
    .replace(/<meta name="ol-csrfToken" content="[^"]*"/g, '<meta name="ol-csrfToken" content="CSRF"')
    .replace(/"ol-csrfToken":"[^"]*"/g, '"ol-csrfToken":"CSRF"')
    .replace(/<input name="_csrf" type="hidden" value="[^"]*"/g, '<input name="_csrf" type="hidden" value="CSRF"')
    // P6.13: the admin gate battery's no-op edit bumps template `version` once
    // per leg (both stacks, +1 each) — normalize the counter.
    .replace(/"version":"\d+"/g, '"version":"V"')
    .replace(/v\d+(?=\.bundle\.zip)/g, 'vNNN')
}

function diffLegs(label: string, A: Leg, B: Leg): string[] {
  const ds: string[] = []
  const ks = [...new Set([...Object.keys(A), ...Object.keys(B)])].sort()
  for (const k of ks) {
    const a = A[k]
    const b = B[k]
    if (!a && !b) continue
    if (!a || !b) {
      ds.push(`${label}:${k} present-on-one-side`)
      continue
    }
    if (a.status !== b.status) ds.push(`${label}:${k} status A=${a.status} B=${b.status}`)
    if (a.ct !== b.ct) ds.push(`${label}:${k} ct A='${a.ct}' B='${b.ct}'`)
    if (a.loc !== b.loc) ds.push(`${label}:${k} loc A='${a.loc}' B='${b.loc}'`)
    const an = normBody(a.body)
    const bn = normBody(b.body)
    if (an !== bn) {
      ds.push(`${label}:${k} body A~${an.slice(0, 140)} || B~${bn.slice(0, 140)}`)
    }
  }
  return ds
}

async function login(email: string, pw: string): Promise<{ ck: string; tok: string }> {
  let lastErr: any = null
  for (let attempt = 0; attempt < 8; attempt++) {
    try {
      const r0: any = await fetch(BASE + '/login', { headers: { 'user-agent': UA }, redirect: 'manual' })
      const html = await r0.text()
      const csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
      const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
      const r1: any = await fetch(BASE + '/login', {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0, 'user-agent': UA },
        body: JSON.stringify({ email, password: pw }),
        redirect: 'manual',
      })
      const body1 = await r1.text()
      if (r1.status === 502 || r1.status === 503 || r1.status === 504) {
        lastErr = new Error('drain window ' + r1.status)
        await sleep(1500)
        continue
      }
      if (r1.status !== 200) throw new Error('login failed ' + r1.status + ' ' + body1.slice(0, 200))
      const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
      const cs: any = await fetch(BASE + '/dev/csrf', { headers: { cookie: ck, 'user-agent': UA } })
      const tok = (await cs.text()).trim()
      return { ck, tok }
    } catch (e: any) {
      lastErr = e
      await sleep(1500)
    }
  }
  if (lastErr instanceof Error) throw lastErr
  throw new Error('login failed (retries exhausted)')
}

let leg1: Leg | null = null

function leg1Load(): Leg {
  if (leg1) return leg1
  if (existsSync(LEG1_PATH)) {
    return JSON.parse(readFileSync(LEG1_PATH, 'utf8')) as Leg
  }
  throw new Error('leg1 baseline missing (leg 1 did not record it)')
}

async function runLeg(): Promise<Leg> {
  const U = await login(USER.email, USER.password)
  const AD = await login(ADMIN.email, ADMIN.password)
  const UH = { cookie: U.ck, 'x-csrf-token': U.tok, 'user-agent': UA, accept: 'application/json' }
  const UH_NOCSRF = { cookie: U.ck, 'user-agent': UA, accept: 'application/json' }
  const AH = { cookie: AD.ck, 'x-csrf-token': AD.tok, 'user-agent': UA, accept: 'application/json' }
  const AN = { 'user-agent': UA, accept: 'application/json' } // anon GET json
  const ANB = { 'user-agent': UA } // anon GET bare

  async function call(init: {
    path: string
    method?: string
    headers?: Record<string, string>
    json?: unknown
    raw?: boolean
  }): Promise<{ status: number; ct: string; loc: string; body: string }> {
    let body: string | undefined
    const headers = { ...(init.headers || {}) }
    if (init.json !== undefined) {
      body = JSON.stringify(init.json)
      headers['content-type'] = headers['content-type'] || 'application/json'
    }
    const r: any = await fetch(BASE + init.path, { method: init.method || 'GET', headers, body, redirect: 'manual' })
    const text = await r.text()
    if (init.raw) {
      // zip/binary: re-compressed per stack; pin the shape, not the bytes
      // (entry-wise semantic parity verified out-of-band — same filestore
      // payloads on both stacks).
      return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', body: 'BINARY ' + (r.headers.get('content-disposition') || '').replace(/v\d+/g, 'vNNN') }
    }
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', body: text }
  }

  const pins: Leg = {}

  // ---- anon (global chain) ---------------------------------------------------
  pins.a_list_json = await call({ path: '/api/templates', headers: AN })
  pins.a_list_bare = await call({ path: '/api/templates', headers: ANB })
  pins.a_templates_bare = await call({ path: '/templates', headers: ANB })
  pins.a_adminlist_post = await call({ path: '/api/templates/admin-list', method: 'POST', headers: { 'user-agent': UA }, json: {} })
  pins.a_bundle_bare = await call({ path: `/template/${TPL}/bundle`, headers: ANB })
  pins.a_import_post = await call({ path: '/template/bundle/import', method: 'POST', headers: { 'user-agent': UA }, json: {} })
  pins.a_new_post = await call({ path: `/template/new/${TPL}`, method: 'POST', headers: { 'user-agent': UA }, json: {} })

  // ---- non-privileged user (mgmt 403 chain) ---------------------------------
  pins.u_adminlist_json = await call({ path: '/api/templates/admin-list', headers: UH })
  pins.u_adminlist_html = await call({ path: '/api/templates/admin-list', headers: { cookie: U.ck, 'user-agent': UA } })
  pins.u_import = await call({ path: '/template/bundle/import', method: 'POST', headers: UH, json: {} })
  pins.u_importurl = await call({ path: '/template/bundle/import-url', method: 'POST', headers: UH, json: {} })
  pins.u_edit = await call({ path: `/template/${TPL}/edit`, method: 'POST', headers: UH, json: {} })
  pins.u_delete = await call({ path: `/template/${TPL}/delete`, method: 'DELETE', headers: UH })
  pins.u_new = await call({ path: `/template/new/${TPL}`, method: 'POST', headers: UH, json: {} })

  // ---- admin: gallery reads ---------------------------------------------------
  pins.g_list = await call({ path: '/api/templates', headers: AH })
  pins.g_list_byname = await call({ path: '/api/templates?by=name&order=asc', headers: AH })
  pins.g_list_cat = await call({ path: '/api/templates?category=academic-journal', headers: AH })
  pins.g_list_evil_by = await call({ path: '/api/templates?by=evil', headers: AH })
  pins.g_list_evil_order = await call({ path: '/api/templates?order=evil', headers: AH })
  pins.g_list_repeat_by = await call({ path: '/api/templates?by=a&by=b', headers: AH })
  pins.g_item = await call({ path: `/api/template?key=_id&val=${TPL}`, headers: AH })
  pins.g_item_miss = await call({ path: '/api/template?key=name&val=Nope-Not-Here-P613', headers: AH })
  pins.g_cats = await call({ path: '/api/template/categories', headers: AH })
  pins.g_bundle = await call({ path: `/template/${TPL}/bundle`, headers: { cookie: AD.ck, 'user-agent': UA }, raw: true })
  pins.g_bundle_ghost = await call({ path: `/template/${GHOST}/bundle`, headers: AH })
  pins.g_bundle_badhex = await call({ path: '/template/xyz/bundle', headers: AH })
  pins.g_preview = await call({ path: `/template/${TPL}/preview`, headers: AH })

  // ---- admin: legacy 301s + 404 page ------------------------------------------
  pins.l_redirect = await call({ path: `/template/${TPL}`, headers: AH })
  pins.l_manage = await call({ path: '/templates/manage', headers: AH })
  pins.l_tplroot = await call({ path: '/templates', headers: AH })
  pins.l_tpltrail = await call({ path: '/templates/', headers: AH })
  pins.l_tplcat = await call({ path: '/templates/academic-journal', headers: AH })
  pins.l_404_two = await call({ path: '/templates/a/b', headers: AH })

  // ---- admin: mgmt (validation / no-op states; no durable mutations) ----------
  pins.m_import_empty = await call({ path: '/template/bundle/import', method: 'POST', headers: AH, json: {} })
  pins.m_importurl_empty = await call({ path: '/template/bundle/import-url', method: 'POST', headers: AH, json: {} })
  pins.m_importurl_ssrf = await call({ path: '/template/bundle/import-url', method: 'POST', headers: AH, json: { url: 'http://127.0.0.1:27017/x.zip' } })
  pins.m_importurl_bad = await call({ path: '/template/bundle/import-url', method: 'POST', headers: AH, json: { url: 'ftp://example.com/x.zip' } })
  pins.m_edit_empty = await call({ path: `/template/${TPL}/edit`, method: 'POST', headers: AH, json: {} })
  pins.m_edit_ghost = await call({ path: `/template/${GHOST}/edit`, method: 'POST', headers: AH, json: { name: 'p613-gate-edit' } })
  pins.m_edit_badhex = await call({ path: '/template/xyz/edit', method: 'POST', headers: AH, json: {} })
  pins.m_delete_ghost = await call({ path: `/template/${GHOST}/delete`, method: 'DELETE', headers: AH })
  pins.m_new_ghost = await call({ path: `/template/new/${GHOST}`, method: 'POST', headers: AH, json: {} })

  // ---- logged-in user w/o csrf (mutation → csrf-reject chain) ------------------
  pins.t_import = await call({ path: '/template/bundle/import', method: 'POST', headers: UH_NOCSRF, json: {} })
  pins.t_edit = await call({ path: `/template/${TPL}/edit`, method: 'POST', headers: UH_NOCSRF, json: {} })

  // ---- nginx method-guard fall-through (→ Node answers on both legs) -------------
  pins.ft_get_import = await call({ path: '/template/bundle/import', headers: AH })
  pins.ft_post_bundle = await call({ path: `/template/${TPL}/bundle`, method: 'POST', headers: AH, json: {} })
  pins.ft_get_delete = await call({ path: `/template/${TPL}/delete`, headers: AH })
  pins.ft_post_tplroot = await call({ path: '/templates', method: 'POST', headers: AH, json: {} })

  return pins
}

test('leg 0: flip off before start (force-strip any leftovers)', async () => {
  try {
    await flip('strip')
  } catch {
    /* best effort */
  }
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
}, 60_000)

test('leg 1: Node baseline battery', async () => {
  dexeStrict(
    'ol-e2e-redis-1',
    'sh -c "redis-cli --scan --pattern \\"rate-limit:*\\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  leg1 = await runLeg()
  expect(Object.keys(leg1!).length).toBe(48)
  writeFileSync(LEG1_PATH, JSON.stringify(leg1)) // disk anchor (module state can be isolated per test)
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  dexeStrict(
    'ol-e2e-redis-1',
    'sh -c "redis-cli --scan --pattern \\"rate-limit:*\\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  const leg2 = await runLeg()
  const ds = diffLegs('go', leg1Load(), leg2)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 6000))
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  dexeStrict(
    'ol-e2e-redis-1',
    'sh -c "redis-cli --scan --pattern \\"rate-limit:*\\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  const leg3 = await runLeg()
  const ds = diffLegs('node-determinism', leg1Load(), leg3)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 6000))
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1Load()
  // anon (global chain)
  expect(L.a_list_json.status).toBe(401)
  expect(L.a_list_json.ct).toBe('text/plain')
  expect(L.a_list_json.body).toBe('Unauthorized')
  expect(L.a_list_bare.status).toBe(302)
  expect(L.a_list_bare.loc).toBe('/login')
  expect(L.a_templates_bare.status).toBe(302)
  expect(L.a_adminlist_post.status).toBe(403)
  // non-privileged mgmt chain
  expect(L.u_adminlist_json.status).toBe(403)
  expect(L.u_adminlist_json.body).toContain('restricted')
  expect(L.u_import.status).toBe(403)
  expect(L.u_edit.status).toBe(403)
  expect(L.u_delete.status).toBe(403)
  expect(L.u_new.status).toBe(403)
  // gallery reads
  expect(L.g_list.status).toBe(200)
  expect(L.g_list.body).toContain('"totalSize"')
  expect(L.g_list.body).toContain('Parity Fixture Template')
  expect(L.g_list_byname.status).toBe(200)
  expect(L.g_list_evil_by.status).toBe(500)
  expect(L.g_list_evil_by.ct).toBe('text/html')
  expect(L.g_list_evil_order.status).toBe(500)
  expect(L.g_item.status).toBe(200)
  expect(L.g_item.body).toContain(TPL)
  expect(L.g_cats.status).toBe(200)
  // bundle: zip shape (bytes re-compressed per stack — pinned out-of-band)
  expect(L.g_bundle.status).toBe(200)
  expect(L.g_bundle.ct).toBe('application/zip')
  expect(L.g_bundle.body).toContain('Parity_Fixture_Template_v')
  expect(L.g_bundle_ghost.status).toBe(500)
  expect(L.g_bundle_badhex.status).toBe(500)
  expect(L.g_bundle_badhex.body).toContain('Cast to ObjectId failed for value')
  // legacy 301s
  expect(L.l_redirect.status).toBe(301)
  expect(L.l_manage.status).toBe(301)
  expect(L.l_tplroot.status).toBe(301)
  expect(L.l_tpltrail.status).toBe(301)
  expect(L.l_tplcat.status).toBe(301)
  // 404 page (rendered)
  expect(L.l_404_two.status).toBe(404)
  expect(L.l_404_two.ct).toBe('text/html')
  // mgmt validation
  expect(L.m_import_empty.status).toBe(400)
  expect(L.m_importurl_empty.status).toBe(400)
  expect(L.m_importurl_ssrf.status).toBe(422)
  expect(L.m_edit_empty.status).toBe(200)
  expect(L.m_edit_empty.body).toContain('lastUpdated')
  expect(L.m_edit_ghost.status).toBe(500)
  expect(L.m_delete_ghost.status).toBe(200)
  expect(L.m_new_ghost.status).toBe(400)
  // csrf chain
  expect(L.t_import.status).toBe(403)
  expect(L.t_edit.status).toBe(403)
  // method-guard fall-throughs all answered by Node (pinned, identical both legs)
  expect(L.ft_get_import.status).not.toBe(200)
  expect(L.ft_post_bundle.status).not.toBe(200)
  expect(L.ft_get_delete.status).not.toBe(200)
  expect(L.ft_post_tplroot.status).not.toBe(200)
}, 20_000)
