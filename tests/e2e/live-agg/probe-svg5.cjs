const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  await page.goto(H.BASE + `/editor/${TEX}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2500)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(6000)
  const st = await page.evaluate(() => {
    const vis = (sel) => { const e = document.querySelector(sel); return e ? { w: Math.round(e.getBoundingClientRect().width), h: Math.round(e.getBoundingClientRect().height) } : null }
    return {
      diagram: vis('[class*="diagram"]'),
      tikz: vis('.tikz-viewer'),
      cmVisible: (() => { const e = document.querySelector('.cm-editor'); return e ? getComputedStyle(e).display !== 'none' : false })(),
      bodySnippet: (document.body.innerText || '').replace(/\n+/g, ' | ').slice(0, 300)
    }
  })
  console.log(JSON.stringify(st, null, 1).slice(0, 900))
  await page.screenshot({ path: '/var/tmp/svg5.png' })
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
