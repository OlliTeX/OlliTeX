const H = require('./harness.cjs')
const TYP = H.fixturePid('agg-typst-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const typPage = await ctx.newPage()
  await typPage.goto(`${H.BASE}/editor/${TYP}`, { waitUntil: 'domcontentloaded' })
  await typPage.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await typPage.waitForTimeout(20000)
  // open the log pane (Ctrl+3? or the log tab). Try the toolbar/log toggle.
  const log = await typPage.evaluate(() => {
    // find visible log text areas
    const els = Array.from(document.querySelectorAll('[class*="log"], pre, .log'))
    let best = ''
    for (const e of els) {
      const t = (e.textContent || '').trim()
      if (t.length > best.length) best = t
    }
    return best.slice(0, 1500)
  })
  console.log('LOG TEXT:', log || '(none found)')
  await typPage.screenshot({ path: '/var/tmp/ak12c.png' })
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
