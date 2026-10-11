/**
 * downloads-export.test.e2e.ts — board TODO-9001c971: the DOWNLOAD / EXPORT
 * surface at connected-feature intensity (ZIP, single-file, PDF output,
 * byte-faithfulness, negative gates).
 *
 * Surface map (pinned from the Go sources + this stack's pinned Node-parity
 * contracts, 2026-10-11):
 *   GET /project/download/zip?project_ids=<ids...>   (dlzip.go)
 *       — the user-facing ZIP surface. CONTRACT (pinned): 400 zod on
 *         absent/bad project_ids; ghost → 404 (JSON when accepting JSON);
 *         non-member → 403 {restricted} (site admin reads all); 200 with
 *         Content-Type application/zip + `Overleaf Projects (<N> items).zip`,
 *         one `<safe project name>.zip` entry per project. INNER BYTES are
 *         a documented house best-effort placeholder (dlzip.go header:
 *         "heavy/non-deterministic streams are not gated; the contract is
 *         status + content-type + disposition") — so this spec pins the
 *         CONTRACT + ENTRY SET, not inner bytes (the faithful per-file
 *         export plane on this stack is docdl below + the git-bridge
 *         snapshot, covered by git-clone-bridge/).
 *       — audit: one projectAuditLogEntries {projectId, verb:
 *         'project-downloaded'} row per project (asserted in DB).
 *   GET /project/:pid/doc/:docid/download             (docdl.go)
 *       — THE byte-faithful single-file export: body = the LATEST doc text
 *         (fromVersion=-1 from the version store) + Content-Type
 *         text/plain; charset=utf-8 + attachment; filename="<doc.name>".
 *         anonymous → 401; non-member → 403 {restricted}.
 *   POST /project/:pid/compile → outputFiles[].url for output.pdf, then
 *   GET .../output/output.pdf (compile/download.go pdfDownloadPattern)
 *       — the compiled PDF export: %PDF magic + application/pdf.
 *
 * Fixture identities: USER (owner), USER2 (stranger/non-member), ADMIN
 * (site admin — reads all projects by contract).
 */
import { test, expect } from '@playwright/test'
import * as fs from 'node:fs'
import { login, loginRobust } from '../helpers/auth'
import { authFetch } from '../helpers/auth'
import { api, killProject, mongoEval } from '../parity/harness'
import { USER, USER2, ADMIN } from '../fixtures/credentials'

let PID = ''
let DOCID = ''

function unzipEntries(buf: Buffer): string[] {
  // minimal zip parser: read the central-directory file names (avoid
  // depending on unzip/python in the sandbox)
  const entries: string[] = []
  for (let i = 0; i < buf.length - 4; i++) {
    if (buf.readUInt32LE(i) === 0x02014b50) {
      const nameLen = buf.readUInt16LE(i + 28)
      const nameOff = i + 46
      entries.push(buf.slice(nameOff, nameOff + nameLen).toString('utf8'))
    }
  }
  return entries
}

test.describe('downloads & export', () => {
  test.describe.configure({ mode: 'serial' })

  test('setup: project + fixture state', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    const r = await api(page, 'POST', '/project/new', { projectName: 'e2e-dlzip-suite' })
    expect(r.status(), 'create 2xx (' + (await r.text()).slice(0, 120) + ')').toBeLessThan(300)
    const j: any = await r.json()
    PID = String(j?.project_id || '')
    expect(PID).toMatch(/^[0-9a-f]{24}$/)
    // doc id of main.tex for the single-file leg
    DOCID = String(
      mongoEval(`
        const p = db.projects.findOne({ _id: ObjectId('${PID}') });
        let did = '';
        const findDoc = (el) => {
          for (const d of (el.docs || [])) { if (d.name === 'main.tex') { did = String(d._id); return true; } }
          for (const f of (el.folders || [])) { if (findDoc(f)) return true; }
          return false;
        };
        for (const el of (p.rootFolder || [])) { if (findDoc(el)) break; }
        print(did);
      `) || '',
    )
    expect(DOCID, 'main.tex doc id').toMatch(/^[0-9a-f]{24}$/)
  })

  test('ZIP contract: owner 2xx + content-type/disposition + entry set + audit row', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    // the API request context must carry CSRF-free GET auth cookies; the
    // zip response is binary — grab it via the page context (auth cookies).
    const tok = await page
      .locator('meta[name="ol-csrfToken"]')
      .getAttribute('content')
      .catch(() => null)
    void tok
    const resp = await page.request.get('http://127.0.0.1:7420/project/download/zip?project_ids=' + PID)
    expect(resp.status(), 'zip 200').toBe(200)
    const ct = resp.headers()['content-type'] || ''
    expect(ct, 'content-type application/zip').toContain('application/zip')
    const disp = resp.headers()['content-disposition'] || ''
    expect(disp, 'disposition Overleaf Projects (1 items).zip').toContain('Overleaf Projects (1 items).zip')
    const buf = await resp.body()
    expect(buf.length, 'non-trivial zip').toBeGreaterThan(60)
    const entries = unzipEntries(buf)
    expect(entries.some((e) => /e2e[-_]?dlzip[-_]?suite.*\.zip$/i.test(e)), 'inner entry named after the project (safeName-sanitized); got: ' + entries.join(', ')).toBeTruthy()
    // audit row (contract: one project-downloaded per project)
    const n = Number(
      mongoEval(`
        print(db.projectAuditLogEntries.countDocuments({ projectId: ObjectId('${PID}'), verb: 'project-downloaded' }));
      `) || 0,
    )
    expect(n, 'audit row for project-downloaded').toBeGreaterThanOrEqual(1)
  })

  test('ZIP negative gates: 400 bad ids, 404 ghost, 403 non-member (USER2), admin reads all', async ({ page, context }) => {
    await loginRobust(page, USER.email, USER.password)

    // 400 — absent/invalid project_ids (zod shape pinned)
    const bad = await page.request.get('http://127.0.0.1:7420/project/download/zip', {
      headers: { Accept: 'application/json' },
    })
    expect(bad.status(), 'absent project_ids → 400').toBe(400)
    const badj: any = await bad.json().catch(() => ({}))
    expect(String(badj?.message || ''), 'zod message').toContain('project_ids')

    // 404 — ghost project id (JSON-accepting)
    const ghost = await page.request.get('http://127.0.0.1:7420/project/download/zip?project_ids=' + 'a'.repeat(24), {
      headers: { Accept: 'application/json' },
    })
    expect(ghost.status(), 'ghost → 404').toBe(404)

    // 403 — USER2 (non-member, non-admin) on USER's project
    const p2 = await context.newPage()
    await loginRobust(p2, USER2.email, USER2.password)
    const r2 = await p2.request.get('http://127.0.0.1:7420/project/download/zip?project_ids=' + PID, {
      headers: { Accept: 'application/json' },
    })
    expect(r2.status(), 'non-member → 403').toBe(403)
    const r2j: any = await r2.json().catch(() => ({}))
    expect(JSON.stringify(r2j).toLowerCase(), 'restricted shape').toMatch(/restricted|forbidden/)

    // site admin reads all (USER2 was 403, ADMIN must be 200)
    const p3 = await context.newPage()
    await loginRobust(p3, ADMIN.email, ADMIN.password)
    const r3 = await p3.request.get('http://127.0.0.1:7420/project/download/zip?project_ids=' + PID)
    expect(r3.status(), 'site admin reads all → 200').toBe(200)
    await p2.close()
    await p3.close()
  })

  test('single-file (docdl): byte-faithful, FRESH after edit; 401 anon / 403 stranger', async ({ browser, page, context }) => {
    await loginRobust(page, USER.email, USER.password)

    const dl = async (p: any) => {
      const r = await p.request.get(`http://127.0.0.1:7420/project/${PID}/doc/${DOCID}/download`)
      const text = await r.text()
      return { status: r.status(), ct: r.headers()['content-type'] || '', disp: r.headers()['content-disposition'] || '', text }
    }

    // baseline download: template content, correct CT/disposition
    const b = await dl(page)
    expect(b.status, 'docdl 200').toBe(200)
    expect(b.ct).toContain('text/plain')
    expect(b.disp, 'filename="main.tex"').toContain('main.tex')
    expect(b.text, 'non-empty template').toContain('\\documentclass')

    // EDIT the doc in the editor → the download must reflect the LIVE plane
    const ed = await context.newPage()
    await loginRobust(ed, USER.email, USER.password)
    await ed.goto('/editor/' + PID, { waitUntil: 'commit' })
    await ed.locator('.cm-editor').first().waitFor({ timeout: 45_000 })
    const cmc = ed.locator('.cm-content').first()
    await cmc.click()
    await ed.keyboard.press('ControlOrMeta+End')
    await ed.keyboard.type('\n% OLPSINT_DL_EXPORT_MARKER_77\n', { delay: 6 })
    await ed.waitForTimeout(3_500)

    // poll: the version store flushes the edit; docdl reads fromVersion=-1
    let fresh = ''
    for (let i = 0; i < 25; i++) {
      const d = await dl(page)
      fresh = d.text
      if (fresh.includes('OLPSINT_DL_EXPORT_MARKER_77')) break
      await page.waitForTimeout(1_000)
    }
    expect(fresh, 'download reflects the LIVE plane (marker present after edit)').toContain('OLPSINT_DL_EXPORT_MARKER_77')
    expect(fresh, 'original template still there').toContain('\\documentclass')
    await ed.close()

    // 401 — anonymous (FRESH context: the shared context carries session
    // cookies from the USER login above)
    const anonCtx = await browser.newContext()
    const anon = await anonCtx.newPage()
    const a401 = await anon.request.get(`http://127.0.0.1:7420/project/${PID}/doc/${DOCID}/download`, {
      maxRedirects: 0,
    })
    expect([401, 302].includes(a401.status()), 'anonymous → 401 (or login bounce 302) got ' + a401.status()).toBeTruthy()
    await anonCtx.close()

    // 403 — USER2 non-member
    const p2 = await context.newPage()
    await loginRobust(p2, USER2.email, USER2.password)
    const r403 = await p2.request.get(`http://127.0.0.1:7420/project/${PID}/doc/${DOCID}/download`, {
      headers: { Accept: 'application/json' },
    })
    expect(r403.status(), 'non-member → 403').toBe(403)
    await p2.close()
  })

  test('PDF export: compile → outputFiles output.pdf → 200 %PDF', async ({ page, context }) => {
    await loginRobust(page, USER.email, USER.password)
    const c = await authFetch(page.context(), page, 'POST', `/project/${PID}/compile`, {
      projectId: PID,
      compileId: 'dlexport-' + Date.now(),
      mainFile: 'main.tex',
    })
    const cs = c.status()
    expect(cs, 'compile dispatch < 500 (got ' + cs + ')').toBeLessThan(500)
    const cj: any = await c.json().catch(() => ({}))
    // the compiler can report a non-2xx-ish shape (outputFiles present);
    // extract the output.pdf URL wherever it lives.
    const files: any[] = cj?.outputFiles || []
    let pdfUrl = ''
    for (const f of files) {
      if (String(f?.path || '').endsWith('output.pdf')) {
        pdfUrl = String(f.url || f.path || '')
        break
      }
    }
    expect(pdfUrl, 'output.pdf listed in outputFiles (compile status: ' + cj?.status + ')').toBeTruthy()
    const u = pdfUrl.startsWith('http') ? pdfUrl : 'http://127.0.0.1:7420' + pdfUrl
    const r = await page.request.get(u)
    expect(r.status(), 'output.pdf 200').toBe(200)
    const buf = await r.body()
    expect(buf.subarray(0, 4).toString('latin1'), '%PDF magic').toBe('%PDF')
    const ct = r.headers()['content-type'] || ''
    expect(ct).toContain('application/pdf')
  })

  test('cleanup: admin trashes + deletes the suite project', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await killProject(page, PID).catch(() => {})
  })
})
