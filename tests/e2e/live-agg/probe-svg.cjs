const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const { page } = await H.openEditor(ctx, TEX)
  const svgFile = page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first()
  await svgFile.dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(4500)
  await page.screenshot({ path: '/var/tmp/svg-open.png' })
  const st = await page.evaluate(() => {
    const open = Array.from(document.querySelectorAll('[class*="active"], [aria-selected="true"]')).map(e => (e.textContent || '').trim().slice(0, 40)).filter(Boolean)
    const body = (document.body.innerText || '')
    const editorVisible = !!document.querySelector('.cm-editor') && getComputedStyle(document.querySelector('.cm-editor')).display !== 'none'
    return { open, bodySnippet: body.slice(0, 300), editorVisible }
  })
  console.log(JSON.stringify(st).slice(0, 600))
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
