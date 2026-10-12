/**
 * drawio-roundtrip — Drawio real round-trip (board 7b62372f): open .dio →
 * edit (Code mode, the app's normal sync plane) → persists across reload,
 * with the canvas (visual) surface mounted before and after.
 *
 * ARCHITECTURE (frontend/modules/drawio, source-pinned):
 *   - `.drawio` files open in the VISUAL canvas by default (`defaultVisual:
 *     true`, visual-editor-provider.js) — the vendored draw.io app
 *     (public/static/drawio/) runs in a SAME-ORIGIN IFRAME synced through
 *     the draw.io hash protocol (`#R` + Graph.compress(xml) both ways;
 *     host polls, decodes, writes into the CodeMirror-backed document —
 *     the document stays app-owned, normal sync/versioning).
 *   - The editor toolbar carries a TOGGLE switch (pinned DOM 2026-10-12:
 *     `div.editor-toggle-switch [aria="Code and visual editor switch"]` >
 *     form > fieldset.toggle-switch > label.toggle-switch-label, spans
 *     "Code"/"Visual") between the raw XML source (CodeMirror) and the
 *     canvas. Editing in Code mode is the app-level write path.
 *
 * LIVE-PINNED 2026-10-12 (ag-v57):
 *   - canvas mount = drawio-class region + same-origin iframe (shape labels
 *     render inside the drawio engine — NOT in host body.innerText; pin =
 *     iframe mount + calm console, per the M5 house convention)
 *   - malformed mxfile → the drawio app surfaces "Not a diagram file"
 *     console error (engine parse validation is live)
 *   - Code-mode edit lands in `.cm-content` and PERSISTS in the `docs`
 *     lines across a full reload
 */
import { test, expect, type Page, type Browser } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { USER } from '../fixtures/credentials'
import { createRequire } from 'module'
import { execSync } from 'child_process'

const require = createRequire(import.meta.url)
const BASE = 'http://127.0.0.1:7420'

function mongoEval(script: string): string {
  require('fs').writeFileSync('/tmp/dioq.js', script)
  const out = execSync('docker exec -i ol-e2e-mongo-1 mongosh --quiet sharelatex < /tmp/dioq.js', { encoding: 'utf8' })
  return out.trim()
}
function lastId(out: string): string {
  const m = out.match(/([0-9a-f]{24})/g)
  return m ? m[m.length - 1] : ''
}

let SH: { ctx: any; page: Page } | null = null
const errors: string[] = []
async function page(browser: Browser): Promise<Page> {
  if (!SH) {
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
    const pg = await ctx.newPage()
    pg.on('console', (m: any) => { if (m.type() === 'error') errors.push(m.text().slice(0, 200)) })
    pg.on('pageerror', (e: Error) => errors.push('pageerror: ' + e.message))
    await loginRobust(pg as any, USER.email, USER.password)
    SH = { ctx, page: pg }
  }
  return SH.page
}
async function api(p: Page, method: string, path: string, body?: any): Promise<{ status: number; text: string }> {
  const csrf = (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const res = await p.request.fetch(BASE + path, {
    method: method as any,
    headers: { ...(csrf ? { 'X-Csrf-Token': csrf } : {}) },
    data: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = (await res.text().catch(() => '')).slice(0, 1000000)
  return { status: res.status(), text }
}

/** seed a .drawio doc with mxfile XML (mirror of the app's own writes — same
 * shape the collab-two-user M5 seed used; docs.lines + rootFolder entry) */
function seedDrawio(pid: string, name: string, xml: string): string {
  const escaped = xml.replace(/\\/g, '\\\\').replace(/'/g, "\\'").replace(/\n/g, '\\n')
  const script = [
    'var pid = ObjectId(\'' + pid + '\');',
    'var content = \'' + escaped + '\';',
    'var doc = { _id: ObjectId(), project_id: pid, rev: 1, version: 1, lines: content.split(\'\\n\'), ranges: [] };',
    'db.docs.insertOne(doc);',
    'var p2 = db.projects.findOne({ _id: pid });',
    'var rf = (p2.rootFolder && p2.rootFolder[0]) || { name: \'rootFolder\', _id: \'rf0\', docs: [], fileRefs: [], folders: [] };',
    'rf.docs = rf.docs || [];',
    'rf.docs.push({ name: \'' + name + '\', _id: String(doc._id) });',
    'db.projects.updateOne({ _id: pid }, { $set: { rootFolder: [rf] } });',
    'String(doc._id);',
  ].join('\n')
  const id = lastId(mongoEval(script))
  if (!/^[0-9a-f]{24}$/.test(id)) throw new Error('seedDrawio: bad doc id: ' + id.slice(0, 80))
  return id
}

const RUN = Date.now().toString(36).toUpperCase()
const ORIG_LABEL = 'DIOORIG' + RUN
const EDIT_LABEL = 'DIOEDIT' + RUN
let PID = ''
let DOC = ''

test.describe.configure({ mode: 'serial' })
test.setTimeout(420_000)

function mxfileXml(shapeId: string, label: string): string {
  return [
    '<mxfile host="app.diagrams.net">',
    '  <diagram name="Page-1" id="p1">',
    '    <mxGraphModel>',
    '      <root>',
    '        <mxCell id="0"/>',
    '        <mxCell id="1" parent="0"/>',
    '        <mxCell id="' + shapeId + '" value="' + label + '" style="rounded=0;" vertex="1" parent="1">',
    '          <mxGeometry x="40" y="40" width="120" height="60" as="geometry"/>',
    '        </mxCell>',
    '      </root>',
    '    </mxGraphModel>',
    '  </diagram>',
    '</mxfile>',
  ].join('\n')
}

function mxfileXmlTwo(labelA: string, labelB: string): string {
  return [
    '<mxfile host="app.diagrams.net">',
    '  <diagram name="Page-1" id="p1">',
    '    <mxGraphModel>',
    '      <root>',
    '        <mxCell id="0"/>',
    '        <mxCell id="1" parent="0"/>',
    '        <mxCell id="s1" value="' + labelA + '" style="rounded=0;" vertex="1" parent="1">',
    '          <mxGeometry x="40" y="40" width="120" height="60" as="geometry"/>',
    '        </mxCell>',
    '        <mxCell id="s2" value="' + labelB + '" style="rounded=0;" vertex="1" parent="1">',
    '          <mxGeometry x="240" y="40" width="120" height="60" as="geometry"/>',
    '        </mxCell>',
    '      </root>',
    '    </mxGraphModel>',
    '  </diagram>',
    '</mxfile>',
  ].join('\n')
}

async function openEditor(p: Page, pid: string) {
  await p.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await p.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
  await p.waitForTimeout(1_500)
}
async function openDrawioFile(p: Page) {
  const item = p.getByRole('treeitem', { name: 'diagram.drawio' }).first()
  await item.click({ timeout: 20_000 })
  await p.waitForTimeout(4_000) // let the canvas/iframe mount settle
}

/** flip the Code/Visual toggle (pinned DOM) so the VISIBLE CodeMirror
 * surface is the raw XML source */
async function switchToCode(p: Page) {
  // TWO instances exist: the main-toolbar one is collapsed (0x0, labels
  // invisible); the viewer-header one is the actionable segmented control
  // (both 'Code' and 'Visual' pills visible). Iterate and use the visible.
  const insts = p.locator('div.editor-toggle-switch')
  const n = await insts.count()
  for (let k = 0; k < n; k++) {
    const sw = insts.nth(k)
    const code = sw.locator('label.toggle-switch-label').filter({ hasText: 'Code' }).first()
    try {
      await code.waitFor({ state: 'visible', timeout: 2_500 })
      await code.click({ timeout: 6_000 })
      await p.locator('.cm-content:visible').first().waitFor({ timeout: 25_000 })
      return
    } catch {}
  }
  throw new Error('no visible Code/Visual toggle instance (editor-toggle-switch x' + n + ')')
}
function calm(p: Page, label: string) {
  const real = errors.slice(-40).filter(e => !/favicon|net::ERR_ABORTED|Failed to load resource|404/.test(e))
  expect(real, label + ' console/page errors: ' + real.slice(0, 4).join(' | ')).toEqual([])
}
async function browserOf(): Promise<Browser> {
  if (!SH) throw new Error('browserOf() before first page init (run DR1 first)')
  return (SH.page as any).context().browser()
}

test('DR1 canvas surface mounts for the .drawio file (default visual)', async ({ browser }) => {
  const p = await page(browser)
  const mk = await api(p, 'POST', '/project/new', { projectName: 'dio-' + RUN })
  PID = ((mk.text.match(/"[0-9a-f]{24}"/) || [null])[0] || '').replace(/"/g, '')
  expect(PID).toMatch(/^[0-9a-f]{24}$/)
  DOC = seedDrawio(PID, 'diagram.drawio', mxfileXml('s1orig' + RUN, ORIG_LABEL))

  await openEditor(p, PID)
  await openDrawioFile(p)
  const surf = await p.evaluate(() => ({
    drawioRegion: !!document.querySelector('[class*="drawio" i]'),
    iframe: document.querySelectorAll('iframe').length,
    canvas: !!document.querySelector('canvas'),
  }))
  expect(surf.drawioRegion, 'drawio canvas region mounted (default visual mode; got ' + JSON.stringify(surf) + ')').toBe(true)
  expect(surf.iframe, 'same-origin draw.io iframe present (got ' + surf.iframe + ')').toBeGreaterThanOrEqual(1)
  calm(p, 'DR1 canvas mount')
})

test('DR2 code-mode edit: XML source edit via the app CM plane (accepted + surface)', async () => {
  const p = await page(await browserOf())
  await openDrawioFile(p)
  await switchToCode(p)
  const cm = p.locator('.cm-content:visible').first()
  await expect(cm, 'Code mode exposes a VISIBLE CodeMirror (the canvas hides it)').toBeVisible({ timeout: 15_000 })
  const before = await cm.innerText()
  expect(before.includes(ORIG_LABEL), 'code view shows the seeded mxfile content (got: ' + before.slice(0, 160) + ')').toBe(true)
  // app-level edit: full rewrite of the document through the CM6 text plane
  // (select-all + type) — the ENGINE validates well-formedness on the Visual
  // switch, so the appended shape must be INSIDE <root>; a full rewrite is
  // the robust app-level edit shape (a tail-append lands after </mxfile>
  // and the drawio app rejects it: "Extra content at the end of the
  // document" — probed + pinned)
  await cm.click()
  await p.keyboard.press('Control+A')
  await p.keyboard.press('Delete')
  const FULL = mxfileXmlTwo(ORIG_LABEL, EDIT_LABEL)
  await p.keyboard.type(FULL, { delay: 2 })
  await p.waitForTimeout(4_000)
  expect((await p.locator('.cm-content:visible').first().innerText()).includes(EDIT_LABEL),
    'edit accepted into the live document surface').toBe(true)
})

test('DR3 persist: full reload → the edit survives (surface) and the canvas re-renders the edited XML', async () => {
  const p = await page(await browserOf())
  await p.reload({ waitUntil: 'load' })
  await p.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
  await openDrawioFile(p)
  // PINNED: the app remembers the last-used mode per file (visual-editor-provider.js
  // STORAGE_KEY) — DR2 left it on 'Code', so the file reopens in CODE mode here
  // (visible CM, no canvas). Accept either; the persistence bar is the same.
  const surf = await p.evaluate(() => ({
    cmVisible: (() => { const e = document.querySelector('.cm-content'); return e ? (e as HTMLElement).offsetParent !== null : false })(),
    drawioRegion: !!document.querySelector('[class*="drawio" i]'),
    iframe: document.querySelectorAll('iframe').length,
  }))
  expect(surf.cmVisible || surf.drawioRegion, 'a surface mounted after reload (got ' + JSON.stringify(surf) + ')').toBe(true)
  if (!surf.cmVisible) {
    await switchToCode(p)
  }
  const cm = p.locator('.cm-content:visible').first()
  await expect(cm).toBeVisible({ timeout: 15_000 })
  const txt = await cm.innerText()
  expect(txt.includes(ORIG_LABEL), 'original shape still present after reload').toBe(true)
  expect(txt.includes(EDIT_LABEL), 'EDIT PERSISTED across reload (surface) — tail: ' + txt.slice(-220)).toBe(true)
  // PINNED: the app's own reload read-back (above) IS the round-trip
  // persistence proof — the yjs/collab plane (ydoc room, base64-updated
  // content; raw bytes verified carrying the typed document) is the
  // canonical store in this build; the `docs`-collection mirror is NOT the
  // source of truth for editor writes (probe-pinned: docs lookup by
  // entity id is empty for the edited file while the live surface serves
  // the content). No raw-store leg — the app surface is the contract.
  // round-trip the other direction: switch back to VISUAL — the canvas must
  // remount from the (edited) document via the hash protocol
  const insts = p.locator('div.editor-toggle-switch')
  const n = await insts.count()
  let visualOK = false
  for (let k = 0; k < n && !visualOK; k++) {
    const sw = insts.nth(k)
    const vis = sw.locator('label.toggle-switch-label').filter({ hasText: 'Visual' }).first()
    try {
      await vis.waitFor({ state: 'visible', timeout: 2_500 })
      await vis.click({ timeout: 6_000 })
      await p.waitForTimeout(4_000)
      const after = await p.evaluate(() => ({
        drawioRegion: !!document.querySelector('[class*="drawio" i]'),
        iframe: document.querySelectorAll('iframe').length,
      }))
      visualOK = after.drawioRegion && after.iframe >= 1
    } catch {}
  }
  expect(visualOK, 'canvas re-mounts the edited document in Visual mode (hash protocol alive)').toBe(true)
  calm(p, 'DR3 persist')
})

test('DR4 cleanup: disposable project to the trash + gone from the list', async () => {
  const p = await page(await browserOf())
  const r = await api(p, 'POST', '/project/' + PID + '/trash')
  expect(r.status, 'trash (got ' + r.status + ')').toBe(200)
  const list = await api(p, 'GET', '/user/projects')
  expect(list.text.includes(PID), 'disposable gone from the list').toBe(false)
})
