const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const { page } = await H.openEditor(ctx, TEX)
  // 1) SVG file view header
  const svgFile = page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first()
  await svgFile.dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(4000)
  const svgHeader = await page.evaluate(() => {
    const btns = Array.from(document.querySelectorAll('button')).map(b => (b.textContent || '').trim()).filter(t => t && t.length < 40)
    const preview = document.querySelector('.file-view, [class*="file-view"], .ol-file-view')
    return {
      buttons: btns.slice(0, 30),
      hasEditImage: btns.some(t => /edit image/i.test(t)),
      hasDownload: btns.some(t => /download/i.test(t)),
      fileViewSel: preview ? preview.className.slice(0, 60) : null
    }
  })
  console.log('SVGHEADER', JSON.stringify(svgHeader).slice(0, 500))
  // 2) rail help entry
  const rail = await page.evaluate(() => {
    const railEl = document.querySelector('nav[aria-label*="rail" i], .ol-v2-rail, .ide-rail')
    if (!railEl) return { rail: false }
    const items = Array.from(railEl.querySelectorAll('button, a')).map(b => (b.getAttribute('aria-label') || b.textContent || '').trim().slice(0, 40))
    return { rail: true, items }
  })
  console.log('RAIL', JSON.stringify(rail).slice(0, 600))
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
