const H = require('./harness.cjs')
const INIT = `
(function(){
  const rec = []
  const push = (t, v) => { try { rec.push('[' + t + '] ' + String(v && (v.stack || v.message || v) || v).slice(0, 300)) } catch(_) {} }
  const oe = console.error
  console.error = function() { push('console.error', Array.prototype.slice.call(arguments).join(' ')); try { oe.apply(console, arguments) } catch(_) {} }
  const ow = console.warn
  console.warn = function() { push('console.warn', Array.prototype.slice.call(arguments).join(' ')); try { ow.apply(console, arguments) } catch(_) {} }
  window.addEventListener('unhandledrejection', ev => push('rejection', ev.reason), true)
  window.__logs = rec
  Object.defineProperty(window, '__logs', { value: rec, writable: false })
})();
`
async function main() {
  const { browser, ctx } = await H.getContext()
  await ctx.addInitScript(INIT)
  const sc = await H.newScratch(ctx, 'agg-drawio9')
  const pid = sc.pid
  const cPage = await ctx.newPage()
  await cPage.goto(H.BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded' })
  const csrf = await H.csrfOf(cPage)
  await H.ensureFile(ctx, pid, 'd.drawio', '<mxfile host="app.diagrams.net"><diagram name="A" id="A"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="KIO" style="rounded=0;" vertex="1" parent="1"><mxGeometry x="50" y="50" width="120" height="60" as="geometry"/></mxCell></root></mxGraphModel></diagram></mxfile>', 'application/xml', csrf)
  const hostConsole = []
  const page = await ctx.newPage()
  page.on('console', m => hostConsole.push(m.type() + ': ' + m.text().slice(0, 200)))
  await page.goto(H.BASE + `/editor/${pid}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2000)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'd.drawio' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(14000)
  const st = await page.evaluate(() => {
    const ifr = document.querySelector('.drawio-viewer iframe')
    const r = {}
    try {
      const win = ifr.contentDocument.defaultView
      r.frameLogs = (win.__logs || []).slice(0, 25)
      r.graph = !!win.graph
    } catch (e) { r.err = ('' + e.message).slice(0, 100) }
    return r
  })
  console.log('FRAME LOGS:')
  ;(st.frameLogs || []).forEach(l => console.log('  ' + l))
  console.log('graph:', st.graph)
  console.log('HOST CONSOLE (errors):', hostConsole.filter(x => /error|warn|fail|reject/i.test(x)).slice(0, 6).join(' || '))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
