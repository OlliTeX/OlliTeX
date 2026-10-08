const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const reqs = []
  page.on('response', r => { const u = r.url(); if (u.includes('static/drawio')) reqs.push(r.status() + ' ' + u.replace('https://psintern.neuro.uni-bremen.de', '')) })
  page.on('requestfailed', r => reqs.push('FAIL ' + r.url().replace('https://psintern.neuro.uni-bremen.de', '')))
  await page.goto(H.BASE + '/static/drawio/index.html?spin=1&noHelpS=1', { waitUntil: 'load', timeout: 60000 }).catch(() => {})
  await page.waitForTimeout(12000)
  // the app asked for the bundle via mxUtils.getAll; resolve its URL:
  const asked = await page.evaluate(() => {
    try {
      return {
        def: typeof mxResources !== 'undefined' ? mxResources.getDefaultBundle('resources/dia', 'en') : 'no-mxResources',
        special: typeof mxResources !== 'undefined' ? mxResources.getSpecialBundle('resources/dia', 'en') : null
      }
    } catch (e) { return { err: e.message } }
  })
  console.log('ASSED:', JSON.stringify(asked))
  console.log('RELEVANT REQUESTS:');
  reqs.filter(x => /resources|\.xml|\.txt|dia_|styles|404/.test(x)).slice(0, 14).forEach(x => console.log('  ' + x))
  const codes = reqs.filter(x => x.startsWith('4') || x.startsWith('5') || x.startsWith('FAIL'))
  console.log('ERRORS:');
  codes.forEach(x => console.log('  ' + x))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
