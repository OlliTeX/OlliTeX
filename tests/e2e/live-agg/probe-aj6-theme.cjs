const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  await page.goto(H.BASE + '/hub#/home', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(3500)
  // open account menu (top-right avatar / name)
  const acct = page.locator('[aria-haspopup="menu"], [class*="account"], [aria-label*="account" i], [aria-label*="menu" i]').first()
  await acct.click().catch(() => {})
  await page.waitForTimeout(1200)
  let sel = await page.evaluate(() => {
    const s = document.querySelector('.theme-toggle-select')
    if (!s) return { found: false }
    return { found: true, options: Array.from(s.options).map(o => o.value), value: s.value }
  })
  if (!sel.found) {
    // menu may be a portal dropdown; search across portals
    await page.screenshot({ path: '/var/tmp/probe-acct.png' })
  }
  if (sel.found) {
    const target = sel.options.includes('dark_mode') ? 'dark_mode' : sel.options[0]
    await page.evaluate(v => {
      const s = document.querySelector('.theme-toggle-select')
      const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set
      setter.call(s, v)
      s.dispatchEvent(new Event('change', { bubbles: true }))
    }, target)
    await page.waitForTimeout(1500)
    const applied = await page.evaluate(() => ({
      bodyClass: document.body.className,
      htmlData: document.documentElement.getAttribute('data-ol-theme') || document.documentElement.className
    }))
    console.log('after change:', JSON.stringify(applied))
    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(3000)
    const persisted = await page.evaluate(() => ({
      bodyClass: document.body.className,
      htmlData: document.documentElement.getAttribute('data-ol-theme') || document.documentElement.className
    }))
    console.log('after reload:', JSON.stringify(persisted))
    // also check editor page picks it up
    await page.goto(H.BASE + `/editor/${H.fixturePid('agg-tex-fixture')}`, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(3500)
    const editorTheme = await page.evaluate(() => document.body.className)
    console.log('editor bodyClass:', JSON.stringify(editorTheme))
    // restore default
    await page.goto(H.BASE + '/hub#/home', { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2500)
  }
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
