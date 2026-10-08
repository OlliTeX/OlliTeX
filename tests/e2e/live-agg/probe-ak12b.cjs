const H = require('./harness.cjs')
const TYP = H.fixturePid('agg-typst-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const typPage = await ctx.newPage()
  const errs = []
  typPage.on('pageerror', e => errs.push(e.message.slice(0, 120)))
  await typPage.goto(`${H.BASE}/editor/${TYP}`, { waitUntil: 'domcontentloaded' })
  await typPage.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await typPage.waitForTimeout(1500)
  const t0 = Date.now()
  let ok = false
  const sel = '.pdf-view, [class*="pdf"] canvas, iframe[src*="pdf"], object[type*="pdf"], .pdf-js-container'
  while (Date.now() - t0 < 120000) {
    const snap = await typPage.evaluate(s2 => {
      const el = document.querySelector(s2)
      let pdf = false
      if (el) { const r = el.getBoundingClientRect(); pdf = r.width > 50 && r.height > 50 }
      const btns = Array.from(document.querySelectorAll('button')).map(b => (b.getAttribute('aria-label') || b.textContent || '').trim()).filter(t => /compil|recompil/i.test(t)).slice(0, 3)
      const logEl = document.querySelector('[class*="log"]')
      const docText = (document.body.innerText || '')
      const errLine = (docText.match(/[Ee]rror[^\n]{0,100}/g) || []).slice(0, 2)
      return { pdf, btns, errLine }
    }, sel)
    const s = Math.round((Date.now() - t0) / 1000)
    console.log(`t=${s}s`, JSON.stringify(snap).slice(0, 260))
    if (snap.pdf) { ok = true; break }
    await typPage.waitForTimeout(12000)
  }
  console.log('RESULT:', ok, 'PE:', errs.slice(0, 3).join(' | '))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
