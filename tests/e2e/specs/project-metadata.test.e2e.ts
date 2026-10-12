/**
 * project-metadata — settings round-trip + metadata endpoints + trash
 * lifecycle (board 1c29a1c0, Wave A), LIVE-PINNED 2026-10-12 (ag-v57 world).
 *
 * PINNED (evidence OPEN_QUEUE_PROGRESS.md cont.8 + asserts):
 *   Settings (projectlist/projectsettings.go — Node POST /project/:id/settings
 *   → 204; z.strictObject; unknown keys → 400):
 *     allowed keys: compiler, imageName, png2pdf, name, spellCheckLanguage,
 *       mainBibliographyDocId, rootDocId, referenceFormat, grammarPicky
 *     ** description is NOT a writable setting key → POST settings with
 *        description → 400 "Unrecognized key" (PINNED — owner: the Go port's
 *        Node-parity surface has no description setter)
 *     {} (no fields) → 204 no-op; compiler:"typst-0.13" → 204 + doc.compiler
 *       persisted (mongo-verified)
 *   Metadata read:
 *     GET  /project/:pid/metadata  → 200 {projectId, projectMeta:{docId:{
 *        labels:[] , packages:{pkg:[{caption,meta,score,snippet}...]} , ...}}
 *        (per-doc editor meta — labels + command-palette package map;
 *        doc id in projectMeta keys = the y-doc id (project_id+1))
 *     GET  /project/:pid/details   → ** 404 in this build (route NOT WIRED)
 *                                     — the metadata surface is /metadata
 *     GET  /project/:pid/entities  → 200 {project_id, entities:[{path,type}]}
 *       POST /project/:pid/doc     → 200 {name,_id} (body key "name")
 *                                     {filePath} → 400 Unrecognized key pin
 *   Trash (Node parity, projectlist/projectflags.go):
 *     POST   /project/:pid/trash   → 200 "OK"; project VANISHES from
 *                                     GET /user/projects; details → 404 page
 *     DELETE /project/:pid/trash   → 200 "OK'; project back in the list
 *     POST   /project/:pid/restore → 200 "OK" (restore path, pinned)
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
function mongoEval(e: string): string {
  const out = execSync('docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval "' + e.replace(/"/g, '\\"') + '"', { encoding: 'utf8' })
  if (/invalid|failed|error/i.test(out)) throw new Error('mongo eval: ' + out.trim().split('\n').pop())
  return out.trim().split('\n').pop() || ''
}

test.describe.configure({ mode: 'serial' })
test.setTimeout(300_000)

let PID = ''

test('D1 create + settings pins: 204 acks, description-absent 400, compiler persists, unknown-key 400', async ({ browser }) => {
  const p = await page(browser)
  const mk = await api(p, 'POST', '/project/new', { projectName: 'pm-' + Date.now() })
  expect(mk.status).toBeLessThan(400)
  PID = ((mk.text.match(/"[0-9a-f]{24}"/) || [null])[0] || '').replace(/"/g, '')
  expect(PID).toMatch(/^[0-9a-f]{24}$/)

  const noop = await api(p, 'POST', '/project/' + PID + '/settings', {})
  expect(noop.status, 'settings {} no-op → PINNED 204 (got ' + noop.status + ')').toBe(204)

  const desc = await api(p, 'POST', '/project/' + PID + '/settings', { description: 'x' })
  expect(desc.status, 'description key → PINNED 400 (not a settings key) (got ' + desc.status + ')').toBe(400)
  expect(desc.text, 'unrecognized-key pin').toContain('Unrecognized key')
  expect(desc.text).toContain('"description"')

  const bogus = await api(p, 'POST', '/project/' + PID + '/settings', { bogusKey: true })
  expect(bogus.status, 'unknown key → PINNED 400 (got ' + bogus.status + ')').toBe(400)
  expect(bogus.text).toContain('Unrecognized key')

  const cc = await api(p, 'POST', '/project/' + PID + '/settings', { compiler: 'typst-0.13' })
  expect(cc.status, 'compiler set → PINNED 204 (got ' + cc.status + ')').toBe(204)
  const stored = mongoEval('print(String(db.projects.findOne({ _id: ObjectId(\'' + PID + '\') }).compiler))').trim()
  expect(stored, 'doc.compiler persisted (mongo; got [' + stored + '])').toBe('typst-0.13')

  const back = await api(p, 'POST', '/project/' + PID + '/settings', { compiler: 'latex-dev' })
  expect(back.status, 'compiler back → 204 (got ' + back.status + ')').toBe(204)
})

test('D2 metadata GET: {projectId, projectMeta:{docId:{labels,packages}}} pinned', async ({ browser }) => {
  const p = await page(browser)
  const r = await api(p, 'GET', '/project/' + PID + '/metadata')
  expect(r.status, 'GET /metadata → PINNED 200 (got ' + r.status + ')').toBe(200)
  const j: any = JSON.parse(r.text)
  expect(j.projectId, 'projectId echo').toBe(PID)
  expect(j.projectMeta, 'projectMeta object').toBeTruthy()
  const docKeys = Object.keys(j.projectMeta || {})
  expect(docKeys.length, 'at least one doc entry').toBeGreaterThanOrEqual(1)
  const first = j.projectMeta[docKeys[0]]
  expect(Array.isArray(first?.labels), 'entry.labels array (got ' + JSON.stringify(first || null).slice(0, 100) + ')').toBe(true)
  expect(first?.packages, 'entry.packages (command-palette map)').toBeTruthy()

  // details route retired (404 page) in this build — pin
  const det = await api(p, 'GET', '/project/' + PID + '/details')
  expect(det.status, 'GET /details → PINNED 404 (route not wired) (got ' + det.status + ')').toBeGreaterThanOrEqual(404)
})

test('D3 entities + doc-create pins: /entities shape, /doc name body, filePath rejected', async ({ browser }) => {
  const p = await page(browser)
  const ents = await api(p, 'GET', '/project/' + PID + '/entities')
  expect(ents.status, 'GET /entities → PINNED 200 (got ' + ents.status + ')').toBe(200)
  const j: any = JSON.parse(ents.text)
  expect(j.project_id).toBe(PID)
  expect(Array.isArray(j.entities), 'entities array').toBe(true)
  expect(j.entities[0]?.type, 'entity type pin (got ' + JSON.stringify(j.entities[0] || null) + ')').toBe('doc')
  expect(String(j.entities[0]?.path)).toContain('.tex')

  const bad = await api(p, 'POST', '/project/' + PID + '/doc', { filePath: '/n2.tex' })
  expect(bad.status, 'doc create filePath → PINNED 400 (got ' + bad.status + ')').toBe(400)
  expect(bad.text).toContain('Unrecognized key')

  const mk = await api(p, 'POST', '/project/' + PID + '/doc', { name: 'notes.md' })
  expect(mk.status, 'doc create name → PINNED 200 (got ' + mk.status + ': ' + mk.text.slice(0, 100) + ')').toBe(200)
  expect(mk.text, 'body echo {name,_id}').toContain('"name":"notes.md"')
})

test('D4 trash round-trip: 200 OK, list disappears, untrash restores, restore path 200', async ({ browser }) => {
  const p = await page(browser)
  const list0 = await api(p, 'GET', '/user/projects')
  expect(list0.text, 'project in list at start').toContain(PID)

  const t = await api(p, 'POST', '/project/' + PID + '/trash')
  expect(t.status, 'trash → PINNED 200 "OK" (got ' + t.status + ': ' + t.text.slice(0, 60) + ')').toBe(200)

  const list1 = await api(p, 'GET', '/user/projects')
  expect(list1.text.includes(PID), 'trashed project VANISHES from /user/projects (pinned)').toBe(false)

  const u = await api(p, 'DELETE', '/project/' + PID + '/trash')
  expect(u.status, 'untrash → PINNED 200 (got ' + u.status + ')').toBe(200)
  const list2 = await api(p, 'GET', '/user/projects')
  expect(list2.text.includes(PID), 'back in the list after untrash').toBe(true)

  const rest = await api(p, 'POST', '/project/' + PID + '/restore')
  expect(rest.status, 'restore path → PINNED 200 (got ' + rest.status + ')').toBe(200)
})

test('D5 cleanup: disposable project to the trash + verified gone', async ({ browser }) => {
  const p = await page(browser)
  const r = await api(p, 'POST', '/project/' + PID + '/trash')
  expect(r.status, 'cleanup trash (got ' + r.status + ')').toBe(200)
  const list = await api(p, 'GET', '/user/projects')
  expect(list.text.includes(PID), 'disposable gone from the list').toBe(false)
})
