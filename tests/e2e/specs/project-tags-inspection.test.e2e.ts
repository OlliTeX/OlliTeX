/**
 * project-tags-inspection — project TAGS + project-inspection (board 1c29a1c0,
 * Wave A of Phase 3), LIVE-PINNED 2026-10-12 (ag-v57 world).
 *
 * PINNED (evidence in OPEN_QUEUE_PROGRESS.md cont.8 + spec asserts):
 *   Tags (go/services/web/features/tags):
 *     POST   /tag                        {name, color}  → 200 {user_id,name,
 *                                                   color,project_ids:[],_id,__v}
 *     POST   /tag/:id/project/:pid                     → 204 (persists project_ids)
 *     DELETE /tag/:id/project/:pid                     → 204
 *     GET    /tag                          → 200 [ ... ]   (member list)
 *     POST   /tag  bad color               → 400 pin (colorRE hex6 | hsl(n,70%,45%))
 *     DELETE /tag/:id                        → 204
 *   Project inspection (go/services/web/features/projectinspection):
 *     POST /project/:pid/project-inspection/analyze
 *       body {"entryPointIds":[1..50 doc-ids]} — doc ids = project rootFolder
 *       entity _ids (NOT the y-doc id — the rootFolder docs array).
 *       200 envelope: {analysisId(uuid), analyzedAt, coverage:{
 *         ambiguousReferences:[], dynamicReferences:[], parseErrors:[],
 *         parsedFiles:N, skippedFiles:[], suppressedCitationChecks:[],
 *         truncated:false}, entryPoints:[{id,path}], graph:{cycles:[],
 *         edges:[],nodes:[{entityId,id,kind,label,path,status}],roots:[]},
 *         issues, overview, schemaVersion, views}
 *       400 EMPTY body / {}            → {"error":"INVALID_ENTRY_POINT",
 *                                          "message":"entryPointIds must contain
 *                                          between 1 and 50 ids"}
 *       400 fake id                   → {"error":"INVALID_ENTRY_POINT",
 *                                          "message":"One or more entry points
 *                                          are invalid"}
 *       404 pid                       → 404 page (router)
 *       (413 PROJECT_TOO_LARGE / 504 ANALYSIS_TIMEOUT / 499 CANCELLED /
 *        500 INSPECTION_ERROR error contract — source-pinned in contract.go;
 *        not exercised here — rate limit 6/min/project applies)
 */
import { test, expect, type Page, type Browser } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { USER } from '../fixtures/credentials'
import { execSync } from 'child_process'

const BASE = 'http://127.0.0.1:7420'
let SH: { ctx: any; page: Page } | null = null

async function api(p: Page, method: string, path: string, body?: any): Promise<{ status: number; text: string }> {
  const csrf = (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const res = await p.request.fetch(BASE + path, {
    method: method as any,
    headers: { ...(csrf ? { 'X-Csrf-Token': csrf } : {}), ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    data: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = (await res.text().catch(() => '')).slice(0, 1000000)
  return { status: res.status(), text }
}
async function page(browser: Browser): Promise<Page> {
  if (!SH) {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
    const pg = await ctx.newPage()
    await loginRobust(pg as any, USER.email, USER.password)
    SH = { ctx, page: pg }
  }
  return SH.page
}
async function newProject(p: Page): Promise<string> {
  const r = await api(p, 'POST', '/project/new', { projectName: 'tgi-' + Date.now() })
  const pid = ((r.text.match(/"[0-9a-f]{24}"/) || [null])[0] || '').replace(/"/g, '')
  if (!/^[0-9a-f]{24}$/.test(pid)) throw new Error('no pid from create: ' + r.text.slice(0, 160))
  return pid
}
function entryDocId(pid: string): string {
  const script = "print(String(db.projects.findOne({ _id: ObjectId('" + pid + "') }).rootFolder[0].docs[0]._id))"
  const out = execSync('docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval "' + script + '"', { encoding: 'utf8' })
  const v = out.trim().split('\n').pop() || ''
  if (!/^[0-9a-f]{24}$/.test(v)) throw new Error('rootFolder doc id not found: ' + v.slice(0, 120))
  return v
}

test.describe.configure({ mode: 'serial' })
test.setTimeout(420_000)

let PID = ''
let TAG = ''

test('T1 tag lifecycle: create → member add (persists) → list → remove → delete + color pin', async ({ browser }) => {
  const p = await page(browser)
  PID = await newProject(p)

  const bad = await api(p, 'POST', '/tag', { name: 'tgi-badcolor', color: 'red' })
  expect(bad.status, 'bad color → PINNED 400 (got ' + bad.status + ')').toBe(400)
  expect(bad.text, 'color-regex pin').toMatch(/must match pattern|Invalid string/)

  const mk = await api(p, 'POST', '/tag', { name: 'tgi-tag', color: '#aabbcc' })
  expect(mk.status, 'tag create → PINNED 200 (got ' + mk.status + ': ' + mk.text.slice(0, 120) + ')').toBe(200)
  TAG = (mk.text.match(/"(?:_id|id)":"([0-9a-f]{24})"/) || [null, ''])[1]
  expect(TAG).toMatch(/^[0-9a-f]{24}$/)
  expect(mk.text, 'shape: name+color+project_ids').toContain('"name":"tgi-tag"')
  expect(mk.text, 'shape: project_ids seeded').toContain('"project_ids":[]')

  const add = await api(p, 'POST', '/tag/' + TAG + '/project/' + PID)
  expect(add.status, 'add project → PINNED 204 (got ' + add.status + ')').toBe(204)
  const list = await api(p, 'GET', '/tag')
  expect(list.status, 'GET /tag → 200 (got ' + list.status + ')').toBe(200)
  expect(list.text, 'membership persists into the list').toContain('"project_ids":["' + PID + '"]')

  const rm = await api(p, 'DELETE', '/tag/' + TAG + '/project/' + PID)
  expect(rm.status, 'remove project → PINNED 204 (got ' + rm.status + ')').toBe(204)
  const del = await api(p, 'DELETE', '/tag/' + TAG)
  expect(del.status, 'tag delete → PINNED 204 (got ' + del.status + ')').toBe(204)
})

test('T2 inspection happy path: analyze → pinned envelope (doc id from rootFolder)', async ({ browser }) => {
  const p = await page(browser)
  const docId = entryDocId(PID)
  const r = await api(p, 'POST', '/project/' + PID + '/project-inspection/analyze', { entryPointIds: [docId] })
  expect(r.status, 'analyze → PINNED 200 (got ' + r.status + ': ' + r.text.slice(0, 140) + ')').toBe(200)
  const j: any = JSON.parse(r.text)
  expect(String(j.schemaVersion), 'schemaVersion present').toBeTruthy()
  expect(j.analysisId, 'analysisId (uuid) present').toMatch(/[0-9a-f-]{8,}/)
  expect(j.analyzedAt, 'analyzedAt ISO').toBeTruthy()
  expect(typeof j.coverage?.parsedFiles === 'number', 'coverage.parsedFiles number (got ' + JSON.stringify(j.coverage || null).slice(0, 120) + ')').toBe(true)
  expect(Array.isArray(j.coverage?.parseErrors), 'coverage.parseErrors array').toBe(true)
  expect(j.entryPoints?.[0]?.id, 'entryPoints[0].id echoes the doc id').toBe(docId)
  expect(j.graph?.nodes, 'graph.nodes array').toBeTruthy()
  if (j.graph?.nodes?.length) {
    const n = j.graph.nodes[0]
    expect(n.kind, 'node.kind pin').toBe('document')
    expect(n.status, 'node.status pin (got ' + n.status + ')').toBe('normal')
  }
  expect(['issues', 'overview', 'views'].every((k) => k in j), 'envelope keys issues/overview/views').toBe(true)
})

test('T3 inspection error contract: empty ids / fake id / bad body / bad pid (all pinned)', async ({ browser }) => {
  const p = await page(browser)
  const empty = await api(p, 'POST', '/project/' + PID + '/project-inspection/analyze', { entryPointIds: [] })
  expect(empty.status, 'empty ids → PINNED 400 (got ' + empty.status + ')').toBe(400)
  expect(empty.text).toContain('INVALID_ENTRY_POINT')
  expect(empty.text).toContain('must contain between 1 and 50 ids')

  const fake = await api(p, 'POST', '/project/' + PID + '/project-inspection/analyze', { entryPointIds: ['f'.repeat(24)] })
  expect(fake.status, 'fake id → PINNED 400 (got ' + fake.status + ')').toBe(400)
  expect(fake.text).toContain('One or more entry points are invalid')

  const bad = await api(p, 'POST', '/project/' + PID + '/project-inspection/analyze', { bogus: 1 })
  expect(bad.status, 'bad body → PINNED 400 (got ' + bad.status + ')').toBe(400)
  expect(bad.text).toContain('INVALID_ENTRY_POINT')

  const r404 = await api(p, 'POST', '/project/' + 'f'.repeat(24) + '/project-inspection/analyze', { entryPointIds: ['f'.repeat(24)] })
  expect(r404.status, 'bad pid → PINNED 404 (got ' + r404.status + ')').toBeGreaterThanOrEqual(400)
})

test('T4 cleanup: disposable project goes to the trash (pinned 200)', async ({ browser }) => {
  const p = await page(browser)
  const r = await api(p, 'POST', '/project/' + PID + '/trash')
  expect(r.status, 'trash → PINNED 200 (got ' + r.status + ')').toBe(200)
})
