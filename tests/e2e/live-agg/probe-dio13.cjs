const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const msgs = []
  const page = await ctx.newPage()
  page.on('dialog', async d => { msgs.push(d.type() + ': ' + d.message()); await d.dismiss().catch(() => {}) })
  await page.goto(H.BASE + '/static/drawio/index.html?spin=1&noHelpS=1', { waitUntil: 'domcontentloaded', timeout: 60000 })
  await page.waitForTimeout(25000)
  console.log('DIALOGS:', JSON.stringify(msgs, null, 1))
  const st = await page.evaluate(() => ({ geStatus: (document.getElementById('geStatus') || {}).textContent || null, graph: !!window.graph }))
  console.log(JSON.stringify(st))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
