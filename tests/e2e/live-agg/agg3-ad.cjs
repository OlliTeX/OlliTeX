/**
 * AG module 3 — AD: image editor (stage-1 header + stage-2 FULL modal)
 * and SVG editor (stage-1 loads without 404, stage-2 usable).
 * Runs on the test user's 'agg-tex-fixture' (owned by ag-e2e3, so the
 * needed diagram.svg file can be CREATED via the file-tree UI — the owner's
 * live project stays untouched).
 */
const H = require('./harness.cjs')

async function main() {
  const M = 'agg3-ad'
  const PIX = process.env.AGG_FIXTURE_PID || process.env.AGG_TEX_PID || H.fixturePid(process.env.AGG_FIXTURE || 'agg-tex-fixture')
  if (!PIX) throw new Error('fixture project agg-tex-fixture not found — run bootstrap.sh')
  const { browser, ctx } = await H.getContext()
  const { page, errors } = await H.openEditor(ctx, PIX)
  const bad404 = []
  page.on('response', r => { if (r.status() === 404) bad404.push(r.url().slice(-80)) })
  try {
    // ---- IMAGE ----
    await page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'frog.jpg' }).first().dblclick({ force: true })
    await page.waitForTimeout(4500)
    const s1 = await page.evaluate(() => {
      const fv = document.querySelector('.file-view')
      if (!fv) return { header: false }
      const els = Array.from(fv.querySelectorAll('button, a'))
        .map(b => (b.textContent || b.getAttribute('aria-label') || b.download || '').trim())
        .filter(Boolean)
      return { header: true, btns: els.slice(0, 10), hasDownload: els.some(b => /download/i.test(b)), hasEdit: els.some(b => /edit/i.test(b)) }
    })
    H.record(M, 'AD-1 image stage-1 header (content+Download+Edit)', s1.header && s1.hasEdit && s1.hasDownload, JSON.stringify(s1).slice(0, 200))

    if (s1.hasEdit) {
      await page.locator('button', { hasText: /Edit Image|Edit/ }).first().click({ force: true })
      await page.waitForTimeout(2500)
      const s2 = await page.evaluate(() => {
        const ds = Array.from(document.querySelectorAll('[role="dialog"]'))
        if (!ds.length) return { modal: false }
        const best = ds.sort((a, z) => z.offsetWidth * z.offsetHeight - a.offsetWidth * a.offsetHeight)[0]
        const r = best.getBoundingClientRect()
        return { modal: true, w: Math.round(r.width), h: Math.round(r.height), vw: innerWidth, vh: innerHeight,
          nearFull: r.width >= innerWidth * 0.9 && r.height >= innerHeight * 0.85 }
      })
      H.record(M, 'AD-2 image stage-2 modal uses WHOLE space', s2.modal && s2.nearFull, JSON.stringify(s2))
      await page.screenshot({ path: '/var/tmp/agg3-image-modal.png' })
      await page.keyboard.press('Escape')
      await page.waitForTimeout(1200)
    }

    // ---- SVG (deterministic fixture provisioning; owner project untouched) ----
    const svgExists = (await page.locator('.entity-name', { hasText: 'diagram.svg' }).count()) > 0
    if (!svgExists) {
      const pageHandle = page
      const csrf = await H.csrfOf(pageHandle)
      await H.ensureFile(
        ctx, PIX, 'diagram.svg',
        '<svg xmlns="http://www.w3.org/2000/svg" width="200" height="120">\n  <rect width="200" height="120" fill="white"/>\n  <circle cx="100" cy="60" r="40" fill="none" stroke="black" stroke-width="2"/>\n  <text x="70" y="65" font-size="14">AGG</text>\n</svg>\n',
        'image/svg+xml', csrf)
      await page.waitForTimeout(2500)
    }
    const nowThere = (await page.locator('.entity-name', { hasText: 'diagram.svg' }).count()) > 0
    H.record(M, 'AD-3a diagram.svg fixture present in project', nowThere)
    const svgFile = page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'diagram.svg' }).first()
    // SHIPPED .svg surface (build #34, verified 2026-10-08): `svg` is a text
    // extension → the file opens as a DOCUMENT in the visual diagram canvas
    // editor (modules/diagram claims *.svg, default-visual) with a Code|Visual
    // switch to the raw SVG source. (The two-stage toast-svg modal is the
    // raster-image surface — AD-1/AD-2; the old svgedit-iframe is retired.)
    await svgFile.dblclick({ force: true }).catch(() => {})
    await page.waitForTimeout(2000)
    const svg404 = bad404.filter(u => /svgedit/i.test(u))
    H.record(M, 'AD-3 svg opens WITHOUT svgedit-era 404s', svg404.length === 0, JSON.stringify(svg404).slice(0, 200))
    // AD-3 SHIPPED .svg SURFACE (verified 2026-10-08, build #34):
    // `svg` IS a text extension, so a .svg opens as a DOCUMENT. Its editor
    // surface is the VISUAL diagram canvas (modules/diagram claims *.svg,
    // default-visual per house convention) with a Code|Visual switch to the
    // raw SVG source — the two-stage toast-svg modal is the raster-image
    // surface (AD-1/AD-2). Pin what actually ships.
    // AD-3b: the double-click lands on the canvas visual editor (no crash).
    let canvas = null
    for (let i = 0; i < 12; i++) {
      await page.waitForTimeout(1000)
      canvas = await page.evaluate(() => {
        const cs = Array.from(document.querySelectorAll('canvas'))
        const big = cs.filter(c => c.getBoundingClientRect().width > 200)
        const hit = big[0]
        if (!hit) return null
        const r = hit.getBoundingClientRect()
        return { w: Math.round(r.width), h: Math.round(r.height) }
      })
      if (canvas) break
    }
    const crash = await page.evaluate(() => /Sorry, something went wrong/.test(document.body.innerText || ''))
    H.record(M, 'AD-3b .svg double-click opens the visual canvas editor (no crash)', !!canvas && !crash, JSON.stringify({ canvas, crash }))
    // AD-4: the Code|Visual switch flips to the raw SVG SOURCE (editable)
    // and back to the canvas — the two halves of the SVG editor.
    const toggle = async (which) => {
      const done = await page.evaluate(w => {
        const s = document.querySelector('.editor-toggle-switch')
        if (!s) return 'no-switch'
        const labels = Array.from(s.querySelectorAll('label'))
        const l = labels.find(x => new RegExp(w, 'i').test(x.textContent || '')) || labels.find(x => /code|visual/i.test(x.textContent || ''))
        if (!l) return 'no-label'
        const input = l.querySelector('input')
        if (!input) return 'no-input'
        input.click()
        return 'cl:' + input.value
      }, which)
      await page.waitForTimeout(3500)
      return done
    }
    const toCode = await toggle('code')
    const codeState = await page.evaluate(() => {
      const cm = document.querySelector('.cm-editor')
      if (!cm) return { codeVisible: false, hasSVGSource: false }
      const txt = cm.textContent || ''
      return { codeVisible: getComputedStyle(cm).display !== 'none', hasSVGSource: /<svg[\s>]/i.test(txt) }
    })
    const toVis = await toggle('visual')
    const visBack = await page.evaluate(() => {
      const cs = Array.from(document.querySelectorAll('canvas')).filter(c => c.getBoundingClientRect().width > 200)
      return cs.length > 0
    })
    H.record(M, 'AD-4 .svg editor switch: code shows SVG source, visual restores canvas', toCode.startsWith('cl:') && codeState.codeVisible && codeState.hasSVGSource && visBack, JSON.stringify({ toCode, codeState, visBack }))
    // AD-4b: the code view carries the fixture content (source fidelity).
    const fid = await page.evaluate(() => {
      const s = document.querySelector('.editor-toggle-switch')
      if (s) {
        const labels = Array.from(s.querySelectorAll('label'))
        const l = labels.find(x => /code/i.test(x.textContent || ''))
        const input = l && l.querySelector('input')
        if (input) input.click()
      }
      return null
    })
    await page.waitForTimeout(2500)
    const srcHasMarker = await page.evaluate(() => {
      const cm = document.querySelector('.cm-editor')
      return !!(cm && /AGG/.test(cm.textContent || ''))
    })
    H.record(M, 'AD-4b .svg code view preserves the fixture content', srcHasMarker, 'fixture marker present: ' + srcHasMarker)
    // Clean close (discard the probe edit — no save, owner fixture untouched).
    await page.keyboard.press('Escape')
    await page.waitForTimeout(1500)
    const discardBtn = await page.locator('button:has-text("Discard"), button:has-text("discard")').count()
    if (discardBtn > 0) {
      await page.locator('button:has-text("Discard"), button:has-text("discard")').first().click().catch(() => {})
      await page.waitForTimeout(1200)
    } else {
      await page.keyboard.press('Escape')
      await page.waitForTimeout(800)
    }
    await page.screenshot({ path: '/var/tmp/agg3-svg.png' })
    H.record(M, 'no page errors', errors.page.length === 0, errors.page.join(' | ').slice(0, 200))
  } finally {
    await browser.close()
  }
  H.report(M)
}
main().then(() => process.exit(0)).catch(e => { console.error('FATAL', e); process.exit(1) });
