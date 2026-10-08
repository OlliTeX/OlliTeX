const H = require('./harness.cjs')
;(async () => {
  const r = await H.getContext()
  const page = await r.ctx.newPage()
  const errs = []
  page.on('pageerror', e => errs.push('PE: ' + e.message.slice(0, 150)))
  const resp = await page.goto(H.BASE + '/editor/' + H.OWNER_PID, { waitUntil: 'domcontentloaded', timeout: 30000 }).catch(e => null)
  console.log('goto status:', resp && resp.status())
  await page.waitForTimeout(12000)
  const st = await page.evaluate(() => ({
    title: document.title,
    url: location.href,
    cm: !!document.querySelector('.cm-editor'),
    bodyHead: (document.body.innerText || '').replace(/\s+/g, ' ').slice(0, 300),
    hasRail: !!document.querySelector('.ide-rail'),
    hasErrRoot: !!document.querySelector('[class*="error"], [class*="Error"]')
  }))
  console.log(JSON.stringify(st, null, 1))
  console.log('pageerrors:', JSON.stringify(errs.slice(0, 6), null, 1))
  await r.browser.close()
})().catch(e => console.log('TOP:', e.message))
