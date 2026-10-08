const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const { page, errors } = await H.openEditor(ctx, TEX)
  const svgFile = page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first()
  await svgFile.dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(4500)
  const st = await page.evaluate(() => {
    // The editor main pane: find the largest visible central container
    const all = Array.from(document.querySelectorAll('div'))
    const cands = all.filter(d => {
      const r = d.getBoundingClientRect()
      return r.width > 400 && r.height > 300 && r.left < 300 && r.top < 300
    }).sort((a, b) => (b.getBoundingClientRect().width * b.getBoundingClientRect().height) - (a.getBoundingClientRect().width * a.getBoundingClientRect().height))
    const top = cands.slice(0, 4).map(d => ({
      cls: (d.className || '').toString().slice(0, 90),
      txt: (d.innerText || '').replace(/\n+/g, ' | ').slice(0, 200)
    }))
    return { top, pageErrors: [] }
  })
  console.log(JSON.stringify(st, null, 1).slice(0, 1600))
  console.log('PAGE ERRORS:', errors.page.slice(0, 5))
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
