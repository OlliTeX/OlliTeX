const H = require('./harness.cjs')
const TEX = H.fixturePid('agg-tex-fixture')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const cerr = []
  page.on('console', m => { if (m.type() === 'error') cerr.push(m.text().slice(0, 300)) })
  await page.goto(H.BASE + `/editor/${TEX}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
  await page.waitForTimeout(2500)
  await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first().dblclick({ force: true }).catch(() => {})
  await page.waitForTimeout(5000)
  const st = await page.evaluate(() => {
    const sw = Array.from(document.querySelectorAll('button, [role="tab"], a')).map(b => (b.getAttribute('aria-label') || b.textContent || '').trim()).filter(t => /^(code|visual)$/i.test(t))
    const ls = {}
    try { for (let i = 0; i < localStorage.length; i++) { const k = localStorage.key(i); if (/lastUsedMode/i.test(k)) ls[k] = localStorage.getItem(k) } } catch (e) {}
    const vis = (sel) => { const e = document.querySelector(sel); if (!e) return null; const r = e.getBoundingClientRect(); return { w: Math.round(r.width), h: Math.round(r.height), disp: getComputedStyle(e).display } }
    return { codeVisSwitch: sw, ls, diagram: vis('[class*="diagram-view"], [class*="diagram-viewer"], [class*="maxGraph"], .diagram'), cmShown: (() => { const c = document.querySelector('.cm-editor'); return c ? getComputedStyle(c.parentElement).display + '/' + getComputedStyle(c).display : null })(), visibleText: (document.body.innerText || '').replace(/\n+/g,' | ').slice(0, 200) }
  })
  console.log(JSON.stringify(st, null, 1).slice(0, 1000))
  console.log('ERR:', JSON.stringify(cerr.slice(0, 4)))
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
