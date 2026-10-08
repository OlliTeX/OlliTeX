const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const html = await ctx.request.get(H.BASE + `/editor/${TEX}`).then(r => r.text())
  const i = html.indexOf('visualEditorProviders')
  if (i < 0) { console.log('not in html; len=', html.length); process.exit(0) }
  console.log(html.slice(i, i + 600))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
