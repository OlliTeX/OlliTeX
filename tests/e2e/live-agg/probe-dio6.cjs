const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  // reuse last scratch? find most recent agg-drawio* project
  const sc = await H.newScratch(ctx, 'agg-drawio6')
  const pid = sc.pid
  const cPage = await ctx.newPage()
  await cPage.goto(H.BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded' })
  const csrf = await H.csrfOf(cPage)
  await H.ensureFile(ctx, pid, 'd.drawio', '<mxfile host="app.diagrams.net"><diagram name="A" id="A"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="KIO" style="rounded=0;" vertex="1" parent="1"><mxGeometry x="50" y="50" width="120" height="60" as="geometry"/></mxCell></root></mxGraphModel></diagram></mxfile>', 'application/xml', csrf)
  const w = []
  const page = await ctx.newPage()
  page.on('pageerror', e => w.push('PE: ' + e.message.slice(0, 200)))
  page.on('console', m => { if (m.type() === 'error') w.push('CE: ' + m.text().slice(0, 250)) })
  page.on('requestfailed', r => w.push('RF: ' + r.url().slice(0, 140)))
  await page.goto(H.BASE + `/editor/${pid}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2000)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'd.drawio' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(10000)
  const st = await page.evaluate(() => {
    const ifr = document.querySelector('.drawio-viewer iframe')
    if (!ifr) return { no: 'iframe' }
    let doc = null
    try { doc = ifr.contentDocument } catch (e) {}
    const r = { sameOrigin: !!doc }
    try {
      const win = doc ? doc.defaultView : null
      if (doc) {
        r.geEditor = !!(doc.getElementById && doc.getElementById('geEditor'))
        r.bodyLen = doc.body ? doc.body.querySelectorAll('*').length : -1
        r.title = doc.title
      }
      if (win) {
        r.graph = !!(win.graph)
        r.mxClient = Boolean(win.mxClient)
      }
    } catch (e) { r.err = ('' + e.message).slice(0, 80) }
    return r
  })
  console.log('STATE:', JSON.stringify(st))
  console.log('WARN:', w.slice(0, 8).join(' || '))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
