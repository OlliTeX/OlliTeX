/**
 * collab-two-user — Q (owner 2026-10-10): the two-cooperator SAME-FILE
 * concurrency matrix.
 *
 * Two GENUINE accounts (USER = owner, USER2 = collaborator) in two
 * independent browser contexts open the SAME file at the same time and edit
 * concurrently. One-user two-tab patterns (collab-yjs, multifile-collab)
 * cannot exercise the cross-user privilege/collab path; this can.
 *
 * Matrix (per type: both marks converge in BOTH clients, no cross-file
 * leakage, no page/console errors, write-through persistence):
 *   M1 .tex    main.tex  — A top / B end (CM6)
 *   M2 .typst  q.typst   — A top / B end
 *   M3 .bib    q.bib     — A top / B end
 *   M4 .md     q.md      — Milkdown visual surface, both edit, both converge
 *   M5 .svg .tikz .drawio — A text-edits; B's surface reflects (live or
 *                           write-through), no crash
 *   M6 image  — A uploads a second png while B has the first open; B sees
 *               the new file, no crash
 *   M7 write-through — reload both, the M1 marks persist in both clients
 *
 * Collision policy: any divergence/crash = a real collab bug → fix in
 * source, then re-run to green (owner: "e2e cover + fix collisions").
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject, csrfToken } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { USER, USER2 } from '../fixtures/credentials'

test.setTimeout(600_000)

const RUN = 'q' + (Date.now() % 1_000_000)

// ---------------------------------------------------------------- helpers
const cmText = (page: any) =>
  page.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText)

async function cmTop(page: any): Promise<string> {
  await page.click('.cm-content')
  await page.keyboard.press('Control+Home')
  await page.waitForTimeout(600)
  return page.evaluate(() =>
    (document.querySelector('.cm-content') as HTMLElement).innerText.slice(0, 1500)
  )
}
async function cmEndText(page: any): Promise<string> {
  await page.click('.cm-content')
  await page.keyboard.press('Control+End')
  await page.waitForTimeout(600)
  const t = await page.evaluate(() =>
    (document.querySelector('.cm-content') as HTMLElement).innerText
  )
  return t.slice(-1500)
}
const typeAtEnd = async (page: any, mark: string) => {
  await page.click('.cm-content')
  await page.keyboard.press('Control+End')
  await page.keyboard.type(mark, { delay: 8 })
}
const typeAtTop = async (page: any, mark: string) => {
  await page.click('.cm-content')
  await page.keyboard.press('Control+Home')
  await page.keyboard.type(mark, { delay: 8 })
}

/** wait until `needle` is visible in the page body text (live surface) */
async function bodyHas(page: any, needle: string, budgetMs = 15_000): Promise<boolean> {
  const deadline = Date.now() + budgetMs
  for (;;) {
    const t = await page.evaluate(() => document.body.innerText).catch(() => '')
    if (t.includes(needle)) return true
    if (Date.now() > deadline) return false
  }
}

const errorLogs = new Map<any, string[]>()
function watch(page: any) {
  errorLogs.set(page, [])
  page.on('console', (m: any) => {
    if (m.type() === 'error') {
      const e = errorLogs.get(page)!
      e.push(m.text().slice(0, 200))
    }
  })
  page.on('pageerror', (e: Error) => errorLogs.get(page)!.push('pageerror: ' + e.message))
}
function assertCalm(page: any, label: string) {
  const real = (errorLogs.get(page) || []).filter(e =>
    !/favicon|net::ERR_ABORTED|Failed to load resource|404 \(Not Found\)/.test(e)
  )
  expect(real, `${label} console/page errors: ${real.slice(0, 4).join(' | ')}`).toEqual([])
}

/**
 * Seed a text file into the project (mirror of the app's own writes): a
 * doc in `docs` + a rootFolder tree entry. Returns the doc id (hex).
 */
function seedTextFile(pid: string, name: string, content: string): string {
  const escaped = content
    .replace(/\\/g, '\\\\')
    .replace(/'/g, "\\'")
    .replace(/\n/g, '\\n')
  const script = `
    var pid = ObjectId('${pid}');
    var doc = { _id: ObjectId(), project_id: pid, rev: 1, version: 1,
                lines: '${escaped}'.split('\\n'), ranges: [] };
    db.docs.insertOne(doc);
    var p = db.projects.findOne({ _id: pid });
    var entry = { name: '${name}', _id: String(doc._id) };
    var rf = (p.rootFolder && p.rootFolder[0]) || { name: 'rootFolder', _id: 'rf0', docs: [], fileRefs: [], folders: [] };
    rf.docs = rf.docs || []; rf.fileRefs = rf.fileRefs || []; rf.folders = rf.folders || [];
    rf.docs.push(entry);
    db.projects.updateOne({ _id: pid }, { $set: { rootFolder: [rf] } });
    String(doc._id);
  `
  const out = (mongoEval(script) || '').trim().split('\n').pop() as string
  if (!/^[0-9a-f]{24}$/.test(out)) throw new Error(`seedTextFile(${name}): bad doc id: ${out}`)
  return out
}

/**
 * Membership bootstrap for the concurrency matrix.
 *
 * WHY direct-DB: in this build the invite-accept path is a PINNED
 * Node-parity no-op ("observable contract ... single-use invite
 * consumption + sender contact; NO project refs write" — invite.go),
 * and `PUT /project/:id/users/:uid` 404s for non-members
 * (arrayFilters parity, collab.go) — i.e. the app has no new-member
 * path for a fresh project. The Go access path (access.go) reads
 * membership from the project doc's `collaberator_refs` /
 * `reviewer_refs` / `readOnly_refs` lists (bare ObjectIds). We seed
 * USER2 into `collaberator_refs` (= readAndWrite "INVITE" level —
 * exactly what a successful accept would have represented) and then
 * run two genuine sessions over the real collab transport. The Q
 * question — do two cooperators editing the SAME file converge without
 * collision — is answered on the app's real collab plane, not a mock.
 */
function grantCollaboratorDirect(pid: string, uid2: string): void {
  const out = mongoEval(
    `db.projects.updateOne({ _id: ObjectId('${pid}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } });
     print(db.projects.findOne({ _id: ObjectId('${pid}') }).collaberator_refs.length);`
  )
  const m = /\d+/.exec(out || '')
  expect(m && parseInt(m[0], 10) >= 1, 'USER2 present in collaberator_refs').toBeTruthy()
}

async function openFile(page: any, name: string) {
  let item = page.getByRole('treeitem', { name, exact: false }).first()
  try {
    await expect(item).toBeVisible({ timeout: 10_000 })
  } catch {
    // the file (seeded DB-side) may need one resync — reload and retry
    await page.reload({ waitUntil: 'load' })
    await page.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
    item = page.getByRole('treeitem', { name, exact: false }).first()
    await expect(item).toBeVisible({ timeout: 25_000 })
  }
  await item.click({ timeout: 15_000 })
  // Give the app time to mount WHATEVER surface this file type gets —
  // CM6 (text files), Milkdown (md visual), or a viewer (svg/tikz/drawio/
  // images, which legitimately have no editable surface). Gate on
  // "a surface mounted", not on any specific one.
  const mounted = async () => {
    for (const sel of ['.cm-content', '.milkdown', 'canvas', 'svg', '[class*="viewer"]', '[class*="drawio"]']) {
      try {
        if (await page.evaluate((s: string) => !!document.querySelector(s), sel)) return true
      } catch {}
    }
    return false
  }
  const deadline = Date.now() + 8_000
  for (;;) {
    if (await mounted()) break
    if (Date.now() > deadline) break
    await page.waitForTimeout(500)
  }
  await page.waitForTimeout(1_500)
}

async function editorReady(page: any, pid: string) {
  await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 60_000 })
  // The CM6 surface (.cm-content) mounts ONLY once a document tab is open —
  // a fresh session has no tabs, so gate on the editor chrome + file tree
  // instead, and let openFile() mount the surface per file.
  await page.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
  await page.waitForTimeout(1_500)
}

// ---------------------------------------------------------------- matrix
test.describe.configure({ mode: 'serial' })

test('Q — two-cooperator same-file concurrency matrix (tex/typst/bib/md/svg/tikz/drawio/image)', async ({ page, context, browser }) => {
  // ---- USER (owner) session -------------------------------------------
  await loginRobust(page, USER.email, USER.password)
  const pid = await createBlankProject(page)

  // ---- USER2 (collaborator) session — must exist BEFORE the invite ----
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page2 = await ctx2.newPage()
  watch(page)
  watch(page2)
  await loginRobust(page2, USER2.email, USER2.password)

  // sanity: USER2 fixture user exists; then invite + accept
  const uid2 = (
    (mongoEval(
      `var u = db.users.findOne({ email: '${USER2.email}' }); u ? String(u._id) : '';`
    ) || '')
      .trim()
      .split('\n')
      .pop() as string
  )
  expect(uid2, 'USER2 fixture user must exist').toMatch(/^[0-9a-f]{24}$/)
  grantCollaboratorDirect(pid, uid2)
  // USER2 must now see the project in their hub (Shared with you)
  await page2.goto('/hub', { waitUntil: 'domcontentloaded' })
  await page2.waitForTimeout(2_500)
  const hubTxt = await page2.evaluate(() => document.body.innerText)
  expect(hubTxt, 'USER2 hub lists the project row').toContain('e2e-smoke-project')

  // both clients into the project
  await editorReady(page, pid)
  await editorReady(page2, pid)

  // seed the matrix files (deterministic content)
  seedTextFile(pid, 'q.typst', '#align(left) {\nsuperscript:typst\n}')
  seedTextFile(pid, 'q.bib', '@article{greenwade93,\n  author = {Greenwade, J. H.},\n  title  = {Generating executable scientific articles},\n  year   = {1993}\n}')
  seedTextFile(pid, 'q.md', '# Q matrix\n\nfirst paragraph\n')
  seedTextFile(pid, 'q.svg', '<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100">\n  <rect width="40" height="40" fill="green"/>\n</svg>')
  seedTextFile(pid, 'q.tikz', '\\begin{tikzpicture}\n  \\draw (0,0) rectangle (2,2);\n\\end{tikzpicture}')
  seedTextFile(pid, 'q.drawio', '<mxfile host="app.diagrams.net">\n  <diagram name="Page-1" id="q1">U2lkZXI=</diagram>\n</mxfile>')
  await page.reload({ waitUntil: 'load' }).catch(() => {})
  await page.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
  await page2.reload({ waitUntil: 'load' }).catch(() => {})
  await page2.waitForSelector('[role="treeitem"]', { timeout: 90_000 })

  const bothMarks = async (a: any, b: any, mA: string, mB: string, label: string) => {
    // A sees B's mark AND B sees A's mark (either surface shows body text)
    const aSeesB = await bodyHas(a, mB, 20_000)
    const bSeesA = await bodyHas(b, mA, 20_000)
    expect(aSeesB, `${label}: A never saw B's mark`).toBeTruthy()
    expect(bSeesA, `${label}: B never saw A's mark`).toBeTruthy()
  }

  // ================= M1 .tex (main.tex) ================================
  const m1a = `%%Q${RUN}A_TEX`
  const m1b = `%%Q${RUN}B_TEX`
  await openFile(page, 'main.tex')
  await openFile(page2, 'main.tex')
  await typeAtTop(page, '\n' + m1a)
  await page.waitForTimeout(700) // overlap window
  await typeAtEnd(page2, '\n' + m1b)
  await bothMarks(page, page2, m1a, m1b, 'M1 tex')
  // no leakage: a seeded file must not contain the tex marks
  await openFile(page, 'q.typst')
  const typstLeak = await cmText(page)
  expect(typstLeak, 'M1: tex marks leaked into q.typst').not.toContain(m1a)
  expect(typstLeak, 'M1: tex marks leaked into q.typst').not.toContain(m1b)

  // ====================== M2 .typst =====================================
  const m2a = `//Q${RUN}A_TYPST`
  const m2b = `//Q${RUN}B_TYPST`
  await openFile(page, 'q.typst')
  await openFile(page2, 'q.typst')
  await typeAtTop(page, '\n' + m2a)
  await page.waitForTimeout(700)
  await typeAtEnd(page2, '\n' + m2b)
  await bothMarks(page, page2, m2a, m2b, 'M2 typst')

  // ======================== M3 .bib =====================================
  const m3a = `%%Q${RUN}A_BIB`
  const m3b = `%%Q${RUN}B_BIB`
  await openFile(page, 'q.bib')
  await openFile(page2, 'q.bib')
  await typeAtTop(page, '\n' + m3a)
  await page.waitForTimeout(700)
  await typeAtEnd(page2, '\n' + m3b)
  await bothMarks(page, page2, m3a, m3b, 'M3 bib')

  // ======================== M4 .md (Milkdown) ===========================
  const m4a = `Q${RUN}A_MD`
  const m4b = `Q${RUN}B_MD`
  await openFile(page, 'q.md')
  await openFile(page2, 'q.md')
  // the surface depends on editor-mode: Milkdown (ProseMirror) in visual
  // mode, or the CM6 code surface. Click whichever is ACTUALLY visible —
  // the other stays in the DOM but hidden (a blind click on it hangs).
  const clickMarkdownSurface = async (pg: any): Promise<string> => {
    const tryVisible = async (sel: string, ms = 5000) => {
      try {
        await pg.waitForSelector(sel, { state: 'visible', timeout: ms })
        return true
      } catch {
        return false
      }
    }
    const mdSel = '.ol-md-editor-shell [contenteditable="true"], .milkdown [contenteditable="true"], .milkdown .ProseMirror'
    if (await tryVisible(mdSel, 8000)) {
      await pg.locator(mdSel).first().click({ timeout: 10000 })
      return 'md-visual'
    }
    if (await tryVisible('.cm-content[contenteditable="true"]', 4000)) {
      await pg.click('.cm-content', { timeout: 10000 })
      return 'md-code'
    }
    await pg.locator('[contenteditable="true"]').first().click({ timeout: 15000 })
    return 'md-editable'
  }
  const kA = await clickMarkdownSurface(page)
  await page.keyboard.type(`\n${m4a}`, { delay: 10 })
  await page.waitForTimeout(900) // overlap window
  const kB = await clickMarkdownSurface(page2)
  await page2.keyboard.type(`\n${m4b}`, { delay: 10 })
  expect(kA + '/' + kB, 'both md surfaces interactive').not.toBe('md-editable/md-editable')
  await bothMarks(page, page2, m4a, m4b, 'M4 md')

  // ================ M5 .svg / .tikz / .drawio ===========================
  // Surface-agnostic: if the app opens a text editor, A's concurrent edit
  // must reach B (live or via re-open); if it opens a viewer surface, the
  // bar is both clients healthy with no console/page errors — either way,
  // two cooperators on the same file must not crash or diverge observably.
  const tryTextEdit = async (pg: any, mark: string): Promise<boolean> => {
    // VISIBLE cm6 only — in viewer-mode surfaces the CM6 stays in the DOM
    // but hidden, and clicking it hangs.
    try {
      await pg.waitForSelector('.cm-content:visible', { timeout: 4_000 })
    } catch {
      return false
    }
    await pg.click('.cm-content')
    await pg.keyboard.press('Control+End')
    await pg.keyboard.type(mark, { delay: 8 })
    return true
  }
  for (const [file, mk] of [
    ['q.svg', `SVGQ${RUN}`],
    ['q.tikz', `TQK${RUN}`],
    ['q.drawio', `DRAWQ${RUN}`],
  ] as [string, string][]) {
    await openFile(page, file)
    await openFile(page2, file)
    await page.waitForTimeout(2_500) // let viewer surfaces settle
    const aEdited = await tryTextEdit(page, file + '::' + mk).catch(() => false)
    if (aEdited) {
      const live = await bodyHas(page2, mk, 15_000)
      if (!live) {
        await openFile(page2, file)
        expect(await bodyHas(page2, mk, 20_000), `M5 ${file}: B never saw A edit (live or re-open)`).toBeTruthy()
      }
    } else {
      // viewer-only surface: assert both clients are alive and not blank
      expect(await page.evaluate(() => (document.body.innerText || '').length > 20), `M5 ${file}: A blank`).toBeTruthy()
      expect(await page2.evaluate(() => (document.body.innerText || '').length > 20), `M5 ${file}: B blank`).toBeTruthy()
    }
    assertCalm(page, `M5 ${file} A`)
    assertCalm(page2, `M5 ${file} B`)
  }

  // ====================== M6 image ======================================
  {
    // a 1x1 red png (deterministic bytes)
    const png1x1 = Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
      'base64'
    )
    // A uploads a second image while B has the tree open (concurrent add)
    // mongosh prints real BSON ObjectIds EJSON-wrapped (ObjectId('hex')) —
    // String() them server-side; the upload route validates 24-hex strictly.
    const rfId = (
      (mongoEval(
        `var p = db.projects.findOne({ _id: ObjectId('${pid}') });` +
          ` var rf = (p.rootFolder && p.rootFolder[0]) || {} ;` +
          ` print(rf._id ? String(rf._id) : '');`
      ) || '')
        .trim()
        .split('\n')
        .pop() as string
    )
    expect(rfId, 'rootFolder id must exist').toBeTruthy()
    // unwrap EJSON if the wrapper ever sneaks through
    const rfHex = (rfId.match(/([0-9a-fA-F]{24})/) || [])[1] || rfId
    const tok = await csrfToken(page)
    const up = await page.request.post(
      `/Project/${pid}/upload?folder_id=${rfHex}`,
      {
        headers: { 'x-csrf-token': tok },
        multipart: { name: 'qimg-a.png', qqfile: { name: 'qimg-a.png', mimeType: 'image/png', buffer: png1x1 } },
      }
    )
    expect(up.status(), 'M6 upload second image').toBeLessThan(300)
    await page.waitForTimeout(2_500)
    // B (already inside the same project) must see the new entry — tree
    // refresh or re-open; the editor tree usually dispatches on its own
    const bSeesNew = await bodyHas(page2, 'qimg-a.png', 15_000)
    if (!bSeesNew) {
      await page2.goto('/editor/' + pid, { waitUntil: 'domcontentloaded' })
      await page2.waitForSelector('.cm-content, [class*="file"]', { timeout: 60_000 })
      expect(await bodyHas(page2, 'qimg-a.png', 20_000), 'M6: B never saw the new image file').toBeTruthy()
    }
    assertCalm(page, 'M6 A')
    assertCalm(page2, 'M6 B')
  }

  // ================== M7 write-through ===================================
  {
    // both clients reload; the M1 marks must persist in both
    for (const [pg, who] of [
      [page, 'A'],
      [page2, 'B'],
    ] as [any, string][]) {
      await pg.reload({ waitUntil: 'load' })
      await pg.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
      await openFile(pg, 'main.tex')
      const txt = await cmText(pg)
      expect(txt, `M7: ${who} lost A mark after reload`).toContain(m1a)
      expect(txt, `M7: ${who} lost B mark after reload`).toContain(m1b)
    }
  }

  // ================= final health ========================================
  assertCalm(page, 'end A')
  assertCalm(page2, 'end B')

  await ctx2.close()
}
)
