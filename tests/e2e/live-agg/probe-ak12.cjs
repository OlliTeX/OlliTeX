const H = require('./harness.cjs')
const TYP = H.fixturePid('agg-typst-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const typPage = await ctx.newPage()
  typPage.on('pageerror', e => console.log('PE:', e.message.slice(0, 140)))
  await typPage.goto(`${H.BASE}/editor/${TYP}`, { waitUntil: 'domcontentloaded' })
  await typPage.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await typPage.waitForTimeout(1500)
  const compileBtn = await typPage.evaluate(() => {
    const b = Array.from(document.querySelectorAll('button')).find(x => /compile/i.test(x.getAttribute('aria-label') || x.textContent || ''))
    if (b) b.click()
    return !!b
  })
  console.log('compileBtn:', compileBtn)
  const t0 = Date.now()
  let ok = false
  const sel = '.pdf-view, [class*="pdf"] canvas, iframe[src*="pdf"], object[type*="pdf"], .pdf-js-container'
  while (Date.now() - t0 < 180000) {
    const found = await typPage.evaluate(s2 => {
      const el = document.querySelector(s2)
      if (!el) return false
      const r = el.getBoundingClientRect()
      return r.width > 50 && r.height > 50
    }, sel)
    if (found) { ok = true; break }
    await typPage.waitForTimeout(3000)
  }
  console.log('AK12 result:', ok, 'after', Math.round((Date.now() - t0) / 1000) + 's')
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
