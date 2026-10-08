const H = require('./harness.cjs')
const INIT = `
window.__initErrs = [];
window.addEventListener('error', e => { try { (window.__initErrs = window.__initErrs || []).push({ m: String(e.message||'').slice(0,220), loc: (e.filename||'')+':'+e.lineno }) } catch(_) {} }, true)
window.addEventListener('unhandledrejection', ev => { try { (window.__initErrs = window.__initErrs || []).push({ r: String(ev.reason && (ev.reason.stack || ev.reason.message || ev.reason) || '').slice(0,300) }) } catch(_) {} })
`
async function main() {
  await ctx_addinit()
  async function ctx_addinit() {
    return null
  }
  const { browser, ctx } = await H.getContext()
  await ctx.addInitScript(INIT)
  const sc = await H.newScratch(ctx, 'agg-drawio8')
  const pid = sc.pid
  const cPage = await ctx.newPage()
  await cPage.goto(H.BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded' })
  const csrf = await H.csrfOf(cPage)
  await H.ensureFile(ctx, pid, 'd.drawio', '<mxfile host="app.diagrams.net"><diagram name="A" id="A"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="KIO" style="rounded=0;" vertex="1" parent="1"><mxGeometry x="50" y="50" width="120" height="60" as="geometry"/></mxCell></root></mxGraphModel></diagram></mxfile>', 'application/xml', csrf)
  const page = await ctx.newPage()
  await page.goto(H.BASE + `/editor/${pid}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2000)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'd.drawio' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(14000)
  const st = await page.evaluate(() => {
    const ifr = document.querySelector('.drawio-viewer iframe')
    if (!ifr) return { no: 'iframe' }
    const r = {}
    try {
      const win = ifr.contentDocument.defaultView
      r.errs = win.__initErrs || []
      r.graph = !!win.graph
      r.geStatus = ifr.contentDocument.getElementById('geStatus') ? ifr.contentDocument.getElementById('geStatus').textContent.slice(0,120) : null
    } catch (e) { r.err = ('' + e.message).slice(0, 100) }
    return r
  })
  console.log(JSON.stringify(st, null, 1))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
