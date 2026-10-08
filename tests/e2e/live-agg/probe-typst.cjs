const H = require('./harness.cjs')
const TYP = H.fixturePid('agg-typst-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const cerr = [], cerr2 = []
  page.on('pageerror', e => cerr.push(e.message.slice(0, 200)))
  page.on('console', m => { if (m.type() === 'error') cerr2.push(m.text().slice(0, 200)) })
  await page.goto(H.BASE + `/editor/${TYP}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2500)
  const compileClicked = await page.evaluate(() => {
    const b = Array.from(document.querySelectorAll('button')).find(x => /compile/i.test(x.getAttribute('aria-label') || x.textContent || '') && !/log/i.test(x.getAttribute('aria-label') || ''))
    if (b) { b.click(); return b.getAttribute('aria-label') || b.textContent.trim().slice(0, 40) }
    return null
  })
  console.log('COMPILE BTN:', compileClicked)
  let pdf = false
  for (let i = 0; i < 40; i++) {
    await page.waitForTimeout(2500)
    pdf = await page.evaluate(() => {
      const v = document.querySelector('.pdf-view, .pdf-js-container, [class*="pdf-view"]')
      if (v) { const r = v.getBoundingClientRect(); if (r.width > 100) return 'container ' + Math.round(r.width) + 'x' + Math.round(r.height) }
      const c = document.querySelector('canvas')
      if (c) { const r = c.getBoundingClientRect(); if (r.width > 200) return 'canvas ' + Math.round(r.width) + 'x' + Math.round(r.height) }
      return false
    })
    if (pdf) break
  }
  const logState = await page.evaluate(() => {
    const l = document.querySelector('[class*="log"], .log-pane')
    const errText = document.body.innerText.match(/error|failed|typst[^\n]{0,80}/gi)
    return { errText: (errText || []).slice(0, 4) }
  })
  console.log('PDF:', JSON.stringify(pdf))
  console.log('LOG:', JSON.stringify(logState))
  console.log('PAGEERRORS:', cerr.slice(0, 3).join(' | '))
  console.log('CONSOLE:', cerr2.slice(0, 4).join(' | '))
  await page.screenshot({ path: '/var/tmp/typst1.png' })
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
