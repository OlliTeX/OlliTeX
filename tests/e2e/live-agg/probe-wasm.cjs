const H = require('./harness.cjs')
const TPL = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const sc = await H.newScratch(ctx, 'agg-drawio-wasm')
  const pid = sc.pid
  const cPage = await ctx.newPage()
  await cPage.goto(H.BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded' })
  const csrf = await H.csrfOf(cPage)
  await H.ensureFile(ctx, pid, 'd.drawio', '<mxfile host="app.diagrams.net"><diagram name="A"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/></root></mxGraphModel></diagram></mxfile>', 'application/xml', csrf)
  const w = []
  const page = await ctx.newPage()
  page.on('pageerror', e => w.push('PAGEERROR: ' + e.message.slice(0, 300)))
  page.on('console', m => { if (m.type() === 'error') w.push('CONSOLE: ' + m.text().slice(0, 300)) })
  page.on('requestfailed', r => w.push('REFFAILED: ' + r.url().slice(0, 160)))
  await page.goto(H.BASE + `/editor/${pid}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2000)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'd.drawio' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(9000)
  const st = await page.evaluate(() => {
    const v = document.querySelector('.drawio-viewer')
    if (!v) return { viewer: false }
    const ifr = v.querySelector('iframe')
    return { viewer: true, iframe: ifr ? ifr.src : null, w: Math.round(v.getBoundingClientRect().width) }
  })
  console.log('STATE:', JSON.stringify(st))
  console.log('WARNINGS:', w.slice(0, 6).join('\n'))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
