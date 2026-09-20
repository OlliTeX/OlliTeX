/**
 * WEB-GO P6.4b FLIP GATE (WEB_GO_PLAN.md P6.4b — OlliTeX llm module,
 * project-scoped live-LLM surface):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p64b.conf):
 *     GET  /project/<oid>/llm/models
 *     GET  /project/<oid>/llm/features
 *     GET  /project/<oid>/llm/source-context
 *     GET  /project/<oid>/llm/prompts
 *     POST /project/<oid>/llm/chat
 *     POST /project/<oid>/llm/completion
 *     POST /project/<oid>/llm/compile-fix
 *     POST /project/<oid>/llm/grammar
 *     POST /project/<oid>/llm/generate
 *     GET  /project/<oid>/llm/compliance/rubrics
 *     POST /project/<oid>/llm/compliance/start
 *     GET  /project/<oid>/llm/compliance/status/<jobId>
 *     POST /project/<oid>/llm/compliance/cancel/<jobId>
 *
 *   leg 1  Node baseline (p64b flip OFF)
 *   leg 2  FLIP ON — Go (flips ON, battery, flip OFF)
 *   leg 3  Node re-baseline (flip OFF)
 *
 *   Battery mirrors the Node oracles (/tmp/p64b_oracle.mjs 65 pins +
 *   /tmp/p64b_job.mjs, live 2026-09-16): anon member authz matrix,
 *   zod-objectId 404 JSON, 404 HTML pages, 403 Restricted pages, getModels
 *   (clean / BYO row / disabled row / admin pool / admin-disabled), feature
 *   flags, source-context (16 pins: radius clamp, line parse, path
 *   normalization, missing file), prompts (defaults + admin overrides),
 *   chat (invalid messages, no lane 503, bad row 400, dead-host 502
 *   2-attempt message), completion (no context 400 + dead-host 502),
 *   compile-fix (bad request, dead-host 502 single attempt, missing file
 *   404), grammar (invalid spans + dead-host 502), generate (bad type 400,
 *   default lane + explicit dead row 502), compliance (rubrics clean/user,
 *   start no-rubric, start running response, status unknown, cancel
 *   unknown, review disabled), compliance JOB lifecycle (start -> running
 *   response, poll to done with 'schema is not a function' na item, cancel
 *   after done) — plus FX parity: llmUserBudget counters, llmreviewjobs
 *   docs (job payload incl. result), user LLM profile, admin file bytes,
 *   llmusages counts.
 *
 *   Determinism: every live-LLM attempt in the battery targets a dead
 *   NXDOMAIN BYO row (https://dead-llm.e2e.test) or no lane at all, so the
 *   pinned shape is the offline failure/contract surface. Compliance
 *   reviews deterministically end 'done' with items[].status 'na' and the
 *   pinned local 'schema is not a function' evidence (Node build quirk).
 *
 *   Volatile normalization before diff: enc:v1 blobs -> ENC, 8-hex
 *   provider row ids -> PID, u:<hex> refs -> u:PID:, job ids (job-...) ->
 *   JOB, ISO dates -> TS, documentTokensEstimate -> EST (Node counts
 *   UTF-16 units, Go counts bytes), DNS failure causes (Node
 *   'getaddrinfo ENOTFOUND <host>' vs Go 'lookup <host>: no such host')
 *   -> NETERR.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import fs from 'node:fs'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
// Cumulative flip: P6.4b routes plus the P6.4a user/admin LLM surface — the
// battery mutates state (BYO rows, user rubrics) through the P6.4a routes,
// so the Go leg must exercise the Go P6.4a write/read paths (catches
// obj-corruption regressions); P7 applies the union of all flips anyway.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf']
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const ADMINFILE = '/var/lib/overleaf/data/llm-admin-settings.json'
// pristine admin-settings fixture (repo-tracked; /tmp fallback for ad-hoc runs)
const ADMINBAK_REPO = path.resolve(process.cwd(), 'fixtures/p64a-admin-settings.bak')
const ADMINBAK = fs.existsSync(ADMINBAK_REPO) ? ADMINBAK_REPO : '/tmp/p64a_adminfile.bak'
const UA = 'p64b-gate'
const UPID = '6aa5180a66bc6891d28afce2' // e2e-user, docs: main.typ (39 lines), references.bib
const APID = '6aaa7f570accd346715942b5' // e2e-admin (member must NOT read)

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
  async function withRetry(fn: () => Promise<any>): Promise<any> {
    let last: unknown
    for (let i = 0; i < 6; i++) {
      try {
        return await fn()
      } catch (e) {
        last = e
        await sleep(400 * (i + 1))
      }
    }
    throw last
  }
  const r0: any = await withRetry(() => fetch(BASE + '/login', { headers: { 'user-agent': UA }, redirect: 'manual' }))
  const html: string = await r0.text()
  const csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  if (!csrf) throw new Error('no csrf meta')
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1: any = await withRetry(() => fetch(BASE + '/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0, 'user-agent': UA },
    body: JSON.stringify({ email: acct.email, password: acct.password }),
    redirect: 'manual',
  }))
  const body1 = await r1.text()
  if (r1.status !== 200) throw new Error('login failed ' + r1.status + ' ' + body1.slice(0, 200))
  const ck = (r1.headers.get('set-cookie') || '').split(';')[0]
  const cs: any = await withRetry(() => fetch(BASE + '/dev/csrf', { headers: { cookie: ck, 'user-agent': UA } }))
  const tok: string = (await cs.text()).trim()
  if (!tok) throw new Error('no /dev/csrf token')
  return { ck, csrf: tok }
}

const withCsrf = (a: { ck: string; csrf: string }) => ({ cookie: a.ck, headers: { 'user-agent': UA, 'x-csrf-token': a.csrf } })

function verifyInclude(n: number): void {
  for (const conf of FLIPCONFS) {
    const cnt = dexeStrict(overleafC, `sh -c 'grep -c "overleaf-flips/${conf}" /etc/nginx/sites-enabled/overleaf.conf || true'`).trim()
    if (cnt !== String(n)) throw new Error(`flip include for ${conf}: expected ${n}, found ${cnt}`)
  }
}
async function flip(mode: 'apply' | 'strip'): Promise<void> {
  if (mode === 'strip') {
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`)
    dexeStrict(overleafC, 'nginx -t')
    dexe(overleafC, 'nginx -s reload')
    await sleep(1200)
    verifyInclude(0)
    return
  }
  dexeStrict(overleafC, 'mkdir -p /etc/nginx/overleaf-flips')
  for (const conf of FLIPCONFS) {
    dexeStrict(overleafC, `cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf}`)
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${conf};'+String.fromCharCode(10);if(!s.includes('overleaf-flips/${conf}')){if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)}else{throw new Error('anchor not found')}};fs.writeFileSync(p,s)"`)
  }
  dexeStrict(overleafC, 'nginx -t')
  dexe(overleafC, 'nginx -s reload')
  // nginx -s reload is async; workers drain + accept the new config over a
  // few hundred ms. Fresh requests in that window get 'other side closed'.
  await sleep(1200)
  verifyInclude(1)
}

// ---------- volatile normalization ----------
const norm = (s: string): string =>
  s
    .replace(/enc:v1:[A-Za-z0-9+/=:.]+/g, 'ENC')
    .replace(/"(?:id)":\"[0-9a-f]{8}\"/g, '"id":"PID"')
    .replace(/u:[0-9a-f]{8}:/g, 'u:PID:')
    .replace(/"jobId":"job-[A-Za-z0-9-]*"/g, '"jobId":"JOB"')
    .replace(/"(?:_id|id|jobId)":"job-[A-Za-z0-9-]*"/g, (m: string) => m.replace(/job-[A-Za-z0-9-]*/, 'JOB'))
    .replace(/"createdAt":"[^"]*"/g, '"createdAt":"TS"')
    .replace(/"updatedAt":"[^"]*"/g, '"updatedAt":"TS"')
    .replace(/"documentTokensEstimate":\d+/g, '"documentTokensEstimate":EST')
    .replace(/getaddrinfo ENOTFOUND [^ (]+/g, 'NETERR')
    .replace(/lookup [A-Za-z0-9.-]+: no such host/g, 'NETERR')
    .replace(/nonce="[^"]+"/g, 'nonce="NONCE"')
    .replace(/<meta name="ol-csrfToken" content="[^"]*"/g, '<meta name="ol-csrfToken" content="CSRF"')
    .replace(/<input name="_csrf" type="hidden" value="[^"]*"/g, '<input name="_csrf" type="hidden" value="CSRF"')
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
    const naRaw = a.text
    const nbRaw = b.text
    if (typeof naRaw !== 'string' || typeof nbRaw !== 'string') {
      ds.push(`${label}:${k} non-string body A=${typeof naRaw} B=${typeof nbRaw} :: A~${JSON.stringify(naRaw).slice(0, 120)} || B~${JSON.stringify(nbRaw).slice(0, 120)}`)
      continue
    }
    const na = norm(naRaw)
    const nb = norm(nbRaw)
    if (na !== nb) {
      let i = 0
      while (i < na.length && i < nb.length && na[i] === nb[i]) {
        i++
      }
      if (i >= Math.min(na.length, nb.length)) i = Math.min(na.length, nb.length)
      ds.push(`${label}:${k} body@${i} | A~${na.slice(Math.max(0, i - 60), i + 140).replace(/\n/g, ' ')} || B~${nb.slice(Math.max(0, i - 60), i + 140).replace(/\n/g, ' ')}`)
    }
  }
  return ds
}

function diffFx(label: string, A: FX, B: FX): string[] {
  const ds: string[] = []
  const ks = [...new Set([...Object.keys(A), ...Object.keys(B)])].sort()
  const oidNorm = (t: string): string => t.replace(/"(_id)":"[0-9a-f]{24}"/g, '"_id":"OID"')
  for (const k of ks) {
    const one = (v: any): string => {
      if (Array.isArray(v)) {
        const arr = (v as any[]).map((x) => oidNorm(JSON.stringify(x))).sort()
        return norm(oidNorm(JSON.stringify(arr)))
      }
      return norm(oidNorm(JSON.stringify(v ?? null)))
    }
    const a = one(A[k])
    const b = one(B[k])
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
function clearBudget(email: string): void {
  const uid = msh(`var u = db.users.findOne({email: ${JSON.stringify(email)}}); print(u._id.toHexString())`, true).trim().split('\n').pop()
  msh(`db.llmUserBudget.deleteMany({userId: new ObjectId(${JSON.stringify(uid)})})`)
}
function dropJobs(): void {
  msh(`db.llmreviewjobs.deleteMany({})`)
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
function budgetCount(email: string): number {
  const uid = msh(`var u = db.users.findOne({email: ${JSON.stringify(email)}}); print(u._id.toHexString())`, true).trim().split('\n').pop()
  return Number(msh(`print(db.llmUserBudget.countDocuments({userId: new ObjectId(${JSON.stringify(uid)})}))`, true).trim().split('\n').pop()) || 0
}
function jobsDocs(): any[] {
  const out = msh(`print(JSON.stringify(db.llmreviewjobs.find({}, {_id:1, project_id:1, user_id:1, status:1, position:1, rubric:1, model:1, result:1, error:1, createdAt:1, updatedAt:1}).toArray()))`, true).trim().split('\n').pop()
  try { return JSON.parse(out) || [] } catch { return [{ raw: out }] }
}
function usagesCount(): number {
  return Number(msh(`print(db.llmusages.countDocuments({}))`, true).trim().split('\n').pop()) || 0
}

// ---------- battery (oracle order) ----------
async function runLeg(tag: string): Promise<{ pins: Leg; fx: FX }> {
  await waitGo()
  adminFileReset()
  resetUserLlm(USER.email)
  resetUserLlm(ADMIN.email)
  clearBudget(USER.email)
  clearBudget(ADMIN.email)
  dropJobs()
  const usagesBefore = usagesCount()
  const budgetBefore = budgetCount(USER.email)

  const pins: Leg = {}

  const MU = await login(USER)
  const MA = await login(ADMIN)
  const MH = withCsrf(MU)
  const AH = withCsrf(MA)

  // ---- A. authz + id validation ----
  pins.a_anon_models = await call({ path: `/project/${UPID}/llm/models` })
  pins.a_anon_features = await call({ path: `/project/${UPID}/llm/features` })
  pins.a_anon_srcctx = await call({ path: `/project/${UPID}/llm/source-context?file=main.typ&line=5` })
  pins.a_anon_prompts = await call({ path: `/project/${UPID}/llm/prompts` })
  pins.a_member_adminproj403 = await call({ ...MH, path: `/project/${APID}/llm/models` })
  pins.a_member_srcctx403 = await call({ ...MH, path: `/project/${APID}/llm/source-context?file=main.typ&line=5` })
  pins.a_badoid_models = await call({ ...MH, path: `/project/zzz/llm/models` })
  pins.a_missing_models = await call({ ...MH, path: `/project/6aaa000000000000000000ff/llm/models` })
  pins.a_missing_srcctx = await call({ ...MH, path: `/project/6aaa000000000000000000ff/llm/source-context?file=main.typ&line=5` })

  // ---- B. getModels ----
  pins.b_models_clean = await call({ ...MH, path: `/project/${UPID}/llm/models` })
  const addDead = await call({ ...MH, path: '/user/llm-providers', method: 'POST' },
    { name: 'DeadGW', providerType: 'openaiCompatible', baseUrl: 'https://dead-llm.e2e.test/v1/', apiKey: 'sk-dead', models: ['gpt-dead'], completionModel: 'gpt-dead' })
  if (addDead.status !== 201) throw new Error('dead row add failed: ' + addDead.text)
  const ROWID: string = addDead.json?.provider?.id || '(none)'
  pins.b_models_byo = await call({ ...MH, path: `/project/${UPID}/llm/models` })
  await call({ ...MH, path: `/user/llm-providers/${ROWID}`, method: 'POST' }, { enabled: false })
  pins.b_models_byo_disabled = await call({ ...MH, path: `/project/${UPID}/llm/models` })
  await call({ ...MH, path: `/user/llm-providers/${ROWID}`, method: 'POST' }, { enabled: true })
  // Node admin-save quirk: body without string systemPrompt -> file written,
  // then 500 error page (681B). Pinned on both legs (Node both legs here; the
  // Go surface covers it per P6.4b implementation).
  pins.b_admin_save_emptyurl = await call({ ...AH, path: '/admin/llm/settings', method: 'POST' }, { llmApiUrl: '', allowedModels: ['gpt-x', 'Claude Y'] })
  pins.b_models_adminpool = await call({ ...MH, path: `/project/${UPID}/llm/models` })
  pins.b_admin_save_disabled = await call({ ...AH, path: '/admin/llm/settings', method: 'POST' }, { llmDisabledByAdmin: true })
  pins.b_models_disabledbyadmin = await call({ ...MH, path: `/project/${UPID}/llm/models` })
  adminFileReset()

  // ---- F. features ----
  pins.f_features_clean = await call({ ...MH, path: `/project/${UPID}/llm/features` })
  pins.f_admin_set = await call({ ...AH, path: '/admin/llm/settings', method: 'POST' }, { chatEnabled: false, completionEnabled: true, reviewEnabled: false })
  pins.f_features_flags = await call({ ...MH, path: `/project/${UPID}/llm/features` })
  adminFileReset()

  // ---- C. source-context ----
  pins.s_def = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=5` })
  pins.s_r2 = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=5&radius=2` })
  pins.s_r999 = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=5&radius=999` })
  pins.s_linemax = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=999999` })
  pins.s_line0 = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=0` })
  pins.s_lineneg = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=-3` })
  pins.s_nofile = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=abc` })
  pins.s_missing = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=missing.tex&line=5` })
  pins.s_noparam = await call({ ...MH, path: `/project/${UPID}/llm/source-context` })
  pins.s_nofileparam = await call({ ...MH, path: `/project/${UPID}/llm/source-context?line=5` })
  pins.s_norm_compile = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=compile%2Fmain.typ&line=5` })
  pins.s_norm_lead = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=%2Fmain.typ&line=5` })
  pins.s_dot = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=%2E%2Fmain.typ&line=5` })
  pins.s_rnan = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=5&radius=abc` })
  pins.s_rneg = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=5&radius=-5` })
  pins.s_rzero = await call({ ...MH, path: `/project/${UPID}/llm/source-context?file=main.typ&line=5&radius=0` })

  // ---- D. prompts ----
  pins.p_clean = await call({ ...MH, path: `/project/${UPID}/llm/prompts` })
  pins.p_admin_set = await call({ ...AH, path: '/admin/llm/settings', method: 'POST' }, { askAiSystemPrompt: 'ORA-CUSTOM-SP', errorPrompt: 'ORA-CUSTOM-EP', askAiActionPrompts: { paraphrase: 'ORA-PARA' } })
  pins.p_custom = await call({ ...MH, path: `/project/${UPID}/llm/prompts` })
  adminFileReset()

  // ---- E. chat failure paths ----
  await call({ ...MH, path: `/user/llm-providers/${ROWID}/delete`, method: 'POST' }, {})
  clearBudget(USER.email)
  pins.c_messages_empty = await call({ ...MH, path: `/project/${UPID}/llm/chat`, method: 'POST' }, { messages: [] })
  pins.c_msgs_notarray = await call({ ...MH, path: `/project/${UPID}/llm/chat`, method: 'POST' }, { messages: 'hi' })
  pins.c_no_lane = await call({ ...MH, path: `/project/${UPID}/llm/chat`, method: 'POST' }, { messages: [{ role: 'user', content: 'hi' }] })
  pins.c_profile_ref_norow = await call({ ...MH, path: `/project/${UPID}/llm/chat`, method: 'POST' }, { messages: [{ role: 'user', content: 'hi' }], model: `u:${ROWID}:gpt-dead` })
  const addDead2 = await call({ ...MH, path: '/user/llm-providers', method: 'POST' },
    { name: 'DeadGW', providerType: 'openaiCompatible', baseUrl: 'https://dead-llm.e2e.test/v1/', apiKey: 'sk-dead', models: ['gpt-dead'], completionModel: 'gpt-dead' })
  const ROW2: string = (addDead2.json?.provider?.id || ROWID)
  clearBudget(USER.email)
  pins.c_byo_dead = await call({ ...MH, path: `/project/${UPID}/llm/chat`, method: 'POST' }, { messages: [{ role: 'user', content: 'hi' }], model: `u:${ROW2}:gpt-dead` })
  pins.c_byo_dead_defaultlane = await call({ ...MH, path: `/project/${UPID}/llm/chat`, method: 'POST' }, { messages: [{ role: 'user', content: 'hi' }] })

  // ---- G. completion ----
  clearBudget(USER.email)
  pins.k_noctx = await call({ ...MH, path: `/project/${UPID}/llm/completion`, method: 'POST' }, {})
  pins.k_byo_dead = await call({ ...MH, path: `/project/${UPID}/llm/completion`, method: 'POST' }, { leftContext: 'Hello wor', rightContext: 'ld!' })

  // ---- H. compile-fix ----
  clearBudget(USER.email)
  pins.x_nofile = await call({ ...MH, path: `/project/${UPID}/llm/compile-fix`, method: 'POST' }, { line: 5, message: 'x' })
  pins.x_notext = await call({ ...MH, path: `/project/${UPID}/llm/compile-fix`, method: 'POST' }, { file: '', line: 5 })
  pins.x_badline = await call({ ...MH, path: `/project/${UPID}/llm/compile-fix`, method: 'POST' }, { file: 'main.tex', line: 'abc' })
  pins.x_byo_dead = await call({ ...MH, path: `/project/${UPID}/llm/compile-fix`, method: 'POST' }, { file: 'main.typ', line: 5, level: 'error', message: 'Undefined control sequence' })
  pins.x_missing_file = await call({ ...MH, path: `/project/${UPID}/llm/compile-fix`, method: 'POST' }, { file: 'missing.tex', line: 5, level: 'error', message: 'x' })

  // ---- I. grammar ----
  clearBudget(USER.email)
  pins.g_spans_notarray = await call({ ...MH, path: `/project/${UPID}/llm/grammar`, method: 'POST' }, { spans: 'x' })
  pins.g_spans_empty = await call({ ...MH, path: `/project/${UPID}/llm/grammar`, method: 'POST' }, { spans: [] })
  pins.g_byo_dead = await call({ ...MH, path: `/project/${UPID}/llm/grammar`, method: 'POST' }, { spans: [{ text: 'This is a sentence with a misspelled word here.' }] })

  // ---- J. generate ----
  clearBudget(USER.email)
  pins.e_badtype = await call({ ...MH, path: `/project/${UPID}/llm/generate`, method: 'POST' }, { type: 'bogus' })
  pins.e_title_nolane = await call({ ...MH, path: `/project/${UPID}/llm/generate`, method: 'POST' }, { type: 'title' })
  pins.e_title_byo_dead = await call({ ...MH, path: `/project/${UPID}/llm/generate`, method: 'POST' }, { type: 'title', model: `u:${ROW2}:gpt-dead` })

  // ---- K. compliance ----
  pins.r_rubrics_clean = await call({ ...MH, path: `/project/${UPID}/llm/compliance/rubrics` })
  const compSave = await call({ ...MH, path: '/user/llm/compliance', method: 'POST' },
    { rubrics: [{ id: 'ora-rubric', name: 'ORA Rubric', guidelines: 'be nice', scanPatterns: '' }] })
  if (compSave.status !== 200) throw new Error('comp save failed: ' + compSave.text)
  pins.r_rubrics_user = await call({ ...MH, path: `/project/${UPID}/llm/compliance/rubrics` })
  pins.r_start_norubric = await call({ ...MH, path: `/project/${UPID}/llm/compliance/start`, method: 'POST' }, { rubricId: 'nope' })
  pins.r_start_nolane = await call({ ...MH, path: `/project/${UPID}/llm/compliance/start`, method: 'POST' }, { rubricId: 'ora-rubric' })
  pins.r_status_unknown = await call({ ...MH, path: `/project/${UPID}/llm/compliance/status/6a10000000000000000000aa` })
  pins.r_cancel_unknown = await call({ ...MH, method: 'POST', path: `/project/${UPID}/llm/compliance/cancel/6a10000000000000000000aa` })
  pins.r_admin_set_reviewoff = await call({ ...AH, path: '/admin/llm/settings', method: 'POST' }, { reviewEnabled: false })
  pins.r_review_disabled_after_admin = await call({ ...MH, path: `/project/${UPID}/llm/compliance/rubrics` })
  adminFileReset()

  // ---- K2. compliance job lifecycle (p64b_job oracle) ----
  const jstart = await call({ ...MH, path: `/project/${UPID}/llm/compliance/start`, method: 'POST' }, { rubricId: 'ora-rubric' })
  pins.j_start = jstart
  const jobId: string = jstart.json?.jobId || ''
  let last: { status: number; ct: string; loc: string | null; text: string; json: any } | null = null
  for (let i = 0; i < 40; i++) {
    await sleep(1000)
    last = await call({ ...MH, path: `/project/${UPID}/llm/compliance/status/${jobId}` })
    if (last.json && (last.json.status === 'error' || last.json.status === 'done' || last.json.status === 'cancelled')) break
  }
  if (!last) throw new Error('job never polled')
  pins.j_status_final = last
  if (!(last.json && (last.json.status === 'done' || last.json.status === 'error'))) {
    throw new Error('job did not converge: ' + last.text.slice(0, 200))
  }
  pins.j_cancel_after = await call({ ...MH, path: `/project/${UPID}/llm/compliance/cancel/${jobId}`, method: 'POST' }, {})

  // ---- FX (after battery) ----
  const fx: FX = {
    budgetBefore,
    budgetAfter: budgetCount(USER.email),
    jobs: jobsDocs(),
    userLLM: userLLMProfile(USER.email),
    adminFile: adminFileText(),
    llmusagesBefore: usagesBefore,
    llmusagesAfter: usagesCount(),
  }

  // ---- cleanup ----
  await call({ ...MH, path: `/user/llm-providers/${ROW2}/delete`, method: 'POST' }, {})
  resetUserLlm(USER.email)
  clearBudget(USER.email)
  adminFileReset()
  dropJobs()
  return { pins, fx }
}

// ---------- legs ----------
let leg1: { pins: Leg; fx: FX } | null = null
let leg2: { pins: Leg; fx: FX } | null = null

test.describe.configure({ mode: 'serial' })

test('leg 0: flip off before start', async () => {
  await flip('strip')
})

test('leg 1: Node baseline', async () => {
  leg1 = await runLeg('leg1')
  expect(leg1).toBeTruthy()
})

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  try {
    leg2 = await runLeg('leg2')
  } finally {
    await flip('strip')
  }
  const d = [...diffLegs('p64b', leg1!.pins, leg2!.pins), ...diffFx('p64b', leg1!.fx, leg2!.fx)]
  expect(d).toEqual([])
})

test('leg 3: Node re-baseline', async () => {
  const leg3 = await runLeg('leg3')
  const d = [...diffLegs('p64b-rb', leg1!.pins, leg3.pins), ...diffFx('p64b-rb', leg1!.fx, leg3.fx)]
  expect(d).toEqual([])
})

test('pin sanity: anchors hold (Node baseline)', async () => {
  expect(leg1!.pins.a_anon_models.status).toBe(302)
  expect(leg1!.pins.a_anon_models.loc).toBe('/login')
  expect(leg1!.pins.a_member_adminproj403.status).toBe(403)
  expect(leg1!.pins.a_member_adminproj403.text).toContain('Restricted')
  expect(leg1!.pins.a_badoid_models.status).toBe(404)
  expect(leg1!.pins.a_badoid_models.text).toContain('Invalid Mongo ObjectId')
  expect(leg1!.pins.a_missing_models.status).toBe(404)
  expect(leg1!.pins.a_missing_models.text).toContain('Page Not Found')
  expect(leg1!.pins.b_models_clean.status).toBe(200)
  expect(leg1!.pins.b_models_clean.text).toBe('{"models":[],"userRows":[]}')
  expect(leg1!.pins.b_models_byo.text).toContain('"isDefault":true')
  expect(leg1!.pins.f_features_clean.text).toBe('{"chatEnabled":true,"completionEnabled":true,"reviewEnabled":true,"allowUserSettings":true}')
  expect(leg1!.pins.b_admin_save_emptyurl.status).toBe(500)
  expect(leg1!.pins.b_admin_save_emptyurl.text.length).toBe(681)
  expect(leg1!.pins.b_models_disabledbyadmin.text).toBe('{"models":[],"userRows":[]}')
  expect(leg1!.pins.s_def.status).toBe(200)
  expect(leg1!.pins.s_def.text.startsWith('{"ok":true,"file":"/main.typ","line":5')).toBe(true)
  expect(leg1!.pins.s_missing.text).toBe('{"ok":false,"error":"not_found"}')
  expect(leg1!.pins.c_no_lane.text).toBe('{"ok":false,"error":"llm-disabled","message":"LLM service is not configured"}')
  expect(leg1!.pins.c_profile_ref_norow.status).toBe(400)
  expect(leg1!.pins.c_byo_dead.status).toBe(502)
  expect(leg1!.pins.c_byo_dead.text).toContain('Failed after 2 attempts')
  expect(leg1!.pins.c_byo_dead.text).toContain('Cannot connect to API')
  expect(leg1!.pins.x_byo_dead.status).toBe(502)
  expect(!leg1!.pins.x_byo_dead.text.includes('Failed after'), 'compile-fix is single-attempt')
  expect(leg1!.pins.k_noctx.text).toBe('{"success":false,"error":"No context provided"}')
  expect(leg1!.pins.e_badtype.text).toContain('Unknown generator type')
  expect(leg1!.pins.r_start_norubric.text).toBe('{"ok":false,"error":"no_rubric","message":"Unknown or missing rubric"}')
  expect(leg1!.pins.r_start_nolane.status).toBe(200)
  expect(leg1!.pins.r_start_nolane.text).toContain('"status":"running"')
  expect(leg1!.pins.r_status_unknown.text).toBe('{"ok":false,"error":"not_found","message":"Review not found or expired"}')
  expect(leg1!.pins.r_cancel_unknown.text).toBe('{"ok":true}')
  expect(leg1!.pins.j_start.text).toMatch(/"status":"(running|queued)"/)
  expect(leg1!.pins.j_status_final.status).toBe(200)
  expect(leg1!.pins.j_status_final.text).toContain('"status":"done"')
  expect(leg1!.pins.j_status_final.text).toContain('schema is not a function')
  expect(leg1!.pins.j_status_final.text).toContain('"requirement":"be nice"')
  expect(leg1!.pins.j_cancel_after.text).toBe('{"ok":true}')
  expect(Object.keys(leg1!.pins).length).toBeGreaterThanOrEqual(66)
})

test.afterAll(async () => {
  try {
    await flip('strip')
  } catch { /* best effort */ }
  try {
    adminFileReset()
    resetUserLlm(USER.email)
    clearBudget(USER.email)
    dropJobs()
  } catch { /* best effort */ }
})
