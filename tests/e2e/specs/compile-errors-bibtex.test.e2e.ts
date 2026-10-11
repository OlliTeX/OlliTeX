/**
 * compile-errors-bibtex.test.e2e.ts — board TODO-8593857d: COMPILE failure
 * UX + REFERENCES (bibtex two-pass) round-trip.
 *
 * PLANE FACT (proven 2026-10-11, board TODO-ae4d9982): FIXED ag-v53:
 * the BUILD reads the Go docstore, which is synced from the Yjs room only
 * when an editor SESSION disconnects (room persist). While the editing
 * session is open, builds compile the PRE-EDIT content (stale, for 120s+ of
 * polling). Therefore every build assertion in this suite runs AFTER the
 * editing session ended (deterministic on this stack). The freshness gap
 * itself is pinned as an explicit leg (T1b) so it can be flipped by a fix.
 *
 * Surface map (pinned from this revision, 2026-10-11):
 *   POST /project/:pid/doc        (entadd.go) — new doc entity (REST OK).
 *   Editor UI (.cm-content)       — THE content write plane (Yjs room).
 *   POST /project/:pid/compile    (compile.go) — build dispatch; response
 *       {status, outputFiles[], ...}; resources from getDocLines (docstore).
 *   GET <outputFiles url>         — output.pdf / build log artifacts.
 *   POST /project/:pid/settings   — compiler variant surface.
 *   pdftotext (host /usr/bin)     — rendered-text assertions (references).
 *
 * Failure-UX note: with the freshness gap in place, the editor's own
 * Recompile button shows a STALE (usually "successful") build — so the
 * panel error-surface leg is covered here via the WIRE contract (log
 * artifact carrying the error + status/outputs captured), and the exact UI
 * panel state will follow the ae4d9982 fix.
 */
import { test, expect } from '@playwright/test'
import * as fs from 'node:fs'
import { execFileSync } from 'node:child_process'
import { loginRobust, authFetch } from '../helpers/auth'
import { api, killProject, mongoEval } from '../parity/harness'
import { USER, ADMIN } from '../fixtures/credentials'

const B = 'http://127.0.0.1:7420'

let PID = ''
let D_MAIN = ''

/** Edit in a dedicated session, then END the session (room persist →
 * docstore sync). Deterministic on this stack; see header. */
async function editAndClose(browser: any, lines: string[], file?: 'references.bib' | 'new-entry') {
  const c = await browser.newContext()
  const p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
  await p.goto('/editor/' + PID, { waitUntil: 'commit' })
  const cm = p.locator('.cm-content').first()
  await cm.waitFor({ timeout: 45_000 })
  if (file === 'references.bib') {
    // open the bib doc in the tree (single click selects; double opens)
    const node = p.locator('text="references.bib"').last()
    await node.waitFor({ timeout: 20_000 })
    await node.dblclick()
    await p.waitForTimeout(1_500)
  }
  await cm.click()
  await p.keyboard.press('ControlOrMeta+a')
  await p.keyboard.press('Delete')
  await p.waitForTimeout(300)
  await p.keyboard.type(lines.join('\n'), { delay: 4 })
  await p.waitForTimeout(2_500)
  const last = (lines.slice().reverse().find((l: string) => l.trim().length > 0) || '').trim()
  // flush gate on the LIVE read plane (docdl) before we rely on the persist
  if (last) {
    const did = file === 'references.bib' ? bibDidOf() : D_MAIN
    for (let i = 0; i < 25 && i >= 0; i++) {
      const r = await p.request.get(`${B}/project/${PID}/doc/${did}/download`)
      if (r.status() === 200 && (await r.text()).includes(last)) break
      await p.waitForTimeout(500)
    }
  }
  await c.close() // ← room disconnect → docstore persist (THE sync trigger)
  await new Promise((res) => setTimeout(res, 1_500))
}

let BIB_DID = ''
function bibDidOf(): string {
  return BIB_DID
}

function createBibEntity(page: any) {
  return (async () => {
    const c = await api(page, 'POST', '/project/' + PID + '/doc', { name: 'references.bib' })
    if (c.status() === 200) {
      BIB_DID = String((await c.json().catch(() => ({})))?._id || '')
      expect(BIB_DID, 'addDoc references.bib').toMatch(/^[0-9a-f]{24}$/)
    } else {
      expect(c.status(), 'bib entity already exists → 400').toBe(400)
      BIB_DID = String(mongoEval(`
        const p = db.projects.findOne({ _id: ObjectId('${PID}') });
        const d = (p.rootFolder[0].docs || []).find(x => x.name === 'references.bib');
        print(d ? String(d._id) : '');
      `) || '')
      expect(BIB_DID, 'references.bib present').toMatch(/^[0-9a-f]{24}$/)
    }
  })()
}

/** Compile in a clean session (no editor open) → {response, log}. */
async function compileClosed(browser: any, tag: string) {
  const c = await browser.newContext()
  const p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
  const r = await authFetch(p.context(), p, 'POST', `/project/${PID}/compile`, {
    projectId: PID, compileId: 'cbx-' + tag + '-' + Date.now(), mainFile: 'main.tex',
  })
  let j: any = {}
  try { j = await r.json() } catch { /* non-json */ }
  const logU = (j?.outputFiles || []).find((f: any) => String(f?.path || '').endsWith('.log'))
  let log = ''
  if (logU) {
    const lr = await p.request.get(B + (logU.url || ''))
    log = await lr.text().catch(() => '')
  }
  const pdfU = (j?.outputFiles || []).find((f: any) => String(f?.path || '') === 'output.pdf')?.url || ''
  await c.close()
  return { status: r.status(), j, log, pdfUrl: pdfU }
}

function getBuf(browser: any, url: string) {
  return (async () => {
    const c = await browser.newContext()
    const p = await c.newPage()
    // session-authenticated URL (/user/:uid/...): fetch in a logged-in
    // context (anonymous → the HTML login page!)
    await loginRobust(p, USER.email, USER.password)
    const u = url.startsWith('http') ? url : B + url
    const out = await p.request.get(u).then(async (r) => ({ status: r.status(), buf: await r.body() }))
    await c.close()
    return out
  })()
}

function pdftext(path: string): string {
  try {
    return String(execFileSync('/usr/bin/pdftotext', [path, '-'], { encoding: 'utf8', timeout: 30_000 }))
  } catch {
    return ''
  }
}

test.describe('compile failure + bibtex references (disconnected-session builds)', () => {
  test.describe.configure({ mode: 'serial' })

  test('setup: project + main.tex doc id', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    const r = await api(page, 'POST', '/project/new', { projectName: 'e2e-compile-bib-v2' })
    expect(r.status(), 'create').toBeLessThan(300)
    PID = String((await r.json())?.project_id || '')
    expect(PID).toMatch(/^[0-9a-f]{24}$/)
    D_MAIN = String(
      mongoEval(`
        const p = db.projects.findOne({ _id: ObjectId('${PID}') });
        const d = (p.rootFolder[0].docs || []).find(x => x.name === 'main.tex');
        print(d ? String(d._id) : '');
      `) || '',
    )
    expect(D_MAIN, 'main.tex doc id').toMatch(/^[0-9a-f]{24}$/)
  })

  test('T1 failed build (freshened): log carries the LaTeX error; capture the failure shape', async ({ browser, page }) => {
    await editAndClose(browser, [
      '\\documentclass{article}',
      '\\begin{document}',
      '\\undefinedcmd_olpsint x',
      '\\end{document}',
    ])
    const { status, j, log, pdfUrl } = await compileClosed(browser, 'broken')
    expect(status, 'dispatch <500').toBeLessThan(500)
    // DETERMINISTIC pin: the error is in the build log.
    expect(log, 'log carries the undefined control sequence error').toMatch(/Undefined control sequence|undefinedcm/i)
    // capture the honest shape (rolling outputs / status) as evidence — not
    // asserted brittle, so the pin survives an ae4d9982-style fix.
    console.log('T1_SHAPE status=' + j?.status + ' http=' + status + ' output.pdf=' + (pdfUrl || 'none') + ' files=' + JSON.stringify((j?.outputFiles || []).map((f: any) => f.path)).slice(0, 220))
  })

  test('T1b FRESHNESS (board ae4d9982): OPEN-session build may be stale (recorded); CLOSED-session build is deterministically fresh', async ({ browser }) => {
    // OPEN session: edit + compile while connected — TODAY the build MIGHT
    // still see the pre-edit content (room-persist timing; proven stale for
    // 120s+ in tmp-lagmeasure, yet once observed fresh ~20s in). Record the
    // open-session observation without a brittle pin (it is timing-dependent
    // in both directions until ae4d9982 lands a pre-build flush).
    const c = await browser.newContext()
    const p = await c.newPage()
    await loginRobust(p, USER.email, USER.password)
    await p.goto('/editor/' + PID, { waitUntil: 'commit' })
    const cm = p.locator('.cm-content').first()
    await cm.waitFor({ timeout: 45_000 })
    await cm.click()
    await p.keyboard.press('ControlOrMeta+a')
    await p.keyboard.press('Delete')
    await p.waitForTimeout(300)
    await p.keyboard.type('\\documentclass{article}\n\\begin{document}\n\\undefinedcmd_gap x\n\\end{document}', { delay: 4 })
    await p.waitForTimeout(2_000)
    const r = await authFetch(p.context(), p, 'POST', `/project/${PID}/compile`, {
      projectId: PID, compileId: 'gap-open-' + Date.now(), mainFile: 'main.tex',
    })
    let j: any = {}
    try { j = await r.json() } catch { /* */ }
    const logU = (j?.outputFiles || []).find((f: any) => String(f?.path || '').endsWith('.log'))
    let logOpen = ''
    if (logU) logOpen = await (await p.request.get(B + (logU.url || ''))).text().catch(() => '')
    const openFresh = /undefinedcmd_gap|Undefined control sequence/i.test(logOpen)
    await c.close()
    console.log('T1B open-session build saw the edit? ' + openFresh + ' status=' + j?.status)

    // ae4d9982 (FIXED ag-v53): the compile handler now forces the collab
    // room->docstore write-back before reading content — an OPEN-session
    // build MUST reflect the latest edit. Both directions asserted.
    expect(openFresh, 'ae4d9982 fixed: open-session build reflects the latest edit').toBe(true)

    // The DETERMINISTIC contract (pre-fix baseline, must stay true): after
    // the session ends, a build MUST reflect the latest content.
    const closed = await compileClosed(browser, 'gap-closed')
    const closedFresh = /undefinedcmd_gap|Undefined control sequence/i.test(closed.log)
    console.log('T1B closed-session build saw the edit? ' + closedFresh)
    expect(closedFresh, 'ae4d9982 contract: closed-session build reflects the latest edit').toBe(true)
  })

  test('T2 fix → green flip: fixed content + closed session → PDF present', async ({ browser }) => {
    await editAndClose(browser, [
      '\\documentclass{article}',
      '\\begin{document}',
      'Hello fixed build.',
      '\\end{document}',
    ])
    const { status, j, pdfUrl } = await compileClosed(browser, 'fixed')
    expect(status).toBeLessThan(500)
    expect(pdfUrl, 'output.pdf present after fix').toBeTruthy()
    const got = await getBuf(browser, pdfUrl)
    expect(got.status, 'pdf 200').toBe(200)
    expect(got.buf.subarray(0, 4).toString('latin1')).toBe('%PDF')
  })

  test('T3 bibtex two-pass: references RENDERED + no persistent undefined citation', async ({ browser, page }) => {
    await loginRobust(page, USER.email, USER.password)
    await createBibEntity(page)
    await editAndClose(browser, [
      '@article{smith2020,',
      '  author  = {Smith, A. and Doe, B.},',
      '  title     = {A Great Paper on E2E},',
      '  journal   = {Journal of Pinned Behavior},',
      '  year      = {2020},',
      '}',
    ], 'references.bib')
    await editAndClose(browser, [
      '\\documentclass{article}',
      '\\begin{document}',
      'As shown in the classic two-pass example~\\cite{smith2020}.',
      '',
      '\\bibliographystyle{plain}',
      '\\bibliography{references}',
      '\\end{document}',
    ])

    const first = await compileClosed(browser, 'bib1')
    const second = await compileClosed(browser, 'bib2')
    const pdfUrl = second.pdfUrl || first.pdfUrl
    expect(pdfUrl, 'output.pdf on the bibliography builds').toBeTruthy()
    const got = await getBuf(browser, pdfUrl)
    expect(got.status).toBe(200)
    fs.writeFileSync('/tmp/cbx_ref.pdf', got.buf)
    const text = pdftext('/tmp/cbx_ref.pdf')
    expect(text, 'rendered reference (author) in the PDF').toMatch(/Smith|Doe/)
    expect(text, 'citation marker [1]').toMatch(/\[1\]/)
    const finalLog = second.log || first.log
    expect(finalLog, 'no "Citation `smith2020\' undefined" persists after the passes').not.toMatch(/Citation `?smith2020'? undefined/)
  })

  test('T4 bib edit → recompile dance: the late entry resolves', async ({ browser }) => {
    // extend the existing bib (append in a single doc write) + cite the new key
    await editAndClose(browser, [
      '@article{smith2020,',
      '  author  = {Smith, A. and Doe, B.},',
      '  title     = {A Great Paper on E2E},',
      '  journal   = {Journal of Pinned Behavior},',
      '  year      = {2020},',
      '}',
      '@article{jones2021,',
      '  author  = {Jones, C.},',
      '  title     = {Second Entry Late Add},',
      '  journal   = {Late Add Review},',
      '  year      = {2021},',
      '}',
    ], 'references.bib')
    await editAndClose(browser, [
      '\\documentclass{article}',
      '\\begin{document}',
      'Classic~\\cite{smith2020} plus the late addition~\\cite{jones2021}.',
      '',
      '\\bibliographystyle{plain}',
      '\\bibliography{references}',
      '\\end{document}',
    ])
    const a = await compileClosed(browser, 'bib3')
    const b = await compileClosed(browser, 'bib4')
    const pdfUrl = b.pdfUrl || a.pdfUrl
    expect(pdfUrl, 'pdf after the bib-edit dance').toBeTruthy()
    const got = await getBuf(browser, pdfUrl)
    expect(got.status).toBe(200)
    fs.writeFileSync('/tmp/cbx_ref2.pdf', got.buf)
    const text = pdftext('/tmp/cbx_ref2.pdf')
    expect(text, 'both entries rendered').toMatch(/Smith/)
    expect(text, 'the late entry rendered').toMatch(/Jones/)
    const logT = b.log || a.log
    expect(logT, 'no undefined jones2021 after the dance').not.toMatch(/Citation `?jones2021'? undefined/)
  })

  test('T5 compiler variant surface: decisive accept/reject; working variant still green', async ({ browser, page }) => {
    await loginRobust(page, USER.email, USER.password)
    const s = await api(page, 'POST', '/project/' + PID + '/settings', { compiler: 'XeLaTeX' })
    const st = s.status()
    expect(st, 'compiler settings surface decisive (no silent 5xx); got ' + st).not.toBe(500)
    if (st < 300) {
      const cur = String(mongoEval(`
        print(db.projects.findOne({ _id: ObjectId('${PID}') }).compiler);
      `) || '')
      expect(cur, 'project.compiler changed').toBeTruthy()
      const { j, log, pdfUrl } = await compileClosed(browser, 'xelatex')
      const pdf = await getBuf(browser, pdfUrl).catch(() => null)
      console.log('T5_XE status=' + j?.status + ' pdf=' + (pdfUrl || 'none') + ' logtail=' + log.slice(-200).replace(/\n/g, ' | '))
      if (pdfUrl && pdf) expect(pdf.status, 'xe pdf 200').toBe(200)
      const back = await api(page, 'POST', '/project/' + PID + '/settings', { compiler: 'pdfLaTeX' })
      expect(back.status(), 'revert to pdfLaTeX').toBeLessThan(300)
      const fin = await compileClosed(browser, 'revert')
      expect(fin.pdfUrl, 'reverted baseline build green').toBeTruthy()
    } else {
      console.log('T5_XE_REJECTED=' + st)
      const fin = await compileClosed(browser, 'stillgreen')
      expect(fin.pdfUrl, 'baseline still green after a rejected variant').toBeTruthy()
    }
  })

  test('cleanup: admin trashes + deletes the suite project', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await killProject(page, PID).catch(() => {})
  })
})
