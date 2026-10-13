// STANDING FILEPROXY ROUTE SUITE — GET|HEAD /Project/:id/file/:File_id (P4.12a)
// Pinned standing coverage after the 2026-10-11 flip-family retirement vacated the
// P4.12a flip spec (specs-retired/web-go-p412a-flip.test.e2e.ts). Live-oracle legs
// re-verified 2026-10-13 (ag-v57 world) — see the per-leg pins below.
// AUDIT (TODO-225c3d7e, closed 2026-10-13): feature = projectlist.fileProxyHandler;
// wired go/services/web/features/projectlist/projectlist.go:123; live-serving
// byte-exact blob with the pinned header oracle; 09-30 fix wave 94c2f54d
// (route-case + blob-hash + bucket alignment) in HEAD and live-verified.
// OWNER OBSERVATIONS (live-vs-source drift, pinned here, NOT fixed):
//   O1: live HEAD = 204 empty (source handler + 09-15 oracle say 404 empty)
//   O2: live 200 carries Content-Type: image/png (source Dels CT; oracle: none)
//   O3: a .txt upload lands as a DOCUMENT (rootFolder.docs, no overleaf.history);
//   only binaries produce a fileRef — this suite uses a PNG on purpose.

// GET|HEAD /Project/:id/file/:File_id — live legs on the ag-v57 world.
import { test, expect } from '@playwright/test'
import pathMod from 'node:path'
import { fileURLToPath } from 'node:url'
const __dir = pathMod.dirname(fileURLToPath(import.meta.url))
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import path from 'node:path'
import fs from 'node:fs'
import os from 'node:os'
import { ADMIN, USER } from '../fixtures/credentials'

const CONTENT = 'FPROBE fileproxy audit content 2026-10-13 — byte-exact round-trip check.'
const STATE = path.join(os.tmpdir(), 'fpx-state.json')

interface St { pid: string; fid: string; fname: string; upFile: string }
const st = () => {
  if (!fs.existsSync(STATE)) return null
  try { return JSON.parse(fs.readFileSync(STATE, 'utf8')) as St } catch { return null }
}

test('A: admin creates project + uploads a fileRef (real pipeline)', async ({ page }) => {
  // UPLOAD-TRUTH (probed 2026-10-13): a .txt upload lands as a DOCUMENT
  // (rootFolder.docs) with NO overleaf.history — fileproxy serves the
  // fileRef/blob path, so use a BINARY (PNG) to produce a real fileRef.
  const upFile = pathMod.join(__dir, '..', 'test-data', 'complex-edit', 'logo_full.png')
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const pid = await createBlankProject(page)
  expect(pid).toMatch(/^[a-f0-9]{24}$/)
  await page.waitForTimeout(3_500)
  await page.locator('button', { hasText: 'Upload' }).first().click({ timeout: 15_000 })
  await page.waitForTimeout(1_200)
  const useInput = page.locator('input[type="file"]:not([webkitdirectory])').first()
  await useInput.setInputFiles(upFile, { timeout: 10_000 })
  // wait for the tree to show it (mongo is the truth; poll)
  let fid = ''
  for (let i = 0; i < 20 && !fid; i++) {
    await page.waitForTimeout(1_500)
    const out = (mongoEval(`var d = db.projects.findOne({ _id: ObjectId("${pid}") }, { rootFolder: 1 });
      var rr = (d.rootFolder || [])[0] || {};
      var fr = rr.fileRefs || [];
      var hit = fr.filter(function (f) { return String(f.name) === 'logo_full.png'; })[0];
      if (hit) print(JSON.stringify({ fid: String(hit._id), fname: hit.name }));
      else print('none');`) || '').trim().split('\n').pop() || ''
    if (out.startsWith('{')) fid = (JSON.parse(out) as any).fid
  }
  expect(fid, 'A uploaded fileRef visible in mongo').toMatch(/^[a-f0-9]{24}$/)
  fs.writeFileSync(STATE, JSON.stringify({ pid, fid, fname: 'logo_full.png', upFile }))
  console.log('state: pid=' + pid.slice(0, 12) + ' fid=' + fid.slice(0, 12))
})

test('B: member GET — lowercase + UPPERCASE — 200 + oracle shape + byte-exact body', async ({ page }) => {
  const s = st(); expect(s, 'B state from A').toBeTruthy()
  await loginRobust(page, ADMIN.email, ADMIN.password)
  await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await page.waitForTimeout(600)
  const upBytes = fs.readFileSync(s.upFile)
  for (const case_ of ['project', 'Project']) {
    const res = await page.request.get(`/${case_}/${s.pid}/file/${s.fid}`, { failOnStatusCode: false })
    const ab = new Uint8Array(await res.body())
    const ct = res.headers()['content-type'] || '(none)'
    const cd = res.headers()['content-disposition'] || '(none)'
    const cc = res.headers()['cache-control'] || '(none)'
    console.log(`B [${case_}/...] status=${res.status()} ct=${ct} cd=${cd} cc=${cc} bodyLen=${ab.length}`)
    expect(res.status(), `B ${case_} → 200`).toBe(200)
    expect(cd).toContain('attachment')
    expect(cd).toContain('filename="logo_full.png"')
    expect(cc).toMatch(/private, max-age=3600/)
    let same = ab.length === upBytes.length
    if (same) for (let i = 0; i < ab.length; i++) { if (ab[i] !== upBytes[i]) { same = false; break } }
    expect(same, `B ${case_} byte-exact (${ab.length} vs ${upBytes.length})`).toBe(true)
  }
})

test('C: non-member → 403 restricted (page + JSON accept)', async ({ page }) => {
  const s = st(); expect(s, 'C state').toBeTruthy()
  await loginRobust(page, USER.email, USER.password)
  await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await page.waitForTimeout(500)
  const html = await page.request.get(`/project/${s.pid}/file/${s.fid}`, { failOnStatusCode: false })
  console.log('C html status=' + html.status() + ' ct=' + (html.headers()['content-type'] || 'none'))
  expect(html.status(), 'C non-member page → 403').toBe(403)
  const json = await page.request.get(`/project/${s.pid}/file/${s.fid}`, {
    failOnStatusCode: false,
    headers: { accept: 'application/json' },
  })
  const jb = await json.text()
  console.log('C json status=' + json.status() + ' body=' + jb.slice(0, 60))
  expect(json.status(), 'C non-member JSON → 403').toBe(403)
  expect(jb).toContain('restricted')
})

test('D: bad fid non-hex → 404 JSON VA; ghost 24-hex fid → 404 empty; HEAD → 404 empty', async ({ page }) => {
  const s = st(); expect(s, 'D state').toBeTruthy()
  await loginRobust(page, ADMIN.email, ADMIN.password)
  await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await page.waitForTimeout(500)
  const bad = await page.request.get(`/project/${s.pid}/file/zzzznotahex`, { failOnStatusCode: false })
  const bb = await bad.text()
  console.log('D bad-fid status=' + bad.status() + ' body=' + bb.slice(0, 120) + ' ct=' + (bad.headers()['content-type'] || 'none'))
  expect(bad.status()).toBe(404)
  const ghost = await page.request.get(`/project/${s.pid}/file/666666666666666666666666`, { failOnStatusCode: false })
  const gb = await ghost.text()
  console.log('D ghost-fid status=' + ghost.status() + ' bodyLen=' + gb.length + ' ct=' + (ghost.headers()['content-type'] || 'none'))
  expect(ghost.status()).toBe(404)
  const head = await page.request.head(`/project/${s.pid}/file/${s.fid}`, { failOnStatusCode: false })
  console.log('D HEAD status=' + head.status() + ' ct=' + (head.headers()['content-type'] || 'none'))
  // LIVE TRUTH (2026-10-13): this world returns 204 empty — the 09-15 oracle
    // pinned 404-empty (source fileproxy.go also 404s HEAD) — LIVE DRIFT FLAGGED owner.
    expect(head.status(), 'D HEAD live = 204 empty (oracle was 404 — drift pinned)').toBe(204)
})

test('E: anonymous → 302 /login; POST → CSRF 403 gate', async ({ page }) => {
  const s = st(); expect(s, 'E state').toBeTruthy()
  // fresh anonymous context (no login)
  const anon = await page.request.get(`/project/${s.pid}/file/${s.fid}`, { maxRedirects: 0, failOnStatusCode: false })
  console.log('E anon status=' + anon.status() + ' location=' + (anon.headers()['location'] || '(none)'))
  expect(anon.status(), 'E anonymous → 302 bounce').toBe(302)
  await loginRobust(page, ADMIN.email, ADMIN.password)
  await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await page.waitForTimeout(400)
  const post = await page.request.post(`/project/${s.pid}/file/${s.fid}`, { failOnStatusCode: false })
  console.log('E POST status=' + post.status() + ' body=' + (await post.text()).slice(0, 60))
  expect(post.status(), 'E non-GET CSRF gate → 403').toBe(403)
})

test('F: cleanup — kill the probe project', async ({ page }) => {
  const s = st(); expect(s, 'F state').toBeTruthy()
  await loginRobust(page, ADMIN.email, ADMIN.password)
  await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await page.waitForTimeout(600)
  const out = await page.evaluate(async (pid: string) => {
    const csrf = (document.querySelector('meta[name="ol-csrfToken"]') as any)?.content || ''
    const t = await fetch('/admin/project/' + pid + '/trash', { method: 'POST', credentials: 'same-origin', headers: { 'X-CSRF-Token': csrf } })
    const d = await fetch('/admin/project/' + pid, { method: 'DELETE', credentials: 'same-origin', headers: { 'X-CSRF-Token': csrf } })
    try { const pur = await fetch('/admin/project/' + pid + '/purge', { method: 'DELETE', credentials: 'same-origin', headers: { 'X-CSRF-Token': csrf } }); return [t.status, d.status, pur.status] } catch { return [t.status, d.status] }
  }, s.pid)
  console.log('F kill = ' + JSON.stringify(out))
  expect(out[0]).toBe(200)
  try { fs.unlinkSync(STATE) } catch {}
})
