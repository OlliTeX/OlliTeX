const H = require('./harness.cjs')
const PID = H.fixturePid('agg-tex-fixture')
async function main() {
  const { ctx, browser } = await H.getContext()
  const { page } = await H.openEditor(ctx, PID)
  const railHome = await page.evaluate(() => {
    const b = document.querySelector('.ol-v2-rail-home-entry')
    if (!b) return { present: false }
    const r = b.getBoundingClientRect()
    const logo = b.querySelector('.ol-v2-rail-home-logo')
    const lr = logo ? logo.getBoundingClientRect() : null
    const lcs = logo ? getComputedStyle(logo) : null
    return {
      present: true,
      w: Math.round(r.width), h: Math.round(r.height),
      href: b.getAttribute('aria-label'),
      logoW: lr ? Math.round(lr.width) : null,
      logoH: lr ? Math.round(lr.height) : null,
      logoBg: lcs ? lcs.backgroundImage.slice(0, 80) : null,
      display: lcs ? lcs.display : null,
    }
  })
  console.log('RAIL HOME:', JSON.stringify(railHome))
  // SVG editor: double-click the svg fixture
  const svgFile = await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).count()
  console.log('svg fixture rows:', svgFile)
  if (svgFile > 0) {
    await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first().dblclick({ force: true })
    await page.waitForTimeout(3000)
    const svgUI = await page.evaluate(() => {
      const modal = document.querySelector('.toast-svg-editor-modal, [class*="toast-svg"]')
      if (!modal) return { modal: false }
      const r = modal.getBoundingClientRect()
      const ta = modal.querySelector('textarea')
      const img = modal.querySelector('img')
      const buttons = Array.from(modal.querySelectorAll('button')).map(b => (b.textContent || '').trim()).filter(Boolean)
      return { modal: true, w: Math.round(r.width), h: Math.round(r.height), hasTextarea: !!ta, hasImg: !!img, buttons: buttons.slice(0, 8) }
    })
    console.log('SVG MODAL:', JSON.stringify(svgUI))
    await page.screenshot({ path: '/var/tmp/probe-svg-modal.png' })
  }
  await browser.close().catch(() => {})
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
