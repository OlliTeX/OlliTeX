const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const sc = await H.newScratch(ctx, 'agg-drawio11')
  const pid = sc.pid
  const cPage = await ctx.newPage()
  await cPage.goto(H.BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded' })
  const csrf = await H.csrfOf(cPage)
  await H.ensureFile(ctx, pid, 'd.drawio', '<mxfile host="app.diagrams.net"><diagram name="A" id="A"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="KIO" style="rounded=0;" vertex="1" parent="1"><mxGeometry x="50" y="50" width="120" height="60" as="geometry"/></mxCell></root></mxGraphModel></diagram></mxfile>', 'application/xml', csrf)
  const logs = []
  const page = await ctx.newPage()
  page.on('request', r => {
    const u = r.url()
    if (/\/log\?severity=/.test(u)) logs.push(u)
  })
  await page.goto(H.BASE + `/editor/${pid}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2000)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'd.drawio' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(16000)
  for (const u of logs.slice(0, 4)) {
    let q = ''
    try { const url = new URL(u); q = url.search; } catch (e) { q = u }
    const dec = decodeURIComponent(q)
    console.log('---- LOG BEACON ----')
    console.log(dec.slice(0, 1400))
  }
  console.log('beacons:', logs.length)
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
