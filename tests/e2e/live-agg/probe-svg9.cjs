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
  await page.waitForTimeout(6000)
  const st = await page.evaluate(() => {
    const vis = (s) => { const e = document.querySelector(s); if (!e) return null; const r = e.getBoundingClientRect(); return { w: Math.round(r.width), h: Math.round(r.height) } }
    const cm = document.querySelector('.cm-editor')
    const sw = document.querySelector('.editor-toggle-switch')
    let swVisible = false, swChecked = null
    if (sw) { const r = sw.getBoundingClientRect(); swVisible = r.width > 0; const c = sw.querySelector('input:checked'); swChecked = c && c.value }
    return {
      cmShown: cm ? getComputedStyle(cm).display : null,
      cmText: cm ? (cm.textContent || '').slice(0, 80) : null,
      switch: { present: !!sw, visible: swVisible, checked: swChecked },
      diagramCanvas: vis('canvas'),
      diagramEl: vis('[class*="diagram"]'),
      boundary: /Sorry, something went wrong/.test(document.body.innerText || ''),
      body: (document.body.innerText || '').replace(/\n+/g, ' | ').slice(0, 240)
    }
  })
  console.log(JSON.stringify(st, null, 1))
  console.log('PE:', cerr.join(' | ') || '(none)')
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
