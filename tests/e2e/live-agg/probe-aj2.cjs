const H = require('./harness.cjs')
async function main() {
  const { browser, ctx } = await H.getContext()
  const page = await ctx.newPage()
  const check = async (label, url, probe) => {
    await page.goto(H.BASE + url, { waitUntil: 'domcontentloaded' })
    for (let i = 0; i < 10; i++) {
      await page.waitForTimeout(1200)
      const ok = await page.evaluate(probe)
      if (ok.found) { console.log(label, JSON.stringify(ok)); return }
    }
    console.log(label, 'TIMEOUT')
  }
  await check('AJ-3 activeprojects', '/admin-settings/site.general.activeprojects', () => {
    const c = document.querySelector('#settings-sec-site.general.activeprojects') || Array.from(document.querySelectorAll('[id^="settings-sec-"]')).find(e => /activeprojects/i.test(e.id))
    if (!c) return { found: false }
    const txt = c.innerText || ''
    return { found: true, len: txt.length, hasTable: !!c.querySelector('table, [class*="table"], tr'), error: /couldn't load|went wrong|exception/i.test(txt), preview: txt.slice(0, 200) }
  })
  await check('AJ-3 instancestats', '/admin-settings/site.general.stats', () => {
    const c = document.querySelector('#settings-sec-site.general.stats') || Array.from(document.querySelectorAll('[id^="settings-sec-"]')).find(e => /stats|grafana/i.test(e.id))
    if (!c) return { found: false }
    const txt = c.innerText || ''
    return { found: true, len: txt.length, hasGraph: !!c.querySelector('svg, canvas, iframe'), error: /couldn't load|went wrong|exception/i.test(txt), preview: txt.slice(0, 200) }
  })
  await check('AJ-3 overview', '/admin-settings/overview', () => {
    const c = Array.from(document.querySelectorAll('[id^="settings-sec-"]')).find(e => /overview|activity/i.test(e.id + ' ' + (e.innerText || '').slice(0, 80)))
    if (!c) return { found: false, any: Array.from(document.querySelectorAll('[id^="settings-sec-"]')).map(e => e.id).slice(0, 8) }
    const txt = c.innerText || ''
    return { found: true, len: txt.length, error: /couldn't load|went wrong|exception/i.test(txt), preview: txt.slice(0, 200) }
  })
  await check('AK-7 pythonrunner misc', '/admin-settings/site.general.misc', () => {
    const txt = document.body.innerText || ''
    const lbl = Array.from(document.querySelectorAll('label, [class*="label"]')).find(e => /python runner|python/i.test(e.textContent || ''))
    return { found: !!lbl, label: lbl ? lbl.textContent.trim().slice(0, 60) : null }
  })
  await check('AJ-5 ai-prompts', '/admin-settings/site.llm.prompts', () => {
    const c = Array.from(document.querySelectorAll('[id^="settings-sec-"]')).find(e => /prompts/i.test(e.id))
    if (!c) return { found: false }
    const txt = c.innerText || ''
    const ta = c.querySelector('textarea')
    return { found: true, len: txt.length, hasTextarea: !!ta, preview: txt.slice(0, 160) }
  })
  await browser.close()
  process.exit(0)
}
main().catch(e => { console.error('FATAL', e.message); process.exit(1) })
