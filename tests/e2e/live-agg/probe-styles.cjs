const H = require('./harness.cjs')
const PID = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const { page } = await H.openEditor(ctx, PID)
  const styles = await page.evaluate(() => {
    const links = Array.from(document.querySelectorAll('link[rel="stylesheet"]')).map(l => l.href.split('/').pop())
    const rules = []
    for (const ss of document.styleSheets) {
      try {
        for (const r of ss.cssRules) {
          if (r.selectorText && r.selectorText.includes('ol-v2-rail-home-logo')) {
            rules.push((ss.href || 'inline').split('/').pop() + ' :: ' + r.selectorText + ' -> ' + r.style.backgroundImage.slice(0, 60))
          }
        }
      } catch (e) {}
    }
    return { links, rules }
  })
  console.log(JSON.stringify(styles, null, 1))
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
