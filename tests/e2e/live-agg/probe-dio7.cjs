const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const sc = await H.newScratch(ctx, 'agg-drawio7')
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
  await page.waitForTimeout(18000)
  const st = await page.evaluate(() => {
    const ifr = document.querySelector('.drawio-viewer iframe')
    const r = {}
    try {
      const doc = ifr.contentDocument
      const win = doc.defaultView
      r.mxClient = !!win.mxClient
      r.graph = !!win.graph
      const keys = Object.keys(win).filter(k => /graph|editor|app|draw|error|ready/i.test(k)).slice(0, 30)
      r.winKeys = keys
      const ge = doc.getElementById('geEditor')
      r.geEditor = ge ? { style: ge.getAttribute('style') } : null
      const bodyKids = Array.from(doc.body.children).map(c => c.tagName + '#' + c.id).slice(0, 14)
      r.bodyKids = bodyKids
      if (win.graph) r.graphCells = win.graph.model ? win.graph.model.nodeCount : -1
      const gei = doc.getElementById('geInfo')
      r.geInfoText = gei ? (gei.textContent || '').replace(/\s+/g,' ').trim().slice(0,200) : null
      const gest = doc.getElementById('geStatus')
      r.geStatus = gest ? (gest.textContent||'').trim().slice(0,200) : null
    } catch (e) { r.err = ('' + e.message).slice(0, 120) }
    return r
  })
  console.log(JSON.stringify(st, null, 0))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
