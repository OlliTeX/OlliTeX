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
    // The SVG surface is the two-stage toast-svg editor (owner AD 2026-10-07):
    // dblclick opens the FILE VIEW (preview + header), whose header carries
    // the "Edit SVG" button (fileViewButtons hook, next to Download). The old
    // svgedit-iframe surface is retired.
    for (let i = 0; i < 3; i++) {
      await svgFile.dblclick({ force: true }).catch(() => {})
      await page.waitForTimeout(4000)
      if ((await page.locator('button:has-text("Edit SVG")').count()) > 0) break
      await svgFile.click({ force: true }).catch(() => {})
      await page.waitForTimeout(2500)
    }
    const svg404 = bad404.filter(u => /svgedit|svg/.test(u))
    H.record(M, 'AD-3 svg file view opens WITHOUT 404s', svg404.length === 0, JSON.stringify(svg404).slice(0, 200))
    // Stage 1: the file view renders an <img> preview AND the Edit SVG button.
    const stage1 = await page.evaluate(() => {
      const preview = document.querySelector('img[src*="diagram.svg"], img[src*=".svg"], svg, object[data*="svg"], embed[src*="svg"]')
      const btn = Array.from(document.querySelectorAll('button')).find(b => /edit\s*svg/i.test(b.textContent || ''))
      return { preview: !!preview, editSvgButton: !!btn }
    })
    H.record(M, 'AD-3b svg file view: preview + "Edit SVG" header button', stage1.preview && stage1.editSvgButton, JSON.stringify(stage1))
    // Stage 2: the full-size editor modal (source textarea + live preview + Save).
    await page.locator('button:has-text("Edit SVG")').first().click()
    await page.waitForTimeout(3500)
    const svgApp = await page.evaluate(() => {
      const modal = document.querySelector('.toast-svg-editor-modal, [class*="toast-svg"]')
      if (!modal) return { modal: false }
      const r = modal.getBoundingClientRect()
      const ta = modal.querySelector('textarea')
      const img = modal.querySelector('img')
      const buttons = Array.from(modal.querySelectorAll('button')).map(b => (b.textContent || '').trim()).filter(Boolean)
      return { modal: true, w: Math.round(r.width), h: Math.round(r.height), hasTextarea: !!ta, hasPreviewImg: !!img, hasSave: buttons.some(b => /save/i.test(b)), hasClose: buttons.some(b => /close/i.test(b)), taLen: ta ? ta.value.length : 0 }
    })
    H.record(M, 'AD-4 svg stage-2 editor modal rendered (source+preview+Save)', svgApp.modal && svgApp.hasTextarea && svgApp.hasPreviewImg && svgApp.hasSave, JSON.stringify(svgApp).slice(0, 220))
    // Live-preview sanity: append a marker comment to the source — the
    // preview <img> src must change (re-renders from the edited source).
    let livePreviewOk = false
    if (svgApp.modal && svgApp.hasTextarea) {
      const before = await page.evaluate(() => {
        const modal = document.querySelector('.toast-svg-editor-modal, [class*="toast-svg"]')
        const img = modal ? modal.querySelector('img') : null
        return img ? img.src : null
      })
      await page.evaluate(() => {
        const modal = document.querySelector('.toast-svg-editor-modal, [class*="toast-svg"]')
        const ta = modal ? modal.querySelector('textarea') : null
        if (ta) {
          const next = ta.value.includes('</svg>')
            ? ta.value.replace('</svg>', '<line x1="0" y1="0" x2="50" y2="50" stroke="red" stroke-width="4"/>\n</svg>')
            : ta.value + ' <!-- agg-live -->'
          ta.value = next
          ta.dispatchEvent(new Event('input', { bubbles: true }))
        }
      })
      await page.waitForTimeout(3000)
      const after = await page.evaluate(() => {
        const modal = document.querySelector('.toast-svg-editor-modal, [class*="toast-svg"]')
        const img = modal ? modal.querySelector('img') : null
        return img ? img.src : null
      })
      livePreviewOk = !!(before && after && before !== after)
      H.record(M, 'AD-4b svg live preview re-renders on source edit', livePreviewOk, `before=${(before || '').slice(0, 80)} after=${(after || '').slice(0, 80)}`)
    }
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
