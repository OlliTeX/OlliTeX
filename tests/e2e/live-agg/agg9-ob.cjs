/**
 * AG module 9 — 024 Option B: true multi-file collaboration (per-(project,doc)
 * Yjs rooms). Runs against PRODUCTION (toolkit deploy) with the module's own
 * scratch project + a second document it creates — no owner fixture touched.
 *
 *  OB-1  scratch + second document created
 *  OB-2  root room contract: GET /project/:pid/collab/room → room == {pid}
 *  OB-3  per-doc room: GET .../collab/room?doc={fid} → room == {pid}-{fid}, root falsy
 *  OB-4  foreign doc id rejected (400/404)
 *  OB-5  B1: each file shows ITS OWN content (second doc marker NOT in root doc; doc head distinct)
 *  OB-6  B2: typing in one file does NOT leak into the other (both directions)
 *  OB-7  B3: a REAL second client editing the same doc converges (CRDT in {pid}-{fid}); root stays clean
 *  OB-8  B4: write-through — the second-doc edit persists across a full reload (room head → docstore → reseed)
 *  OB-9  B5: history plane still the ROOT room (≥2 versions)
 *  OB-10 B6: no page/console errors across the flow
 */
const H = require('./harness.cjs')

const settle = (pg, ms) => new Promise(r => setTimeout(r, ms))

async function cmTop (pg) {
  await pg.locator('.cm-content').first().click()
  await pg.keyboard.press('Control+Home')
  await settle(pg, 800)
  return pg.evaluate(() => (document.querySelector('.cm-content') || { innerText: '' }).innerText.slice(0, 2000))
}
async function cmEnd (pg) {
  await pg.locator('.cm-content').first().click()
  await pg.keyboard.press('Control+End')
  await settle(pg, 800)
  const t = await pg.evaluate(() => (document.querySelector('.cm-content') || { innerText: '' }).innerText)
  return t.slice(-2000)
}
async function typeAtEnd (pg, text) {
  await pg.locator('.cm-content').first().click()
  await pg.keyboard.press('Control+End')
  await pg.keyboard.type(text, { delay: 10 })
  await settle(pg, 2500)
}
// open a document from the file tree (entity rows), return settle state
async function openDoc (pg, name) {
  const row = pg.locator('[data-testid="file-tree"] .entity-name, .entity-name', { hasText: name }).first()
  await row.click({ timeout: 20000 })
  await settle(pg, 2500)
  return true
}

async function docIdOf (pid, name) {
  // the project's rootFolder tree (Mongo — read-only, canonical doc ids)
  const out = require('child_process').execSync(
    `docker exec ollitex-mongo mongosh --quiet ollitex --eval '
      const d = db.projects.findOne({ _id: ObjectId("${pid}") });
      let found = "";
      function walk (n) {
        if (!n || typeof n !== "object") return;
        if (n._id && n.name === "${name}" && !found) found = String(n._id);
        (n.docs || []).forEach(walk); (n.folders || []).forEach(walk);
        (n.children || []).forEach(walk); (n.fileRefs || []).forEach(walk);
      }
      if (d) { (d.rootFolder||[]).forEach(walk); (d.docs||[]).forEach(walk); }
      print(found);
    '`, { encoding: 'utf8' }).trim()
  return /^[0-9a-f]{24}$/i.test(out) ? out : ''
}

async function main () {
  const M = 'agg9-ob'
  const errors = { page: [], console: [], http: [] }
  const { browser, ctx } = await H.getContext()
  const { pid, status } = await H.newScratch(ctx, 'agg9-ob-' + (Date.now() % 1e9))
  if (!pid) { H.record(M, 'OB-1 scratch created', false, 'status ' + status); await browser.close(); H.report(M); process.exit(1) }

  const { page } = await H.openEditor(ctx, pid)
  page.on('pageerror', e => errors.page.push(String(e.message).slice(0, 160)))
  page.on('console', m => { if (m.type() === 'error') errors.console.push(m.text().slice(0, 160)) })
  page.on('response', r => { if (r.status() >= 400) errors.http.push(r.status() + ' ' + r.url().slice(0, 80)) })
  const tab2Events = { page: [], console: [] }
  let secondDocName = 'ob-second.tex'

  try {
    // OB-1: second doc with a UNIQUE content line (never in the root doc)
    const csrf = await H.csrfOf(page)
    await H.ensureFile(ctx, pid, secondDocName,
      'OB-SECOND-DOC-HEAD\nThis is the second document body.\nOB-SECOND-DOC-FOOT\n',
      'application/x-tex', csrf)
    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
    await settle(page, 2000)
    const secondId = await docIdOf(pid, secondDocName)
    H.record(M, 'OB-1 scratch + second document created', !!pid && !!secondId, 'pid=' + pid + ' fid=' + secondId)

    // OB-2: root room contract (role-gated resolver — authoritative)
    const rootRoom = await ctx.request.get(`${H.BASE}/project/${pid}/collab/room`)
    let rr = {}
    try { rr = JSON.parse(await rootRoom.text()) } catch (e) {}
    H.record(M, 'OB-2 root doc room = {pid} (D19 contract intact)', rootRoom.status() === 200 && rr.room === pid, JSON.stringify({ s: rootRoom.status(), rr }).slice(0, 120))

    // OB-3: per-(project,doc) room
    const bibRoom = await ctx.request.get(`${H.BASE}/project/${pid}/collab/room?doc=${secondId}`)
    let br = {}
    try { br = JSON.parse(await bibRoom.text()) } catch (e) {}
    H.record(M, 'OB-3 second doc room = {pid}-{fid}, root falsy', bibRoom.status() === 200 && br.room === `${pid}-${secondId}` && !br.root, JSON.stringify({ s: bibRoom.status(), br }).slice(0, 140))

    // OB-4: foreign doc id rejected
    const foreign = await ctx.request.get(`${H.BASE}/project/${pid}/collab/room?doc=deadbeefdeadbeefdeadbee1`)
    H.record(M, 'OB-4 foreign doc id rejected (400/404)', [400, 404].includes(foreign.status()), 'status=' + foreign.status())

    // OB-5 (B1): each file shows ITS OWN content.
    const rootTop = await cmTop(page)
    await openDoc(page, secondDocName)
    const secTop = await cmTop(page)
    const secOwn = /OB-SECOND-DOC-HEAD/.test(secTop)
    const rootClean = !/OB-SECOND-DOC/.test(rootTop)
    H.record(M, 'OB-5 each file shows its own content (no cross-file render)', secOwn && rootClean,
      JSON.stringify({ secTop: secTop.slice(0, 80), rootTopHead: rootTop.slice(0, 60) }))

    // OB-6 (B2): typing leaks neither way (both directions, across switches)
    const secMark = '% OB-SEC-' + Date.now()
    await typeAtEnd(page, '\n' + secMark)
    const secHas = (await cmEnd(page)).includes(secMark)
    await openDoc(page, 'main.tex')
    const mainEnd1 = await cmEnd(page)
    await typeAtEnd(page, '\n% OB-MAIN-' + Date.now())
    const mainMark = (await cmEnd(page)).match(/% OB-MAIN-\d+/)
    const mainMarkTxt = mainMark ? mainMark[0] : ''
    await openDoc(page, secondDocName)
    const secEnd2 = await cmEnd(page)
    H.record(M, 'OB-6 no contamination both directions (B2)',
      secHas && !mainEnd1.includes(secMark) && !!mainMarkTxt && secEnd2.includes(secMark) && !secEnd2.includes(mainMarkTxt),
      JSON.stringify({ secHas, mainLeak: mainEnd1.includes(secMark), mainMarkTxt, secKept: secEnd2.includes(secMark), mainIntoSec: secEnd2.includes(mainMarkTxt) }))

    // OB-7 (B3): REAL second client on the same per-doc room converges (CRDT)
    const remoteMark = '% OB-REMOTE-' + Date.now()
    const tab2 = await ctx.newPage()
    tab2.on('pageerror', e => tab2Events.page.push(String(e.message).slice(0, 160)))
    tab2.on('console', m => { if (m.type() === 'error') tab2Events.console.push(m.text().slice(0, 160)) })
    await tab2.goto(`${H.BASE}/editor/${pid}`, { waitUntil: 'domcontentloaded' })
    await tab2.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
    await settle(tab2, 2000)
    await openDoc(tab2, secondDocName)
    const tab2Own = /OB-SECOND-DOC-HEAD/.test(await cmTop(tab2))
    await typeAtEnd(tab2, '\n' + remoteMark)
    // first client (still on the second doc) must converge the remote edit
    for (let i = 0; i < 10; i++) { await settle(page, 1000); if ((await cmEnd(page)).includes(remoteMark)) break }
    const merged = await cmEnd(page)
    const hasBoth = merged.includes(secMark) && merged.includes(remoteMark)
    // root doc stays clean of BOTH marks (rooms isolated)
    await openDoc(page, 'main.tex')
    const rootEnd = await cmEnd(page)
    H.record(M, 'OB-7 second client converges in per-doc room; root clean (B3/CRDT)',
      tab2Own && hasBoth && !rootEnd.includes(secMark) && !rootEnd.includes(remoteMark),
      JSON.stringify({ tab2Own, hasBoth, rootLeak: rootEnd.includes(secMark) || rootEnd.includes(remoteMark) }))

    // OB-8 (B4): write-through — reload, the second-doc edit persists (docstore reseed)
    await openDoc(page, secondDocName)
    await page.reload({ waitUntil: 'load' })
    await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
    await settle(page, 1500)
    await openDoc(page, secondDocName)
    const afterSec = await cmEnd(page)
    const persisted = afterSec.includes(secMark) && afterSec.includes(remoteMark)
    // main.tex persisted independently
    await openDoc(page, 'main.tex')
    const afterMain = await cmEnd(page)
    const mainIndependent = afterMain.includes(mainMarkTxt) && !afterMain.includes(secMark) && !afterMain.includes(remoteMark)
    H.record(M, 'OB-8 write-through persists across full reload (B4)', persisted && mainIndependent,
      JSON.stringify({ persisted, mainIndependent }))

    // OB-9 (B5): the history plane is still the ROOT room (root doc versions ≥2)
    const hist = await ctx.request.get(`${H.BASE}/project/${pid}/collab/history`)
    let versions = 0
    if (hist.status() === 200) {
      const hj = await hist.json().catch(() => ({}))
      versions = Array.isArray(hj.versions) ? hj.versions.length : Array.isArray(hj.history) ? hj.history.length : 0
    }
    H.record(M, 'OB-9 history plane intact on root room (B5)', hist.status() === 200 && versions >= 2, 'status=' + hist.status() + ' versions=' + versions)

    // OB-10 (B6): no real page/console errors
    const noise = /favicon|net::ERR_ABORTED|Failed to load resource|chunk/
    const realPage = [...errors.page, ...tab2Events.page].filter(e => !noise.test(e))
    const realCon = [...errors.console, ...tab2Events.console].filter(e => !noise.test(e) && !/404 \(Not Found\)/.test(e))
    const realHttp = errors.http
    H.record(M, 'OB-10 no page/console/HTTP errors across the flow (B6)',
      realPage.length === 0 && realCon.length === 0 && realHttp.length === 0,
      JSON.stringify({ realPage: realPage.slice(0, 3), realCon: realCon.slice(0, 3), realHttp: realHttp.slice(0, 5) }).slice(0, 300))

    await tab2.close().catch(() => {})
    await page.screenshot({ path: '/var/tmp/agg9-ob.png' }).catch(() => {})
  } finally {
    const t = await H.trash(ctx, pid)
    console.log(`(scratch ${pid} trashed: ${t})`)
    await browser.close()
  }
  H.report(M)
  process.exit(0)
}
main().then(() => process.exit(0)).catch(e => { console.error('FATAL', e); process.exit(1) })
