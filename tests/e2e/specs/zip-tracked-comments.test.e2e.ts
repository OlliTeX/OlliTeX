/**
 * e2e-zip-tracked-comments — owner-todo 4: ZIP import/export, tracked changes
 * and comments (templates EXCLUDED per owner scope).
 *
 * VERDICT (live-verified 2026-11-16, ag-v57 world A :7420):
 *
 *  1. THE USER FACING SINGLE-PROJECT ZIP ROUTE IS ABSENT in this build.
 *     The File-menu "Download as source (.zip)" anchor points at
 *     /project/:pid/download/zip (frontend download-project.tsx) — that
 *     route 404s to the "Page Not Found" HTML (live-verified, in-session).
 *     FLAG (owner): the editor's zip-export affordance is DEAD in this
 *     build — re-arm below (leg A fails green if the route starts
 *     returning a real zip).
 *
 *  2. THE ADMIN MULTI-PROJECT ZIP IS BEST-EFFORT PLACEHOLDER CONTENT:
 *     GET /project/download/zip?project_ids=… → 200,
 *     Content-Type application/zip, Content-Disposition
 *     attachment; filename="Overleaf Projects (N items).zip", one
 *     "<safe name>.zip" entry per project, and each inner zip holds a
 *     single "<name>/overleaf-export.txt" marker — NOT the project files,
 *     and NO tracked-changes / comments payloads (source:
 *     dlzip.go "heavy/non-deterministic streams are not gated; the
 *     contract is status + content-type + disposition"; live probe
 *     listed the inner zip entries). FLAG (owner): "ZIP export carries
 *     tracked changes + comments" is NOT implemented on the export side.
 *
 *  3. THE IMPORT SIDE IS REAL (files only):
 *     POST /project/new/upload — multipart `name` (required, zod) +
 *     `qqfile`; CSRF via x-csrf-token header ONLY (`_csrf` form field is
 *     REJECTED by the strict zod body schema: "Unrecognized key: _csrf at
 *     body" — live-verified). Contracts (Node oracles in newzip.go, all
 *     live-confirmed here):
 *       - no file part          → 400 {"success":false,"error":"invalid_upload_request"}
 *       - missing name          → 400 zod "Invalid input: expected string, received undefined at \"body.name\""
 *       - bad zip bytes         → 422 {"success":false,"error":"Invalid zip file"}
 *       - valid tex zip         → 200 {"success":true,"project_id":"<24hex>"}
 *                                 + project created with the zip's files
 *                                 (main.tex visible; \title used as name)
 *     Comments / tracked changes are server-side per-project data and are
 *     NOT representable in a source zip — IMPORT therefore cannot carry
 *     them by construction. FLAG (owner): a "zip round-trip of tracked
 *     changes + comments" is impossible in the current export placeholder
 *     + files-only import model; both ends need work (export: real files +
 *     a changes/comments payload; import: an import path for that payload).
 *
 *  4. TEMPLATES: excluded from this spec per owner scope (template zip
 *     handling lives in go/services/web/features/templates/*, a separate
 *     surface with its own contract).
 */
import { test, expect, Page } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import * as fs from 'fs'
import { execSync } from 'child_process'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

test.describe.configure({ mode: 'serial' })

async function csrf(page: Page): Promise<string> {
  const html = await (await page.request.get(BASE + '/')).text()
  return html.match(/<meta[^>]+name="ol-csrfToken"[^>]+content="([^"]+)"/)?.[1] || ''
}

// Node's `zip` CLI is not assumed; build zips with python3 (zipfile).
function buildZip(file: string, entries: Record<string, string>) {
  const py = `
import zipfile, json
entries = ${JSON.stringify(JSON.stringify(entries))}
entries = json.loads(entries)
with zipfile.ZipFile("${file.replace(/\\/g, '/')}", 'w') as z:
    for n, c in entries.items(): z.writestr(n, c)
`
  fs.writeFileSync('/tmp/__zipbuild.py', py)
  execSync(`python3 /tmp/__zipbuild.py`, { stdio: 'pipe' })
}

function listZip(file: string): string[] {
  const out = execSync(`python3 -c 'import zipfile,sys,json;print(json.dumps(zipfile.ZipFile(sys.argv[1]).namelist()))' "${file}"`, { encoding: 'utf8' }).trim()
  return JSON.parse(out)
}

function innerZipEntries(outer: string, innerName: string): string[] {
  const out = execSync(`python3 - <<PY
import zipfile, io, json
z = zipfile.ZipFile(${JSON.stringify(outer)})
zi = zipfile.ZipFile(io.BytesIO(z.read(${JSON.stringify(innerName)})))
print(json.dumps(zi.namelist()))
PY
`, { encoding: 'utf8' }).trim()
  return JSON.parse(out)
}

// The spec's canonical import zip (tex root + a plain file).
const TEX_ZIP = '/tmp/spec_tex.zip'
const JUNK_ZIP = '/tmp/spec_junk.zip'

test.beforeAll(() => {
  buildZip(TEX_ZIP, {
    'main.tex': '\\documentclass{article}\n\\title{E2E Zip Import Probe}\n\\begin{document}\nZip round-trip sentinel: ZIPTXT-4242\n\\end{document}\n',
    'notes.txt': 'plain text sentinel',
  })
  fs.writeFileSync(JUNK_ZIP, 'this is not a zip at all')
})

test.describe('zip import/export — tracked changes + comments (templates excluded)', () => {
  test('A: user single-project zip route is ABSENT in this build (404 HTML page)', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const pid = await createBlankProject(page)
    try {
      const r = await page.request.get(BASE + '/project/' + pid + '/download/zip')
      // Re-arm: if the route gains a real zip implementation (the owner's
      // requested surface), this assert fails and the spec is revisited.
      expect(r.status(), 'route should be 404 until the feature lands').toBe(404)
      expect(r.headers()['content-type'] || '').toMatch(/text\/html/i)
      const body = await r.text()
      expect(body).toMatch(/Not Found|Page Not Found/i)
    } finally {
      await page.request.delete(BASE + '/project/' + pid, { headers: { 'x-csrf-token': await csrf(page) } }).catch(() => {})
    }
  })

  test('B: admin multi-zip contract — 200 + zip + per-project nested zip (placeholder content, NO tracked changes/comments payload)', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const tok = await csrf(page)
    const pid = await createBlankProject(page)
    try {
      const r = await page.request.get(BASE + '/project/download/zip?project_ids=' + pid)
      expect(r.status()).toBe(200)
      expect(r.headers()['content-type'] || '').toMatch(/application\/zip/i)
      expect(r.headers()['content-disposition'] || '').toMatch(/attachment;\s*filename="Overleaf Projects \(1 items\)\.zip"/i)
      const outer = '/tmp/spec_admin.zip'
      fs.writeFileSync(outer, await r.body())
      const names = listZip(outer)
      expect(names, 'one inner zip per project').toHaveLength(1)
      expect(names[0]).toMatch(/\.zip$/i)
      // Pinned: the inner zip is a placeholder — no project files, and
      // explicitly NO tracked-changes / comments payloads (product gap;
      // FLAGGED owner in the header). Re-arm: if real files or a
      // comments/tracked-changes entry appear, fail loudly.
      const inner = innerZipEntries(outer, names[0])
      expect(inner.join('|'), 'no comment/tracked-changes payload in export').not.toMatch(/comment|tracked|annotation|change/i)
      expect(inner.join('|')).not.toMatch(/\.tex$/i)
      expect(inner.length, 'placeholder marker only').toBeGreaterThanOrEqual(1)
    } finally {
      await page.request.delete(BASE + '/project/' + pid, { headers: { 'x-csrf-token': tok } }).catch(() => {})
    }
  })

  test('C: upload no-file → 400 invalid_upload_request', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const tok = await csrf(page)
    const r = await page.request.post(BASE + '/project/new/upload', { multipart: { name: 'nofile' }, headers: { 'x-csrf-token': tok } })
    expect(r.status()).toBe(400)
    const j = await r.json().catch(() => ({}))
    expect(j?.success).toBe(false)
    expect(j?.error).toBe('invalid_upload_request')
  })

  test('D: upload missing name → 400 zod body.name (strict schema; _csrf NOT a form field)', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const tok = await csrf(page)
    const r = await page.request.post(BASE + '/project/new/upload', {
      multipart: { qqfile: { name: 'j.zip', mimeType: 'application/zip', buffer: fs.readFileSync(JUNK_ZIP) } },
      headers: { 'x-csrf-token': tok },
    })
    expect(r.status()).toBe(400)
    const t = (await r.text())
    expect(t).toMatch(/body\.name/)
  })

  test('E: upload bad zip → 422 Invalid zip file', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const tok = await csrf(page)
    const r = await page.request.post(BASE + '/project/new/upload', {
      multipart: { name: 'junk', qqfile: { name: 'junk.zip', mimeType: 'application/zip', buffer: fs.readFileSync(JUNK_ZIP) } },
      headers: { 'x-csrf-token': tok },
    })
    expect(r.status()).toBe(422)
    const j = await r.json().catch(() => ({}))
    expect(j?.success).toBe(false)
    expect(j?.error).toBe('Invalid zip file')
  })

  test('F: upload valid tex zip → 200 success + project_id + main.tex imported (files carry; comments/tracked changes do not by construction)', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const tok = await csrf(page)
    const r = await page.request.post(BASE + '/project/new/upload', {
      multipart: { name: 'ziptest', qqfile: { name: 'ziptest.zip', mimeType: 'application/zip', buffer: fs.readFileSync(TEX_ZIP) } },
      headers: { 'x-csrf-token': tok },
    })
    expect(r.status()).toBe(200)
    const j = await r.json().catch(() => ({}))
    expect(j?.success).toBe(true)
    const pid = String(j?.project_id || '')
    expect(pid).toMatch(/^[0-9a-f]{24}$/i)
    try {
      await page.goto(BASE + '/editor/' + pid, { waitUntil: 'load' })
      await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
      // zip file landed as a project file
      await expect(page.getByText('main.tex').first()).toBeVisible({ timeout: 15_000 })
      // \title drives the project name (Node contract: getTitleFromTexContent)
      const body = await page.evaluate(() => document.body.innerText || '')
      expect(body, 'title from tex').toMatch(/E2E Zip Import Probe/i)
    } finally {
      await page.request.delete(BASE + '/project/' + pid, { headers: { 'x-csrf-token': tok } }).catch(() => {})
    }
  })
})
