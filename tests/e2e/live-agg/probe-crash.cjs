const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const cerr = []
  page.on('console', m => { if (m.type() === 'error') cerr.push(m.text().slice(0, 200)) })
  await page.goto(H.BASE + `/editor/${TEX}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2500)
  const svgFile = page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first()
  await svgFile.dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(5000)
  const st = await page.evaluate(() => {
    const crash = /Sorry, something went wrong/.test(document.body.innerText || '')
    const btn = Array.from(document.querySelectorAll('button')).find(b => /edit\s*svg/i.test(b.textContent || ''))
    const dl = Array.from(document.querySelectorAll('button')).find(b => /download/i.test(b.textContent || ''))
    const preview = document.querySelector('.file-view img, [class*="file-view"] img, img[src*="blob"]')
    return { crash, editSvg: !!btn, download: !!dl, preview: !!preview }
  })
  console.log('SVG:', JSON.stringify(st))
  console.log('CONSOLE ERR:', JSON.stringify(cerr.slice(0, 3)))
  if (st.editSvg) {
    await page.locator('button', { hasText: 'Edit SVG' }).first().click().catch(() => {})
    await page.waitForTimeout(3500)
    const modal = await page.evaluate(() => {
      const m = document.querySelector('.toast-svg-editor-modal, [class*="toast-svg"]')
      if (!m) return { opened: false }
      const r = m.getBoundingClientRect()
      return { opened: true, w: Math.round(r.width), h: Math.round(r.height), ta: !!m.querySelector('textarea'), img: !!m.querySelector('img'), buttons: Array.from(m.querySelectorAll('button')).map(b => b.textContent.trim()).filter(Boolean).slice(0, 6) }
    })
    console.log('SVG MODAL:', JSON.stringify(modal))
  }
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
