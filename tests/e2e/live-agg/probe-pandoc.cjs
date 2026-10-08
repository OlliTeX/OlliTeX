const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  await page.goto(H.BASE + `/editor/${H.fixturePid('agg-tex-fixture')}`, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(2500)
  const m = await page.evaluate(() => {
    const es = (document.querySelector('meta[name="ol-ExposedSettings"]') || {}).content || ''
    try { const o = JSON.parse(es); return { enablePandocConversions: o.enablePandocConversions, typstEnabled: o.typstEnabled } } catch (e) { return { err: e.message, snippet: es.slice(0, 100) } }
  })
  console.log(JSON.stringify(m))
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
