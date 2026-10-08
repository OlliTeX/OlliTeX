const H = require('./harness.cjs')
;(async () => {
  console.log('launching...')
  try {
    const r = await H.getContext()
    console.log('getContext OK', Object.keys(r))
    await r.browser.close()
  } catch (e) {
    console.log('CONTEXT ERR:', e && e.stack ? e.stack.slice(0, 700) : e)
  }
})()
