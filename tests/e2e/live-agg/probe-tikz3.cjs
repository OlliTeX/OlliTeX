const H = require('./harness.cjs')
const TPL = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const sc = await H.newScratch(ctx, 'agg-tikz3')
  if (sc.status !== 200) throw new Error('scratch create failed ' + sc.status)
  const pid = sc.pid
  // (CSRF: upload needs the x-csrf-token meta from the editor page)
  const cPage = await ctx.newPage()
  await cPage.goto(H.BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded' })
  const csrf = await H.csrfOf(cPage)
  await H.ensureFile(ctx, pid, 'one.tikz', '\\documentclass{standalone}\n\\usepackage{tikz}\n\\begin{document}\n\\begin{tikzpicture}\n\\node{AKK-ONE-MARKER};\n\\end{tikzpicture}\n\\end{document}', 'text/plain', csrf)
  await H.ensureFile(ctx, pid, 'two.tikz', '\\documentclass{standalone}\n\\begin{document}\n\\begin{tikzpicture}\n\\node{AKK-TWO-MARKER};\n\\end{tikzpicture}\n\\end{document}', 'text/plain', csrf)
  const res = await H.openEditor(ctx, pid)
  const page = res.page
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2500)
  const cerr = []
  page.on('console', m => { if (m.type() === 'error') cerr.push(m.text().slice(0, 400)) })
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'one.tikz' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(7000)
  const st = await page.evaluate(() => {
    const vis = (sel) => { const e = document.querySelector(sel); if (!e) return null; const r = e.getBoundingClientRect(); return { w: Math.round(r.width), h: Math.round(r.height) } }
    const cm = document.querySelector('.cm-editor')
    return {
      tikzViewer: vis('.tikz-viewer'),
      cmShown: cm ? getComputedStyle(cm).display : null,
      boundary: /Sorry, something went wrong/.test(document.body.innerText || ''),
      checked: (() => { const i = document.querySelector('.editor-toggle-switch input:checked'); return i && i.value })(),
      body: (document.body.innerText || '').replace(/\n+/, ' | ').slice(0, 260)
    }
  })
  console.log('TIKZ STATE:', JSON.stringify(st))
  console.log('CONSOLE:', JSON.stringify(cerr.slice(0, 5)))
  await page.screenshot({ path: '/var/tmp/tikz2.png' })
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
