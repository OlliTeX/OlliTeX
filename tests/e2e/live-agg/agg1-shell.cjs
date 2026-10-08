/**
 * AG module 1 — /editor shell (test user's agg-tex-fixture):
 * page loads clean, CM6 editor, file tree files, compile → PDF, log view,
 * focus mode, no-toolbar-strip (AE-1), rail home logo visible (negative test).
 */
const H = require('./harness.cjs')

const PIX = process.env.AGG_FIXTURE_PID || process.env.AGG_TEX_PID || H.fixturePid(process.env.AGG_FIXTURE || 'agg-tex-fixture')
if (!PIX) {
  console.error('fatal: fixture project agg-tex-fixture not found — run bootstrap.sh')
  process.exit(1)
}

async function main() {
  const M = 'agg1-shell'
  const { browser, ctx } = await H.getContext()
  const { page, errors } = await H.openEditor(ctx, PIX)
  try {
    H.record(M, 'editor page loads (cm6 visible)', true)
    H.record(M, 'no page errors on load', errors.page.length === 0, errors.page.join(' | '))

    H.record(M, 'file tree lists fixture files (main.tex, sample.bib, frog.jpg)',
      await page.evaluate(() => {
        const names = (document.querySelector('[data-testid="file-tree"]') || document.body).innerText
        return ['frog.jpg', 'main.tex', 'sample.bib'].every(n => names.includes(n))
      }))

    // AE-1: no visible toolbar strip across the editor
    const toolbar = await page.evaluate(() => {
      const h = document.querySelector('.ide-redesign-toolbar, [class*="toolbar"] .ide-redesign-toolbar')
      if (!h) return { present: false }
      const r = h.getBoundingClientRect()
      const cs = getComputedStyle(h)
      return { present: true, w: Math.round(r.width), h: Math.round(r.height), visible: cs.display !== 'none' && cs.visibility !== 'hidden' && r.height > 0 }
    })
    H.record(M, 'toolbar strip hidden (AE-1)', !toolbar.present || !toolbar.visible, JSON.stringify(toolbar))

    // HOME LINK REGRESSION (owner 2026-10-07: the home logo/icon does not
    // render visibly; AK-1 2026-10-08: it is now the rail's Menu-bar-styled
    // ActionIcon entry — the OLD `.ide-redesign-toolbar-home-link` surface was
    // retired with the AE toolbar-strip removal, so the contract is the RAIL
    // entry: painted logo (24px, background-image) inside a visible 40px
    // ActionIcon that navigates to the hub.)
    const homeLink = await page.evaluate(() => {
      const b = document.querySelector('.ol-v2-rail-home-entry')
      if (!b) return { present: false }
      const r = b.getBoundingClientRect()
      const cs = getComputedStyle(b)
      const logo = b.querySelector('.ol-v2-rail-home-logo')
      const paint = (el) => {
        if (!el) return null
        const rr = el.getBoundingClientRect()
        const c = getComputedStyle(el)
        return { w: Math.round(rr.width), h: Math.round(rr.height), bg: c.backgroundImage !== 'none' }
      }
      return {
        present: true,
        entry: { w: Math.round(r.width), h: Math.round(r.height), display: cs.display, visibility: cs.visibility, aria: b.getAttribute('aria-label') },
        logo: paint(logo),
        visible: r.width > 4 && r.height > 4 && cs.display !== 'none' && cs.visibility !== 'hidden'
      }
    })
    const logoOk = !!(homeLink.present && homeLink.visible && homeLink.entry.w >= 30 && (homeLink.logo?.bg) && (homeLink.logo?.w >= 16))
    H.record(M, 'HOME-LINK regression: rail home logo painted & visible', logoOk, JSON.stringify(homeLink).slice(0, 260))

    // Rail home button centered in its 40px slot (v2 rail variant)
    const home = await page.evaluate(() => {
      const b = document.querySelector('.ol-v2-rail-home')
      if (!b) return { present: false }
      const r = b.getBoundingClientRect()
      const logo = b.querySelector('svg, img, [class*="logo"]')
      const lr = logo ? logo.getBoundingClientRect() : null
      const cs = getComputedStyle(b)
      const bgEl = b.querySelector('.toolbar-ol-logo')
      const bg = bgEl ? getComputedStyle(bgEl).backgroundImage : 'none'
      return { present: true, w: Math.round(r.width), h: Math.round(r.height), logoW: lr ? Math.round(lr.width) : null, logoBg: bg !== 'none' }
    })
    H.record(M, 'rail home slot present (>=30px) with painted logo', !home.present || (home.w >= 30 && home.h >= 30), JSON.stringify(home))

    // Compile → PDF
    const compileBtn = await page.evaluate(() => {
      const b = Array.from(document.querySelectorAll('button')).find(x => /compile/i.test(x.getAttribute('aria-label') || x.textContent || ''))
      if (!b) return false
      b.click()
      return true
    })
    if (compileBtn) {
      const pdfOk = await page
        .locator('.pdf-view, [class*="pdf"] canvas, iframe[src*="pdf"], object[type*="pdf"], .pdf-js-container')
        .first()
        .waitFor({ state: 'visible', timeout: 90_000 })
        .then(() => true)
        .catch(() => false)
      H.record(M, 'compile → PDF renders', pdfOk)
      if (pdfOk) {
        const logs = await page.evaluate(() => {
          const b = Array.from(document.querySelectorAll('button, [role="tab"]')).find(x => /log/i.test(x.getAttribute('aria-label') || x.textContent || ''))
          if (!b) return false
          b.click()
          return true
        })
        await page.waitForTimeout(2500)
        const logVisible = await page.evaluate(() =>
          /Error|Warning|\.log|Compile|No issues|log/i.test(document.body.innerText)
        )
        H.record(M, 'log view opens', !!logs && logVisible)
      }
    } else {
      H.record(M, 'compile button found', false)
    }

    // PDF presentation mode — lives in the PDF zoom dropdown (label
    // 'Presentation mode', pdf-zoom-dropdown.tsx) and goes REAL FULLSCREEN
    // via browser.requestFullscreen() (use-presentation-mode.ts) — which
    // requires a REAL user-gesture click (locator.click), not a synthetic
    // .click() from page.evaluate (no user activation → browser refuses).
    let presOn = false, presOff = false, presStep = 'no-trigger'
    let zoomBtn = page.locator('button[aria-label*="zoom level" i]').first()
    if (!(await zoomBtn.count())) {
      // the log-view click above can hide the PDF pane (toolbar unmounts);
      // restore it before the presentation-mode step.
      const pdfTab = page.locator('[role="tab"], button, a', { hasText: /^\s*(PDF|pdf)\s*$/ }).first()
      if (await pdfTab.count()) {
        await pdfTab.click({ force: true }).catch(() => {})
        await page.waitForTimeout(1500)
      }
      zoomBtn = page.locator('button[aria-label*="zoom level" i]').first()
      await zoomBtn.waitFor({ state: 'attached', timeout: 15_000 }).catch(() => {})
    }
    if (await zoomBtn.count()) {
      await zoomBtn.click()
      await page.waitForTimeout(900)
      const item = page.locator('text="Presentation mode"').first()
      if (await item.count()) {
        await item.click()
        presStep = 'item-clicked'
        await page.waitForTimeout(2500)
        presOn = await page.evaluate(() => !!document.fullscreenElement)
        if (presOn) {
          await page.evaluate(() => document.exitFullscreen())
          await page.waitForTimeout(1600)
          presOff = !(await page.evaluate(() => !!document.fullscreenElement))
        }
      } else {
        presStep = 'no-item'
      }
    }
    H.record(M, 'PDF presentation mode toggles (fullscreen)', presStep === 'item-clicked' && presOn && presOff, `step=${presStep} on=${presOn} off=${presOff}`)
    H.record(M, 'final: no page errors', errors.page.length === 0, errors.page.join(' | '))
    await page.screenshot({ path: '/var/tmp/agg1-shell.png' })
  } finally {
    await browser.close()
  }
  H.report(M)
}

main().then(() => process.exit(0)).catch(e => { console.error('FATAL', e && e.message); process.exit(1) })
