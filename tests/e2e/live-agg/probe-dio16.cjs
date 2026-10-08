const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const errs = []
  const failed = []
  page.on('console', m => { if (m.type() === 'error') errs.push(m.text().slice(0, 200)) })
  page.on('response', r => { if (r.status() >= 400) failed.push(r.status() + ' ' + r.url().split('.de').pop()) })
  await page.goto(H.BASE + '/static/drawio/index.html?spin=1&noHelpS=1&od=0', { waitUntil: 'load', timeout: 60000 })
  await page.waitForTimeout(14000)
  const st = await page.evaluate(() => ({
    title: document.title,
    bodyLen: document.body.querySelectorAll('*').length,
    geStatus: (document.getElementById('geStatus') || {}).textContent || null,
    app: typeof App !== 'undefined' ? { mode: App.mode, isMainCalled: App.isMainCalled } : null
  }))
  console.log('STATE:', JSON.stringify(st))
  console.log('CONSOLE ERRS:', errs.slice(0, 5).join(' || ') || '(none)')
  console.log('HTTP ERRS:', failed.slice(0, 6).join(' || ') || '(none)')
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
