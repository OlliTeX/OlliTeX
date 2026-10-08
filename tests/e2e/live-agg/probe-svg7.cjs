const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const cerr = []
  page.on('console', m => { if (m.type() === 'error') cerr.push(m.text().slice(0, 300)) })
  await page.goto(H.BASE + `/editor/${TEX}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2500)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(3500)
  const sw = await page.evaluate(() => {
    const s = document.querySelector('.editor-toggle-switch')
    if (!s) return null
    const r = s.getBoundingClientRect()
    return { present: true, visible: r.width > 0, checked: s.querySelector('input:checked') ? s.querySelector('input:checked').value : null, options: Array.from(s.querySelectorAll('input')).map(i => i.value) }
  })
  console.log('SWITCH:', JSON.stringify(sw))
  if (sw && sw.visible) {
    // click the "Visual" option (label near the rich-text radio)
    const clicked = await page.evaluate(() => {
      const s = document.querySelector('.editor-toggle-switch')
      const labels = Array.from(s.querySelectorAll('label'))
      const l = labels.find(x => /visual/i.test(x.textContent || ''))
      if (!l) return 'no-label'
      const input = l.querySelector('input') || s.querySelector('input[value="rich-text"]')
      input.click()
      return 'clicked:' + input.value
    })
    console.log(clicked)
    await page.waitForTimeout(6000)
    const st = await page.evaluate(() => {
      const vis = (sel) => { const e = document.querySelector(sel); if (!e) return null; const r = e.getBoundingClientRect(); return { w: Math.round(r.width), h: Math.round(r.height) } }
      return {
        crash: /Sorry, something went wrong/.test(document.body.innerText || ''),
        diagramCanvas: vis('canvas'),
        diagram: vis('[class*="diagram"]'),
        cmShown: (() => { const c = document.querySelector('.cm-editor'); return c ? getComputedStyle(c).display : null })(),
        checked: (() => { const i = document.querySelector('.editor-toggle-switch input:checked'); return i && i.value })()
      }
    })
    console.log('AFTER TOGGLE:', JSON.stringify(st))
    console.log('ERR:', JSON.stringify(cerr.slice(0, 3)))
    await page.screenshot({ path: '/var/tmp/svg7.png' })
  }
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
