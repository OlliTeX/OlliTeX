const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  await page.goto(H.BASE + '/hub#/home', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(3500)
  const info = await page.evaluate(() => {
    const menus = Array.from(document.querySelectorAll('[aria-haspopup], button')).map(b => (b.getAttribute('aria-label') || b.textContent || '').trim().slice(0, 40)).filter(Boolean)
    return { menus: menus.slice(0, 25), sel: !!document.querySelector('.theme-toggle-select') }
  })
  console.log(JSON.stringify(info, null, 1))
  await page.screenshot({ path: '/var/tmp/hub-home.png' })
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
