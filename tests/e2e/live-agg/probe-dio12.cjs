const H = require('./harness.cjs')
const INIT = `window.__mark = 'INIT-RAN'; window.__kks = []; const oe = console.error; console.error = function() { try { (window.__kks = window.__kks || []).push(Array.prototype.slice.call(arguments).map(a => (a && (a.stack || a.message)) || String(a)).join(' ').slice(0, 400)) } catch(_) {} try { oe.apply(console, arguments) } catch(_) {} }`
async function main() {
  const { browser, ctx } = await H.getContext()
  await ctx.addInitScript(INIT)
  const page = await ctx.newPage()
  await page.goto(H.BASE + '/static/drawio/index.html?spin=1&noHelpS=1&lightbox=0', { waitUntil: 'domcontentloaded', timeout: 60000 }).catch(e => console.log('goto:', e.message.slice(0,80)))
  await page.waitForTimeout(20000)
  const st = await page.evaluate(() => ({
    mark: window.__mark,
    errs: (window.__kks || []).slice(0, 10),
    geStatus: document.getElementById('geStatus') ? document.getElementById('geStatus').textContent.slice(0, 120) : null,
    graph: !!window.graph
  }))
  console.log(JSON.stringify(st, null, 1))
  await browser.close(); process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
