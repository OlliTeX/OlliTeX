const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  await page.goto(H.BASE + '/static/drawio/index.html?spin=1&noHelpS=1', { waitUntil: 'load', timeout: 60000 }).catch(e => console.log('goto:', e.message.slice(0, 60)))
  await page.waitForTimeout(15000)
  const st = await page.evaluate(() => ({
    readyState: document.readyState,
    mxWinLoaded: typeof mxWinLoaded !== 'undefined' ? mxWinLoaded : 'undef',
    checkAllLoaded: typeof checkAllLoaded,
    AppMain: typeof App !== 'undefined' ? (typeof App.main) : 'noApp',
    AppMainCalled: typeof App !== 'undefined' ? App.isMainCalled : 'noApp',
    isSupportedBrowser: typeof navigator !== 'undefined' && navigator.userAgent.includes('HeadlessChrome'),
    geStatus: (document.getElementById('geStatus') || {}).textContent || null,
    graph: !!window.graph,
    geEditor: !!document.getElementById('geEditor')
  }))
  console.log(JSON.stringify(st, null, 1))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
