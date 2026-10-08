const H = require('./harness.cjs')
const TPL = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const sc = await H.newScratch(ctx, 'agg-drawio5')
  const pid = sc.pid
  const cPage = await ctx.newPage()
  await cPage.goto(H.BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded' })
  const csrf = await H.csrfOf(cPage)
  await H.ensureFile(ctx, pid, 'd.drawio', '<mxfile host="app.diagrams.net" agent="kk"><diagram name="A" id="A"><mxGraphModel dx="300" dy="200" grid="1" gridSize="10" guides="1" tooltips="1" connect="1" arrows="1" fold="1" page="1" pageScale="1" pageWidth="420" pageHeight="297"><root><mxCell id="0" /><mxCell id="1" parent="0" /><mxCell id="2" value="KIO" style="rounded=0;whiteSpace=wrap;html=1;" vertex="1" parent="1"><mxGeometry x="50" y="50" width="120" height="60" as="geometry" /></mxCell></root></mxGraphModel></diagram></mxfile>', 'application/xml', csrf)
  const w = []
  const page = await ctx.newPage()
  page.on('pageerror', e => w.push('PE: ' + e.message.slice(0, 200)))
  page.on('console', m => { if (m.type() === 'error') w.push('CE: ' + m.text().slice(0, 200)) })
  await page.goto(H.BASE + `/editor/${pid}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2000)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'd.drawio' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(10000)
  const st = await page.evaluate(() => {
    const v = document.querySelector('.drawio-viewer')
    if (!v) return { viewer: false }
    const ifr = v.querySelector('iframe')
    let inner = 'no-access'
    if (ifr) {
      try {
        const d = ifr.contentDocument
        const g = d ? d.getElementById('geEditor') : null
        inner = d ? { geEditor: !!g, hasGraph: !!(d.mxGraph || (d.graph && 'graph').length) , bodyKids: d ? d.body.querySelectorAll('*').length : 0 } : { geEditor: false }
      } catch (e) { inner = { err: e.message.slice(0, 60) } }
    }
    return { viewer: true, iframe: ifr && ifr.src, inner }
  })
  console.log('STATE:', JSON.stringify(st))
  console.log('WARN:', w.slice(0, 4).join(' || '))
  await page.screenshot({ path: '/var/tmp/dio5.png' })
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
