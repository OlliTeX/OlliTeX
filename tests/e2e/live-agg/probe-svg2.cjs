const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const { page } = await H.openEditor(ctx, TEX)
  const svgFile = page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first()
  await svgFile.dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(4500)
  const st = await page.evaluate(() => {
    // find the main editor content region and describe its top-level children
    const region = document.querySelector('.ide-redesign-editor, .ol-v2-editor, [class*="editor-pane"]') || document.querySelector('.cm-editor')?.closest('div[class*="editor"]')
    const describe = (el) => el ? {
      cls: (el.className || '').toString().slice(0, 80),
      kids: Array.from(el.children).slice(0, 8).map(k => (k.className || k.tagName).toString().slice(0, 60))
    } : null
    const tabs = Array.from(document.querySelectorAll('[class*="tab"]')).map(t => (t.textContent || '').trim().slice(0, 30)).filter(Boolean)
    // is there an img with the svg's blob URL?
    const imgs = Array.from(document.querySelectorAll('img')).map(i => (i.src || '').slice(0, 80))
    const cm = document.querySelectorAll('.cm-editor').length
    return { region: describe(region), tabs, imgs: imgs.slice(0, 8), cm }
  })
  console.log(JSON.stringify(st, null, 1).slice(0, 1200))
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
