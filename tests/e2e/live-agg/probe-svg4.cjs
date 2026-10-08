const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const logs = []
  page.on('console', m => { if (m.type() === 'error' || m.type() === 'warning') logs.push(m.type() + ': ' + m.text().slice(0, 400)) })
  const perr = []
  page.on('pageerror', e => perr.push(e.message.slice(0, 400)))
  await page.goto(`${H.BASE}/editor/${TEX}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2500)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(4500)
  console.log('CONSOLE:', JSON.stringify(logs.slice(0, 8), null, 1))
  console.log('PAGEERR:', JSON.stringify(perr.slice(0, 5), null, 1))
  const eb = await page.evaluate(() => {
    const el = Array.from(document.querySelectorAll('div')).find(d => /Sorry, something went wrong/.test(d.textContent || '') && d.children.length < 4)
    return el ? el.outerHTML.slice(0, 500) : null
  })
  console.log('BOUNDARY:', eb)
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
