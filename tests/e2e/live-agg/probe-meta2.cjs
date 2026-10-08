const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  await page.goto(H.BASE + '/', { waitUntil: 'domcontentloaded' })
  const r = await page.evaluate(async () => {
    const r2 = await fetch('/Project/123/anything', { method: 'HEAD' }).catch(() => null)
    return null
  })
  // read from the home page inline config
  const cfg = await page.evaluate(() => {
    const scripts = Array.from(document.querySelectorAll('script'))
    for (const s of scripts) {
      const t = s.textContent || ''
      const i = t.indexOf('textExtensions')
      if (i >= 0) return t.slice(i, i + 260)
    }
    return null
  })
  console.log('cfg:', cfg)
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
