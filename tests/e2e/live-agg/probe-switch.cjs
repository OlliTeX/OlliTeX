const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  await page.goto(H.BASE + `/editor/${TEX}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2500)
  const before = await page.evaluate(() => {
    const s = document.querySelector('.editor-toggle-switch'); const r = s && s.getBoundingClientRect()
    return { switchVisible: s ? r.width > 0 : false, checked: s ? (s.querySelector('input:checked') || {}).value : null }
  })
  console.log('main.tex BEFORE:', JSON.stringify(before))
  if (before.switchVisible) {
    const clicked = await page.evaluate(() => {
      const s = document.querySelector('.editor-toggle-switch')
      const l = Array.from(s.querySelectorAll('label')).find(x => !/code/i.test(x.textContent || ''))
      const input = (l && l.querySelector('input')) || s.querySelector('input[value="rich-text"]')
      if (input) { input.click(); return 'cl:' + input.value }
      return 'none'
    })
    console.log(clicked)
    await page.waitForTimeout(6000)
    const after = await page.evaluate(() => ({
      checked: (document.querySelector('.editor-toggle-switch input:checked') || {}).value || null,
      cmShown: (() => { const c = document.querySelector('.cm-editor'); return c ? getComputedStyle(c).display : null })(),
      vis: (() => { const e = document.querySelector('[class*="rich"], [class*="prosemirror"], [class*="visual-editor"]'); return e ? e.className + ' ' + Math.round(e.getBoundingClientRect().width) : null })(),
      body: (document.body.innerText || '').replace(/\n+/g, ' | ').slice(150, 450)
    }))
    console.log('AFTER:', JSON.stringify(after))
  }
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
