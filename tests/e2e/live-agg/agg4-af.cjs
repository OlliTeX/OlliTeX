/**
 * AG module 4 — AF: synctex both directions (controlled round trip with a
 * real, DYNAMIC buildId taken from the compile response) + word count (modal
 * renders real counts; no locateFile error; no re-count console flood).
 * Runs on the test user's 'agg-tex-fixture' (compiling it is safe — the
 * owner's live project is never touched).
 */
const H = require('./harness.cjs')

async function main() {
  const M = 'agg4-af'
  const PIX = process.env.AGG_FIXTURE_PID || process.env.AGG_TEX_PID || H.fixturePid(process.env.AGG_FIXTURE || 'agg-tex-fixture')
  if (!PIX) throw new Error('fixture project agg-tex-fixture not found — run bootstrap.sh')
  const { browser, ctx } = await H.getContext()
  const { page, errors } = await H.openEditor(ctx, PIX)
  try {
    // ---- compile and capture the buildId from the response ----
    const compRes = await page.evaluate(async (pid) => {
      const m = document.querySelector('meta[name="ol-csrfToken"]')
      const t0 = Date.now()
      const r = await fetch(`/project/${pid}/compile`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': m ? m.content : '' },
      })
      const t = await r.text()
      let body = null
      try { body = JSON.parse(t) } catch (e) { /* keep text */ }
      let build = ''
      if (body) {
        const of = (body.outputFiles || []).find(o => o.build)
        if (of) build = of.build
        if (!build && body.buildId) build = body.buildId
        if (!build && body.compileStatus && body.compileStatus.buildId) build = body.compileStatus.buildId
      }
      if (!build) {
        const mm = t.match(/[0-9a-f]{8}-[0-9a-f]{8,}/) || t.match(/"buildId"\s*:\s*"([^"]+)"/)
        if (mm) build = mm[1]
      }
      return { status: r.status, statusName: body && body.status, build, ms: Date.now() - t0, snippet: t.slice(0, 220) }
    }, PIX)
    H.record(M, 'AF-0 compile succeeds', compRes.status === 200 && compRes.statusName === 'success', `status=${compRes.status} name=${compRes.statusName} ms=${compRes.ms}`)
    H.record(M, 'AF-0b buildId captured from compile response', !!compRes.build, compRes.snippet.slice(0, 160))

    // ---- controlled synctex round trips (rendered lines, both directions) ----
    // Semantics pinned: \title{Your Paper} is line 16 (rendered on page 1);
    // the intro paragraph is line 28. synctex maps a glyph position to the
    // SOURCE RANGE that produced it — for multi-line boxes the returned line
    // is the range start (title box spans ~16..21; intro para 28..).
    if (compRes.build) {
      const round = async (line, tol, label) => {
        const fwd = await page.request
          .get(`${H.BASE}/project/${PIX}/sync/code?file=main.tex&line=${line}&column=1&buildId=${encodeURIComponent(compRes.build)}`)
          .then(r => r.json().catch(() => null)).catch(() => null)
        H.record(M, 'AF-1 tex->pdf ' + label, !!(fwd && fwd.pdf && fwd.pdf[0] && fwd.pdf[0].page), JSON.stringify(fwd && fwd.pdf ? fwd.pdf[0] : fwd).slice(0, 180))
        if (fwd && fwd.pdf && fwd.pdf[0]) {
          const { page: pg, h, v } = fwd.pdf[0]
          const back = await page.request
            .get(`${H.BASE}/project/${PIX}/sync/pdf?page=${pg}&h=${h}&v=${v}&buildId=${encodeURIComponent(compRes.build)}`)
            .then(r => r.json().catch(() => null)).catch(() => null)
          const c = back && back.code && back.code[0]
          H.record(M, 'AF-2 pdf->tex round trip ' + label, !!c && /main\.tex$/.test(c.file || '') && Math.abs(c.line - line) <= tol, JSON.stringify(back && back.code ? back.code[0] : back).slice(0, 180))
        }
      }
      await round(16, 6, 'line16 \\title')
      await round(28, 3, 'line28 intro para')
    }

    // ---- word count (File menu, same pattern as agg5 M-6) ----
    await page.evaluate(() => {
      const b = Array.from(document.querySelectorAll('button')).find(x => (x.textContent || '').trim() === 'File')
      if (b) b.click()
    })
    await page.waitForTimeout(900)
    const wcBtn = await page.evaluate(() => {
      const b = Array.from(document.querySelectorAll('a.dropdown-item, button, [role="menuitem"]')).find(x => /word count/i.test(x.textContent || ''))
      if (!b) return 'no-wc'
      b.click()
      return 'clicked'
    })
    await page.waitForTimeout(6000)
    const wc = await page.evaluate(() => {
      const dialog = Array.from(document.querySelectorAll('[role="dialog"]')).sort((a, z) => z.offsetWidth * z.offsetHeight - a.offsetWidth * a.offsetHeight)[0]
      if (!dialog) return { modal: false }
      const txt = dialog.innerText.replace(/\s+/g, ' ')
      const nums = (txt.match(/\d+/g) || []).map(Number)
      return { modal: true, text: txt.slice(0, 220), maxNum: Math.max(0, ...nums) }
    })
    const wcErr = errors.console.filter(t => /Couldn't find|main\.tex/.test(t))
    H.record(M, 'AF-3 word count modal with real counts', wc.modal && wc.maxNum > 50, JSON.stringify(wc).slice(0, 260))
    H.record(M, 'AF-4 no locateFile error in console', wcErr.length === 0, JSON.stringify(wcErr).slice(0, 160))
    await page.screenshot({ path: '/var/tmp/agg4-wordcount.png' })
  } finally {
    await browser.close()
  }
  H.report(M)
}
main().then(() => process.exit(0)).catch(e => { console.error('FATAL', e); process.exit(1) });
