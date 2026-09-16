/**
 * WEB-GO P6.4a FLIP GATE (WEB_GO_PLAN.md P6.4a — OlliTeX llm module,
 * deterministic settings surface):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p64a.conf):
 *     GET    /user/llm-providers
 *     POST   /user/llm-providers
 *     POST   /user/llm-providers/check
 *     POST   /user/llm-providers/scan
 *     POST   /user/llm-providers/:id
 *     POST   /user/llm-providers/:id/delete
 *     GET|POST /user/llm/selected-model
 *     GET|POST /user/llm/compliance
 *     GET    /user/llm-usage
 *     GET|POST /user/llm-settings/grammar
 *     GET    /user/llm-settings               (301 -> hub)
 *     GET    /admin/llm/settings              (301 -> hub)
 *     GET    /admin/llm/settings/json
 *     POST   /admin/llm/settings
 *     POST   /admin/llm/settings/check
 *     POST   /admin/llm/models
 *     GET    /admin/llm/usage
 *
 *   P6.4b (project-scoped /project/:id/llm/* — live LLM calls) stays on
 *   Node and is intentionally NOT flipped.
 *
 *   leg 1  Node baseline       (p64a flip OFF)
 *   leg 2  FLIP ON — Go        (flips ON, battery, flip OFF)
 *   leg 3  Node re-baseline    (flip OFF)
 *
 *   Battery mirrors the Node oracle (/tmp/p64a_oracle.mjs, 57 pins,
 *   live 2026-09-16): anon matrix, BYO row lifecycle (add/dedupe,
 *   update merge semantics, clearApiKey, not-found, delete lifecycle),
 *   check/scan (SSRF-blocked + refused URLs, candidate-type retry order
 *   — last error wins), add validation (zod messages, bad JSON via HTML
 *   accept), selected-model battery (ref regex / length caps), compliance
 *   rubrics (sanitize/validate/caps), grammar modes (validate/degrade
 *   availability), usage summaries (days clamp, contiguous byDay), the
 *   two 301 page redirects, member/admin authz, admin save merge + bad
 *   input, admin check/scan failure shapes, admin usage — plus FX parity:
 *   user doc LLM fields (encrypted keys normalized), the admin settings
 *   FILE bytes (Node JSON.stringify(data,null,2) equivalence), llmusages
 *   counts.
 *
 *   Volatile normalization before diff: enc:v1 blobs -> ENC, 8-hex
 *   provider row ids -> PID, ISO dates -> TS, duration -> DUR, network
 *   failure reason after '(' -> (ERR).
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import fs from 'node:fs'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const FLIPCONF = 'web-p64a.conf'
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const ADMINFILE = '/var/lib/overleaf/data/llm-admin-settings.json'
// pristine admin-settings fixture (repo-tracked; /tmp fallback for ad-hoc runs)
const ADMINBAK_REPO = path.resolve(process.cwd(), 'fixtures/p64a-admin-settings.bak')
const ADMINBAK = fs.existsSync(ADMINBAK_REPO) ? ADMINBAK_REPO : '/tmp/p64a_adminfile.bak'
const UA = 'p64a-gate'

type Leg = Record<string, { status: number; ct: string; loc?: string | null; text: string }>
type FX = Record<string, any>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function msh(cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', mongoC, 'mongosh', 'mongodb://127.0.0.1:27017/sharelatex', '--quiet', '--eval', cmd], {
      encoding: 'utf8',
      stdio: capture ? 'pipe' : 'ignore',
      maxBuffer: 64 * 1024 * 1024,
    })
    return out || ''
  } catch (e: any) {
    if (capture) throw e
    return (e.stdout ? e.stdout.toString() : '') || (e.stderr ? e.stderr.toString() : '') || ''
  }
}
function dexe(c: string, cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore', maxBuffer: 64 * 1024 * 1024 })
    return out || ''
  } catch (e: any) {
    if (capture) throw e
    return (e.stdout ? e.stdout.toString() : '') || ''
  }
}
function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    if (dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim() === '200') return
    await sleep(500)
  }
  throw new Error('Go shadow /status not 200')
}

// ---------- HTTP (oracle call() semantics) ----------
async function call(
  init: { method?: string; path: string; headers?: Record<string, string>; cookie?: string },
  body?: any,
): Promise<{ status: number; ct: string; loc: string | null; text: string; json: any }> {
  const method = init.method || 'GET'
  const headers: Record<string, string> = { ...(init.headers || {}) }
  if (init.cookie) headers['cookie'] = init.cookie
  let payload: string | undefined
  if (method !== 'GET') {
    headers['content-type'] = 'application/json'
    payload = body === undefined ? '{}' : typeof body === 'string' ? body : JSON.stringify(body)
  }
  let lastErr: unknown = null
  for (let attempt = 0; attempt < 5; attempt++) {
    try {
      const r = await fetch(BASE + init.path, { method, headers, body: payload, redirect: 'manual' })
      const text = await r.text()
      let json: any = null
      try { json = JSON.parse(text) } catch { /* not json */ }
      return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location'), text, json }
    } catch (e) {
      // nginx worker mid-reload can drop a fresh socket; retry with a backoff.
      lastErr = e
      await sleep(500 * (attempt + 1))
    }
  }
  throw lastErr
}

async function login(acct: { email: string; password: string }): Promise<{ ck: string; csrf: string }> {
  const r0: any = await new Promise((res, rej) => {
    fetch(BASE + '/login', { headers: { 'user-agent': UA }, redirect: 'manual' }).then(res, rej)
  })
  const html: string = await r0.text()
  const csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  if (!csrf) throw new Error('no csrf meta')
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1: any = await new Promise((res, rej) => {
    fetch(BASE + '/login', {
      method: 'POST',
      headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0, 'user-agent': UA },
      body: JSON.stringify({ email: acct.email, password: acct.password }),
      redirect: 'manual',
    }).then(res, rej)
  })
  const body1 = await r1.text()
  if (r1.status !== 200) throw new Error('login failed ' + r1.status + ' ' + body1.slice(0, 200))
  const ck = (r1.headers.get('set-cookie') || '').split(';')[0]
  const cs: any = await new Promise((res, rej) => {
    fetch(BASE + '/dev/csrf', { headers: { cookie: ck, 'user-agent': UA } }).then(res, rej)
  })
  const tok: string = (await cs.text()).trim()
  if (!tok) throw new Error('no /dev/csrf token')
  return { ck, csrf: tok }
}

const withCsrf = (a: { ck: string; csrf: string }) => ({ cookie: a.ck, headers: { 'user-agent': UA, 'x-csrf-token': a.csrf } })
const anonH = (a: { ck: string }) => ({ cookie: a.ck, headers: { 'user-agent': UA } })

function verifyInclude(n: number): void {
  const cnt = dexeStrict(overleafC, `sh -c 'grep -c "overleaf-flips/${FLIPCONF}" /etc/nginx/sites-enabled/overleaf.conf || true'`).trim()
  if (cnt !== String(n)) throw new Error(`flip include expected ${n}, found ${cnt}`)
}
function flip(mode: 'apply' | 'strip'): void {
  if (mode === 'strip') {
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`)
    dexeStrict(overleafC, 'nginx -t')
    dexe(overleafC, 'nginx -s reload')
    verifyInclude(0)
    return
  }
  dexeStrict(overleafC, `mkdir -p /etc/nginx/overleaf-flips && cp -f /usr/local/share/overleaf-flips/${FLIPCONF} /etc/nginx/overleaf-flips/${FLIPCONF}`)
  dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${FLIPCONF};'+String.fromCharCode(10);if(!s.includes('overleaf-flips/${FLIPCONF}')){if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)}else{throw new Error('anchor not found')}};fs.writeFileSync(p,s)"`)
  dexeStrict(overleafC, 'nginx -t')
  dexe(overleafC, 'nginx -s reload')
  verifyInclude(1)
}

// ---------- volatile normalization ----------
const norm = (s: string): string =>
  s
    .replace(/enc:v1:[A-Za-z0-9+/=:.]+/g, 'ENC')
    .replace(/"(?:id)":\"[0-9a-f]{8}\"/g, '"id":"PID"')
    .replace(/u:[0-9a-f]{8}:/g, 'u:PID:')
    .replace(/"createdAt":"[^"]*"/g, '"createdAt":"TS"')
    .replace(/"duration":"[0-9]+ms"/g, '"duration":"DUR"')
    .replace(/\((?:fetch failed|dial tcp [^)]*|get [^)]*)\)/g, '(ERR)')
    .replace(/20\d{2}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/g, 'TS')
    .replace(/20\d{2}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z/g, 'TS')

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
    if ((a.loc || '') !== (b.loc || '')) ds.push(`${label}:${k} loc A='${a.loc}' B='${b.loc}'`)
    const na = norm(a.text)
    const nb = norm(b.text)
    if (na !== nb) {
      const i = na.findIndex((c, i2) => c !== nb[i2])
      ds.push(`${label}:${k} body@${i} | A~${na.slice(Math.max(0, i - 60), i + 140).replace(/\n/g, ' ')} || B~${nb.slice(Math.max(0, i - 60), i + 140).replace(/\n/g, ' ')}`)
    }
  }
  return ds
}

function diffFx(label: string, A: FX, B: FX): string[] {
  const ds: string[] = []
  const ks = [...new Set([...Object.keys(A), ...Object.keys(B)])].sort()
  for (const k of ks) {
    const a = JSON.stringify(norm(JSON.stringify(A[k] ?? null)))
    const b = JSON.stringify(norm(JSON.stringify(B[k] ?? null)))
    if (a !== b) ds.push(`${label}:fx ${k}\n  A=${a.slice(0, 400)}\n  B=${b.slice(0, 400)}`)
  }
  return ds
}

// ---------- state (mongo + admin file) ----------
const LEGACY_FIELDS = ['llmApiUrl', 'llmApiType', 'llmApiKey', 'llmModelName', 'llmModels', 'llmModelNames', 'llmCompletionModel', 'llmCompletionModels', 'useOwnLLMSettings']
function resetUserLlm(email: string): void {
  const uid = msh(`var u = db.users.findOne({email: ${JSON.stringify(email)}}); print(u._id.toHexString())`, true).trim().split('\n').pop()
  const unsets = LEGACY_FIELDS.concat(['llmProviders', 'llmSelectedModel', 'llmComplianceRubrics', 'grammar'])
  const jsObj = Object.fromEntries(unsets.map((k) => [k, 1]))
  msh(`db.users.updateOne({_id: new ObjectId(${JSON.stringify(uid)})}, {$unset: ${JSON.stringify(jsObj)}})`)
}
function adminFileReset(): void {
  execFileSync('docker', ['cp', ADMINBAK, `${overleafC}:${ADMINFILE}`], { timeout: 30000 })
  dexeStrict(overleafC, `chown www-data:www-data ${ADMINFILE} && chmod 600 ${ADMINFILE}`)
}
function adminFileText(): string {
  return dexe(overleafC, `cat ${ADMINFILE}`)
}
function userLLMProfile(email: string): any {
  const out = msh(`var u = db.users.findOne({email: ${JSON.stringify(email)}}); print(JSON.stringify({llmProviders: u.llmProviders, llmSelectedModel: u.llmSelectedModel, llmComplianceRubrics: u.llmComplianceRubrics, grammar: u.grammar}))`, true).trim().split('\n').pop() || '{}'
  try { return JSON.parse(out) } catch { return { raw: out } }
}
function usagesCount(): number {
  return Number(msh(`print(db.llmusages.countDocuments({}))`, true).trim().split('\n').pop()) || 0
}
function userIdHex(email: string): string {
  return msh(`var u = db.users.findOne({email: ${JSON.stringify(email)}}); print(u._id.toHexString())`, true).trim().split('\n').pop() || ''
}

// ---------- battery (oracle order) ----------
async function runLeg(tag: string): Promise<{ pins: Leg; fx: FX }> {
  await waitGo()
  adminFileReset()
  resetUserLlm(USER.email)
  const usagesBefore = usagesCount()

  const pins: Leg = {}

  // ---- 1. anon ----
  pins.anon_byo_get = await call({ path: '/user/llm-providers' })
  pins.anon_admin_json = await call({ path: '/admin/llm/settings/json' })

  const MU = await login(USER)
  const MA = await login(ADMIN)
  const MH = withCsrf(MU)
  const AA = anonH(MA)
  const MAH = withCsrf(MA)

  // ---- 2. BYO providers ----
  pins.u_byo_get_0 = await call({ path: '/user/llm-providers', ...MH })
  const add1 = await call({ ...MH, path: '/user/llm-providers', method: 'POST' },
    { name: 'P64a GW', providerType: 'openaiCompatible', baseUrl: 'https://llm-gw.e2e.test', apiKey: 'sk-test-123', models: ['gpt-x', 'gpt-y', 'gpt-x'], completionModel: 'gpt-x', enabled: true })
  pins.u_byo_add = add1
  const row1Id: string = add1.json?.provider?.id || ''
  const add2 = await call({ ...MH, path: '/user/llm-providers', method: 'POST' },
    { name: 'Second', providerType: 'anthropic', baseUrl: 'https://api.anthropic.com', apiKey: 'sk-ant-7', models: ['opus-1'], enabled: false })
  pins.u_byo_add2 = add2
  const row2Id: string = add2.json?.provider?.id || ''
  pins.u_byo_list = await call({ path: '/user/llm-providers', ...MH })
  pins.u_byo_update = await call({ ...MH, path: `/user/llm-providers/${row1Id}`, method: 'POST' },
    { models: ['gpt-x', 'gpt-z'], completionModel: 'gpt-z', enabled: false })
  pins.u_byo_update_bad = await call({ ...MH, path: `/user/llm-providers/${row1Id}`, method: 'POST' },
    { models: ['solo-1'], enabled: true })
  pins.u_byo_clearkey = await call({ ...MH, path: `/user/llm-providers/${row1Id}`, method: 'POST' }, { clearApiKey: true })
  pins.u_byo_setkey2 = await call({ ...MH, path: `/user/llm-providers/${row1Id}`, method: 'POST' }, { apiKey: 'sk-plain-legacy', completionModel: '' })
  pins.u_byo_enable = await call({ ...MH, path: `/user/llm-providers/${row1Id}`, method: 'POST' }, { enabled: true })
  pins.u_byo_notfound = await call({ ...MH, path: '/user/llm-providers/doesnotexist', method: 'POST' }, { models: ['x'] })
  pins.u_byo_delete = await call({ ...MH, path: `/user/llm-providers/${row2Id}/delete`, method: 'POST' }, {})
  pins.u_byo_delete_again = await call({ ...MH, path: `/user/llm-providers/${row2Id}/delete`, method: 'POST' }, {})
  pins.u_byo_delete_ghost = await call({ ...MH, path: '/user/llm-providers/cfedead/delete', method: 'POST' }, {})

  // ---- 3. check / scan (deterministic blocked/refused URLs) ----
  pins.u_check_missing = await call({ ...MH, path: '/user/llm-providers/check', method: 'POST' }, { apiType: 'openaiCompatible', apiKey: 'k' })
  pins.u_check_badurl = await call({ ...MH, path: '/user/llm-providers/check', method: 'POST' }, { baseUrl: 'ftp://x.invalid', providerType: 'openaiCompatible', apiKey: 'k', model: 'm' })
  pins.u_check_ssrf = await call({ ...MH, path: '/user/llm-providers/check', method: 'POST' }, { baseUrl: 'http://127.0.0.1:9', providerType: 'openaiCompatible', apiKey: 'k', model: 'm' })
  pins.u_check_ssrf6 = await call({ ...MH, path: '/user/llm-providers/check', method: 'POST' }, { baseUrl: 'http://[::1]/v1', providerType: 'openaiCompatible', apiKey: 'k', model: 'm' })
  // row-based check: row1 baseUrl is public (.test TLD => NXDOMAIN for both
  // stacks); the retry loop's LAST error (anthropic candidate) surfaces —
  // normalized by gate.
  pins.u_check_ssrfrow = await call({ ...MH, path: '/user/llm-providers/check', method: 'POST' }, { rowId: row1Id })
  pins.u_scan_nourl = await call({ ...MH, path: '/user/llm-providers/scan', method: 'POST' }, { apiKey: 'k' })
  pins.u_scan_ssrf = await call({ ...MH, path: '/user/llm-providers/scan', method: 'POST' }, { baseUrl: 'http://169.254.169.254', apiKey: 'k' })

  // ---- 4. add validation ----
  pins.u_add_badtype = await call({ ...MH, path: '/user/llm-providers', method: 'POST' }, { providerType: 'bogus', baseUrl: 'https://x.test', models: ['m'] })
  pins.u_add_notjson = await call({ ...MH, path: '/user/llm-providers', method: 'POST' }, 'not-json')
  // gate-extra: same bad body with accept: application/json -> Node returns 400 {} (content negotiation).
  const mj = await call({ cookie: MU.ck, headers: { 'user-agent': UA, 'x-csrf-token': MU.csrf, accept: 'application/json' }, path: '/user/llm-providers', method: 'POST' }, 'not-json')
  pins.u_add_notjson_jsonaccept = mj

  // ---- 5. selected model (body field: `selected`) ----
  pins.u_sel_get = await call({ path: '/user/llm/selected-model', ...MH })
  pins.u_sel_save = await call({ ...MH, path: '/user/llm/selected-model', method: 'POST' }, { selected: 'gpt-4o-mini' })
  pins.u_sel_chat_compat = await call({ ...MH, path: '/user/llm/selected-model', method: 'POST' }, { selected: `u:${row1Id}:gpt-z` })
  pins.u_sel_bad = await call({ ...MH, path: '/user/llm/selected-model', method: 'POST' }, { selected: '!!!bad!!!' })
  pins.u_sel_toolong = await call({ ...MH, path: '/user/llm/selected-model', method: 'POST' }, { selected: 'a'.repeat(501) })
  pins.u_sel_reset = await call({ ...MH, path: '/user/llm/selected-model', method: 'POST' }, { selected: '' })

  // ---- 6. compliance rubrics ----
  pins.u_comp_get = await call({ path: '/user/llm/compliance', ...MH })
  pins.u_comp_save = await call({ ...MH, path: '/user/llm/compliance', method: 'POST' },
    { rubrics: [
      { id: 'r1', name: 'No Wikipedia', guidelines: 'Citations must not point at Wikipedia.', scanPatterns: 'WIKI :: wikipedia\\.org' },
      { name: 'nameless-dropped' },
      { id: 'r1', name: 'dup-id-kept' },
    ] })
  pins.u_comp_save_cap = await call({ ...MH, path: '/user/llm/compliance', method: 'POST' },
    { rubrics: [{ id: 'r2', name: 'overcap', scanPatterns: 's' + 't'.repeat(4500) }] })
  pins.u_comp_save_badregex = await call({ ...MH, path: '/user/llm/compliance', method: 'POST' },
    { rubrics: [{ id: 'r9', name: 'bad', scanPatterns: 'X :: (unclosed' }] })
  pins.u_comp_get2 = await call({ path: '/user/llm/compliance', ...MH })

  // ---- 7. grammar ----
  pins.u_gr_get = await call({ path: '/user/llm-settings/grammar', ...MH })
  pins.u_gr_save = await call({ ...MH, path: '/user/llm-settings/grammar', method: 'POST' },
    { mode: 'lt', llmModel: '', language: 'de', blockedRules: ['en.ENG_SPELLER', ' ', 'en.ENG_SPELLER'] })
  pins.u_gr_save_ltllm = await call({ ...MH, path: '/user/llm-settings/grammar', method: 'POST' }, { mode: 'lt+llm' })
  pins.u_gr_save_llm = await call({ ...MH, path: '/user/llm-settings/grammar', method: 'POST' }, { mode: 'llm+lt' })
  pins.u_gr_badmode = await call({ ...MH, path: '/user/llm-settings/grammar', method: 'POST' }, { mode: 'bogus' })
  pins.u_gr_baddllm = await call({ ...MH, path: '/user/llm-settings/grammar', method: 'POST' }, { llmModel: 'u:abc:!!!!' })
  pins.u_gr_baddlang = await call({ ...MH, path: '/user/llm-settings/grammar', method: 'POST' }, { language: 'z'.repeat(65) })
  pins.u_gr_get2 = await call({ path: '/user/llm-settings/grammar', ...MH })

  // ---- 8. usage ----
  pins.u_usage = await call({ path: '/user/llm-usage?days=0', ...MH })
  pins.u_usage_d2 = await call({ path: '/user/llm-usage?days=2', ...MH })

  // ---- 9. user page redirect ----
  pins.u_page_301 = await call({ path: '/user/llm-settings', cookie: MU.ck, headers: { 'user-agent': UA } })

  // ---- 10. admin (A) + member denial (M) ----
  pins.an_json_before = await call({ path: '/admin/llm/settings/json', ...AA })
  pins.an_page_301 = await call({ path: '/admin/llm/settings', cookie: MA.ck, headers: { 'user-agent': UA } })
  pins.member_an_json = await call({ path: '/admin/llm/settings/json', ...MH })
  pins.member_an_save = await call({ ...MH, path: '/admin/llm/settings', method: 'POST' }, {})

  pins.an_save = await call({ ...MAH, path: '/admin/llm/settings', method: 'POST' }, {
    systemPrompt: 'p64a-sp',
    llmApiUrl: 'https://ol-llm.e2e.test/v1/',
    llmApiType: 'openaiCompatible',
    llmApiKey: ' sk-admin-9 ',
    allowedModels: ['m1', 'm2'],
    knownModels: ['m1', 'm2', 'm3'],
    completionModel: 'm1',
    chatEnabled: false,
    maxContextTokens: 5000,
    reviewMaxTokens: 900,
    languageToolUrl: '',
    languageToolDisabledByAdmin: true,
  })
  pins.an_json_after = await call({ path: '/admin/llm/settings/json', ...AA })
  // admin file bytes after save (FX — Node JSON.stringify(data,null,2) parity)
  const adminFileAfter = adminFileText()

  pins.an_check_nourl = await (async () => {
    await call({ ...MAH, path: '/admin/llm/settings', method: 'POST' }, { llmApiUrl: '' })
    return call({ ...MAH, path: '/admin/llm/settings/check', method: 'POST' }, {})
  })()
  pins.an_check_127 = await call({ ...MAH, path: '/admin/llm/settings/check', method: 'POST' },
    { apiUrl: 'http://127.0.0.1:9', apiKey: 'k', apiType: 'openaiCompatible' })
  pins.an_scan_127 = await call({ ...MAH, path: '/admin/llm/models', method: 'POST' },
    { apiUrl: 'http://127.0.0.1:9', apiKey: 'k' })

  pins.an_save_bad = await call({ ...MAH, path: '/admin/llm/settings', method: 'POST' }, { maxContextTokens: 'abc' })
  pins.an_usage_d3 = await call({ path: '/admin/llm/usage?days=3', ...AA })

  const fx: FX = {
    userId: userIdHex(USER.email),
    userLLM: userLLMProfile(USER.email),
    adminFileAfter: norm(adminFileAfter),
    llmusagesBefore: usagesBefore,
    llmusagesAfter: usagesCount(),
  }
  return { pins, fx }
}

// ---------- legs ----------
let leg1: { pins: Leg; fx: FX } | null = null
let leg2: { pins: Leg; fx: FX } | null = null

test.describe.configure({ mode: 'serial' })

test('leg 0: flip off before start', async () => {
  flip('strip')
})

test('leg 1: Node baseline', async () => {
  leg1 = await runLeg('leg1')
  expect(leg1).toBeTruthy()
})

test('leg 2: Go parity (flip on)', async () => {
  flip('apply')
  try {
    leg2 = await runLeg('leg2')
  } finally {
    flip('strip')
  }
  const d = [...diffLegs('p64a', leg1!.pins, leg2!.pins), ...diffFx('p64a', leg1!.fx, leg2!.fx)]
  expect(d).toEqual([])
})

test('leg 3: Node re-baseline', async () => {
  const leg3 = await runLeg('leg3')
  const d = [...diffLegs('p64a-rb', leg1!.pins, leg3.pins), ...diffFx('p64a-rb', leg1!.fx, leg3.fx)]
  expect(d).toEqual([])
})

test('pin sanity: anchors hold (Node baseline)', async () => {
  expect(leg1!.pins.u_byo_add.status).toBe(201)
  expect(leg1!.pins.u_byo_update_bad.status).toBe(400)
  expect(leg1!.pins.u_check_ssrf.status).toBe(400)
  expect(leg1!.pins.u_add_notjson.status).toBe(400)
  expect(leg1!.pins.u_add_notjson.text.length).toBe(705)
  expect(leg1!.pins.u_add_notjson_jsonaccept.status).toBe(400)
  expect(leg1!.pins.u_add_notjson_jsonaccept.text).toBe('{}')
  expect(leg1!.pins.u_sel_bad.status).toBe(400)
  expect(leg1!.pins.u_gr_save_llm.status).toBe(400)
  expect(leg1!.pins.an_save.status).toBe(200)
  expect(leg1!.pins.an_save_bad.status).toBe(400)
  expect(leg1!.pins.an_check_nourl.status).toBe(400)
  expect(leg1!.pins.an_check_127.status).toBe(500)
  expect(leg1!.pins.an_scan_127.status).toBe(500)
  expect(leg1!.pins.member_an_json.status).toBe(302)
  expect(leg1!.pins.u_page_301.loc).toBe('/hub#/mysettings.llm.general')
  expect(leg1!.pins.an_page_301.loc).toBe('/hub#/site.llm.instance')
  expect(leg1!.pins.an_usage_d3.status).toBe(200)
  expect(Object.keys(leg1!.pins).length).toBeGreaterThanOrEqual(57)
})

test.afterAll(async () => {
  try {
    flip('strip')
  } catch { /* best effort */ }
  try {
    adminFileReset()
    resetUserLlm(USER.email)
  } catch { /* best effort */ }
})
