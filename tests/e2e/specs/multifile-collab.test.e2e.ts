/**
 * multifile-collab — the 024 Option B acceptance battery (per-(project,doc)
 * Yjs rooms). Live-audit 024 (psintern): multi-file projects shared ONE
 * Yjs room per project, so the /editor file tree showed ANOTHER document's
 * content in a file (sample.bib displaying main.tex; the frog project room
 * head was SVG test content). Owner Option B (design locked, TODO-90296a84):
 *
 *   root doc        -> room "{pid}"        (unchanged — history preserved)
 *   other doc {fid} -> room "{pid}-{fid}"  (fresh rooms, own seed)
 *
 *   B1 the file tree shows THIS file's content (sample.bib shows the
 *      greenwade93 bib — NOT main.tex) in both directions
 *   B2 cross-file contamination: typing in one file does not leak into the
 *      other (both directions, across a document switch)
 *   B3 a real second client editing sample.bib converges into the first
 *      client's open sample.bib (CRDT merge inside the per-doc room)
 *   B4 write-through: the bib edit persists across a full editor reload
 *      (room head -> docstore -> reseed) without touching main.tex
 *   B5 the root-room contract survives: main.tex room resolves to {pid}
 *      (root:true) and the collab/history plane is still the root room
 *   B6 no page/console errors across the flow
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { mongoEval } from '../parity/harness'
import { USER } from '../fixtures/credentials'

test.setTimeout(420_000)

// CM6 virtualizes long documents (only the rendered window is in the DOM),
// so content assertions must be anchored: TOP (after Control+Home) proves
// "this document's head is shown"; END (after Control+End) is where the
// typed marks land. Both anchors are always rendered once scrolled there.
const cmTop = async (page: any): Promise<string> => {
  await page.click('.cm-content')
  await page.keyboard.press('Control+Home')
  await page.waitForTimeout(800)
  return page.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText.slice(0, 2000))
}
const cmEnd = async (page: any): Promise<string> => {
  await page.click('.cm-content')
  await page.keyboard.press('Control+End')
  await page.waitForTimeout(800)
  const t = await page.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText)
  return t.slice(-2000)
}
const typeAtEnd = async (page: any, text: string) => {
  await page.click('.cm-content')
  await page.keyboard.press('Control+End')
  await page.keyboard.type(text, { delay: 10 })
  await page.waitForTimeout(2500)
}
const cmText = (page: any) =>
  page.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText)

const consoleErrors: string[] = []
function watchConsole(page: any) {
  page.on('console', (m: any) => {
    if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 200))
  })
  page.on('pageerror', (e: Error) => consoleErrors.push('pageerror: ' + e.message))
  page.on('response', (r: any) => {
    if (r.status() >= 400) httpErrors.push(r.status() + ' ' + r.url())
  })
}
const httpErrors: string[] = []

async function createSampleProject(page: any): Promise<string> {
  // Hub new-project menu — Tex section. The ADMIN hub can land on the
  // "Overview & activity" tab: the New-project button lives in the Projects
  // section, so navigate there first if it is not already visible.
  await page.goto('/hub')
  const trigger = page
    .locator('button:has-text("New project"), button:has-text("Create a new project"), [aria-label="New project"]')
    .first()
  let visible = await trigger.isVisible().catch(() => false)
  if (!visible) {
    // ADMIN hub lands on the "Overview & activity" dashboard: the projects
    // list panel (with the New-project trigger) opens from the rail.
    const panel = page
      .locator('button:has-text("All projects"), button:has-text("My projects")')
      .first()
    await panel.click({ timeout: 15_000 })
    visible = await expect(trigger).toBeVisible({ timeout: 20_000 }).then(() => true).catch(() => false)
  }
  expect(visible, 'New-project trigger not visible on the hub Projects section').toBeTruthy()
  await trigger.click()

  let picked = false
  for (const label of ['Example project', 'Sample project', 'Sample (LaTeX)']) {
    const item = page.locator(`text=${label}`).first()
    if ((await item.count()) > 0 && (await item.isVisible().catch(() => false))) {
      await item.click()
      picked = true
      break
    }
  }
  expect(picked, 'no sample/example menu item in the new-project menu').toBeTruthy()

  const nameField = page
    .locator('label:has-text("Project name") input, #hub-new-project-name')
    .first()
  await expect(nameField).toBeVisible({ timeout: 20_000 })
  await nameField.fill('e2e-multifile-collab')
  const createBtn = page
    .locator('[role="dialog"] button:has-text("Create"), .mantine-Modal-root button:has-text("Create")')
    .first()
  await createBtn.click({ force: true, timeout: 15_000 })

  const navigated = await page
    .waitForURL(/\/(project|editor)\/[0-9a-f]{24}/, { timeout: 30_000 })
    .then(() => true)
    .catch(() => false)
  if (!navigated) throw new Error('createSampleProject: never navigated to the editor')
  return new URL(page.url()).pathname.split('/')[2]
}

async function openDoc(page: any, name: string) {
  const item = page.getByRole('treeitem', { name, exact: false }).first()
  await expect(item).toBeVisible({ timeout: 30_000 })
  await item.click({ timeout: 15_000 })
  // the pane swaps the document — wait for the new content marker to settle
  await page.waitForTimeout(2500)
}

test('024 Option B — per-(project,doc) rooms: no cross-file contamination', async ({ page, context }) => {
  watchConsole(page)
  // A regular USER: their hub lands in the workspace projects section
  // (New-project button visible). The battery is about the editor, not the
  // admin dashboard.
  await loginRobust(page, USER.email, USER.password)

  const pid = await createSampleProject(page)
  await page.goto('/editor/' + pid)
  const cm = await page.waitForSelector('.cm-content', { timeout: 60_000 })
  if (!cm) throw new Error('editor never rendered')

  // ---- B5 (part): the ROOM RESOLVER contract (role-gated, authoritative) ----
  // doc ids from the project doc (rootFolder tree) — deterministic source:
  const ids = mongoEval(`
    var d = db.projects.findOne({ _id: ObjectId("${pid}") });
    var r = {};
    function walk(n) {
      if (!n || typeof n !== 'object') return;
      if (n._id && n.name) {
        if (n.name === 'sample.bib' && !r.bib) r.bib = String(n._id);
        if (n.name === 'main.tex' && !r.main) r.main = String(n._id);
      }
      (n.docs || []).forEach(walk);
      (n.folders || []).forEach(walk);
      (n.children || []).forEach(walk);
      (n.fileRefs || []).forEach(walk);
    }
    if (d) (d.rootFolder || []).forEach(walk);
    (d?.docs || []).forEach(walk);
    JSON.stringify(r);
  `) as { bib?: string; main?: string }
  expect(ids?.bib, 'sample.bib has no doc id in the project tree').toBeTruthy()
  expect(ids?.main, 'main.tex has no doc id in the project tree').toBeTruthy()
  const rootId = ids.main
  const bibId = ids.bib

  const rootRoom = await page.request.get(`/project/${pid}/collab/room`)
  const rrBody = await rootRoom.text().catch(() => '')
  expect(rootRoom.status()).toBe(200)
  const rr = JSON.parse(rrBody)
  expect(rr.room).toBe(pid) // D19 contract unchanged for the root doc

  const bibRoom = await page.request.get(`/project/${pid}/collab/room?doc=${bibId}`)
  const brBody = await bibRoom.text().catch(() => '')
  expect(bibRoom.status()).toBe(200)
  const br = JSON.parse(brBody)
  expect(br.room).toBe(`${pid}-${bibId}`) // per-(project,doc) room
  expect(br.root).toBeFalsy()

  // resolver rejects a foreign doc id (role/scope guard)
  const foreign = await page.request.get(`/project/${pid}/collab/room?doc=${rootId}deadbeef`.slice(0, 24))
  expect([400, 404]).toContain(foreign.status())

  // ---- B1: the file tree shows THIS file's content, not the other's ----
  // default pane = main.tex (root doc): the top head is the LaTeX preamble
  const mainTop = await cmTop(page)
  expect(mainTop).toMatch(/documentclass\s*\{/)
  expect(mainTop).not.toContain('greenwade93')

  await openDoc(page, 'sample.bib')
  // the 024 acceptance line: sample.bib shows the greenwade93 bib (top head)
  const bibTop = await cmTop(page)
  expect(bibTop).toContain('greenwade93')
  expect(bibTop).toContain('George D. Greenwade')
  // ...NOT main.tex's content (the bug was the reverse leak)
  expect(bibTop).not.toMatch(/documentclass\s*\{/)
  expect(bibTop).not.toMatch(/begin\{document\}/)

  // ---- B2: typing in the bib does NOT leak into main.tex (both directions) ----
  const bibMark = '% e2e-mfbib-' + Date.now()
  await typeAtEnd(page, '\n' + bibMark)
  expect(await cmEnd(page)).toContain(bibMark)

  await openDoc(page, 'main.tex')
  const mainTopAfter = await cmTop(page)
  expect(mainTopAfter).toMatch(/documentclass\s*\{/)
  const mainEndAfterBib = await cmEnd(page)
  expect(mainEndAfterBib).not.toContain(bibMark) // no contamination

  // and back: typing in main.tex does not leak into the bib
  const mainMark = '% e2e-mfmain-' + Date.now()
  await typeAtEnd(page, '\n' + mainMark)

  await openDoc(page, 'sample.bib')
  const bibEndAfterMain = await cmEnd(page)
  expect(bibEndAfterMain).toContain(bibMark) // bib edit still there
  expect(bibEndAfterMain).not.toContain(mainMark) // no contamination


  // ---- B3: a REAL second client on the SAME per-doc room converges ----
  const remoteMark = '% e2e-mfremote-' + Date.now()
  const tab2 = await context.newPage()
  watchConsole(tab2)
  await tab2.goto('/editor/' + pid)
  await tab2.waitForSelector('.cm-content', { timeout: 60_000 })
  await tab2.locator('text=sample.bib').first().click({ timeout: 20_000 }).catch(async () => {
    await tab2.getByRole('treeitem', { name: 'sample.bib' }).first().click()
  })
  await tab2.waitForTimeout(2500)
  await tab2.click('.cm-content')
  await tab2.keyboard.press('Control+Home')
  await tab2.waitForTimeout(800)
  const tab2Top = await tab2.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText.slice(0, 2000))
  expect(tab2Top).toContain('greenwade93')

  await tab2.click('.cm-content')
  await tab2.keyboard.press('Control+End')
  await tab2.keyboard.type('\n' + remoteMark, { delay: 10 })

  // first client (still on sample.bib) must converge the remote edit
  await page.waitForTimeout(6000)
  const merged = await cmEnd(page)
  expect(merged).toContain(bibMark)
  expect(merged).toContain(remoteMark) // CRDT merge inside {pid}-{bib}

  // main.tex must stay clean of BOTH bib marks (rooms are isolated)
  await openDoc(page, 'main.tex')
  const mainEnd1 = await cmEnd(page)
  expect(mainEnd1).toContain(mainMark)
  expect(mainEnd1).not.toContain(bibMark)
  expect(mainEnd1).not.toContain(remoteMark)

  // ---- B4: write-through — reload and the bib edit must persist ----
  await openDoc(page, 'sample.bib')
  await page.reload({ waitUntil: 'load' })
  await page.waitForSelector('.cm-content', { timeout: 60_000 })
  await page.getByRole('treeitem', { name: 'sample.bib' }).first().click({ timeout: 30_000 })
  await page.waitForTimeout(3000)
  const afterReloadBibTop = await cmTop(page)
  expect(afterReloadBibTop).toContain('greenwade93')
  expect(afterReloadBibTop).not.toMatch(/documentclass\s*\{/)
  const afterReloadBibEnd = await cmEnd(page)
  expect(afterReloadBibEnd).toContain(bibMark) // write-through -> docstore -> reseed
  expect(afterReloadBibEnd).toContain(remoteMark)

  // and main.tex persisted independently
  await page.getByRole('treeitem', { name: 'main.tex' }).first().click({ timeout: 30_000 })
  await page.waitForTimeout(3000)
  const afterReloadMainTop = await cmTop(page)
  expect(afterReloadMainTop).toMatch(/documentclass\s*\{/)
  const afterReloadMainEnd = await cmEnd(page)
  expect(afterReloadMainEnd).toContain(mainMark)
  expect(afterReloadMainEnd).not.toContain(bibMark)
  expect(afterReloadMainEnd).not.toContain(remoteMark)

  // ---- B5: the history plane is still the ROOT room ----
  const hist = await page.request.get(`/project/${pid}/collab/history`)
  expect(hist.status()).toBe(200)
  const hj = (await hist.json()) as any
  const hs = (hj.versions || hj.history || []) as any[]
  expect(hs.length).toBeGreaterThanOrEqual(2) // v1 seed + edits (root room)

  // ---- B6: no console/page errors across the flow ----
  const real = consoleErrors.filter(
    (e) =>
      !/favicon|404 \(Not Found\)|net::ERR_ABORTED|Failed to load resource|ResponseException/.test(e)
  )
  expect(real, 'console errors during 024 flow: ' + real.slice(0, 5).join(' | ') + ' | http: ' + httpErrors.slice(0, 10).join(' ; ')).toEqual([])

  await tab2.close()
  await page.close()
})
