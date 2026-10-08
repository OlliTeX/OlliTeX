const H = require('./harness.cjs')
const INIT = `
(function(){
  const rec = []
  const push = (t, a) => { try { rec.push('[' + t + '] ' + (typeof a === 'string' ? a : a && (a.stack || a.message || JSON.stringify(a)) || String(a)).slice(0, 400)) } catch(_) {} }
  const oe = console.error; console.error = function() { const args = Array.prototype.slice.call(arguments); push('console.error', args.join(' ')); try { oe.apply(console, args) } catch(_) {} }
  const ow = console.warn; console.warn = function() { push('console.warn', Array.prototype.slice.call(arguments).join(' ')); try { ow.apply(console, arguments) } catch(_) {} }
  window.addEventListener('unhandledrejection', ev => push('rejection', ev.reason), true)
  window.addEventListener('error', ev => push('error', (ev.message || '') + ' @ ' + (ev.filename || '') + ':' + ev.lineno), true)
  window.__kks = rec
})()
`
async function main() {
  const { browser, ctx } = await H.getContext()
  await ctx.addInitScript(INIT)
  const sc = await H.newScratch(ctx, 'agg-drawio10')
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
  await page.waitForTimeout(16000)
  const st = await page.evaluate(() => {
    const ifr = document.querySelector('.drawio-viewer iframe')
    const r = {}
    try {
      const win = ifr.contentDocument.defaultView
      r.errors = (win.__kks || []).slice(0, 20)
      r.graph = !!win.graph
    } catch (e) { r.err = ('' + e.message).slice(0, 100) }
    return r
  })
  console.log(JSON.stringify(st.errors, null, 1))
  console.log('graph:', st.graph)
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
