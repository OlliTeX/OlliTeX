const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const netErr = []
  page.on('requestfailed', r => netErr.push(r.url().split('/').pop() + ' :: ' + (r.failure() && r.failure().errorText)))
  await page.goto(H.BASE + `/editor/${TEX}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(3000)
  const cfg = await page.evaluate(async () => {
    // find the visual-editor provider config where it lives
    const scripts = Array.from(document.querySelectorAll('script'))
    for (const s of scripts) {
      const txt = s.textContent || ''
      const i = txt.indexOf('visualEditorProviders')
      if (i >= 0) return { found: 'inline', ctx: txt.slice(i, i + 400) }
    }
    // try the app config API
    try {
      const r = await fetch('/Config/GetConfig.json?project_id=' )
      const j = await r.json()
      const s = JSON.stringify(j)
      const i2 = s.indexOf('visualEditorProviders')
      return { found: 'api', ctx: s.slice(i2, i2 + 400) }
    } catch (e) { return { found: null, e: e.message } }
  })
  console.log('CONFIG:', JSON.stringify(cfg).slice(0, 500))
  console.log('NETERR:', JSON.stringify(netErr.slice(0, 6)))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
