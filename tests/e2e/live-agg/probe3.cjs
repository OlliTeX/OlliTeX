const H = require('./harness.cjs')
;(async () => {
  console.log('step1: openEditor...')
  const r = await H.getContext()
  try {
    const e = await H.openEditor(r.ctx, H.OWNER_PID)
    console.log('step2: openEditor OK, errors.page =', e.errors.page.length)
  } catch (err) {
    console.log('OPENEDITOR ERR:', (err && err.stack || String(err)).slice(0, 500))
  }
  await r.browser.close()
})().catch(e => console.log('TOP ERR:', e))
