const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const cerr = []
  page.on('pageerror', e => cerr.push(e.message.slice(0, 160)))
  await page.goto(H.BASE + `/editor/${TEX}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2500)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(7000)
  const step1 = await page.evaluate(() => {
    const c = document.querySelector('canvas')
    return { canvas: c ? { w: Math.round(c.getBoundingClientRect().width), h: Math.round(c.getBoundingClientRect().height) } : null }
  })
  // toggle to CODE (the raw SVG source) via the Code|Visual switch
  const sw = page.locator('.editor-toggle-switch')
  let swInfo = null
  const swCount = await sw.count()
  if (swCount > 0) {
    const clicked = await page.evaluate(() => {
      const s = document.querySelector('.editor-toggle-switch')
      if (!s) return 'no-switch'
      const labels = Array.from(s.querySelectorAll('label'))
      const l = labels.find(x => /code/i.test(x.textContent || '')) || labels[0]
      const input = (l && l.querySelector('input')) || s.querySelector('input[value="cm6"]')
      if (input) { input.click(); return 'cl:' + input.value }
      return 'no-input'
    })
    swInfo = clicked
    await page.waitForTimeout(3500)
  }
  const step2 = await page.evaluate(() => {
    const cm = document.querySelector('.cm-editor')
    const txt = cm ? (cm.textContent || '').slice(0, 400) : ''
    return { codeVisible: !!cm ? getComputedStyle(cm).display !== 'none' : false, hasSVGSource: /<svg[\s>]/i.test(txt) || /diagram|AGG/.test(txt), snippet: txt.replace(/\s+/g, ' ').slice(0, 160) }
  })
  console.log(JSON.stringify({ step1, swInfo, step2, cerr: cerr.slice(0, 2) }))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
