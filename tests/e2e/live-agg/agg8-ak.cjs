/*
 * ak (owner 2026-10-08) — AK queue live verification.
 *
 * One module that pins EVERY open AK item against the live stack:
 *   AK-1  rail home entry: Menu-bar-styled ActionIcon (40x40) with the
 *         Overleaf logo PAINTED (24px, background-image) + navigates to
 *         /hub#/projects (AK-1's CSS was scoped under .ide-redesign-toolbar,
 *         so the rail logo rendered as an invisible 0x0 span — now unscoped
 *         via editor-v2-tokens.css).
 *   AK-3  hotkeys modal widened (size 1000).
 *   AK-4  editor settings modal widened (size 1200).
 *   AK-5  settings modal → Compiler: the sandbox compile-image select
 *         ("TeX Live Version") with the instance's images
 *         (texlive/texlive:latest-full "TeXLive rolling",
 *          texlive/texlive:TL2025-historic "TeXLive 2025").
 *   AK-6  the "Account settings" section is GONE from the editor settings.
 *   AK-7  Python Runner: opening a .py document renders the split
 *         editor + output pane (overleaf-code split test is enabled).
 *   AK-8  File menu → Download group: zip / PDF / docx / markdown / html.
 *   AK-9  the image editor stage-2 modal carries NO double scrollbar
 *         (exactly one scrollable region inside the modal body).
 *   AK-10 .tikz double-click: the TikZ visual editor RE-MOUNTS for every
 *         document (one.tikz → two.tikz switches the whole embed, not just
 *         the source).
 *   AK-11 .drawio double-click: the draw.io canvas editor (vendored app in
 *         an iframe) boots and shows the document.
 *
 * The owner's live project is never touched: scratch projects and the two
 * AG fixtures (tex + typst per AK-12) are the test targets.
 */
const H = require('./harness.cjs')
const M = 'agg8-ak'
const TEX = H.fixturePid('agg-tex-fixture')
const TYPST = H.fixturePid('agg-typst-fixture')

async function settle(page, ms) {
  await page.waitForTimeout(ms)
}
async function clickText(page, text) {
  await page.locator(`text="${text}"`).first().click({ timeout: 15000 })
}

async function openMenuBar(page) {
  const toggle = page.locator('button[aria-label="Menu bar"]').first()
  await toggle.click()
  await settle(page, 900)
  if (!(await page.evaluate(() => document.body.classList.contains('ol-v2-menubar-open')))) {
    await toggle.click()
    await settle(page, 1200)
  }
  return await page.evaluate(() => document.body.classList.contains('ol-v2-menubar-open'))
}

async function closeMenuBar(page) {
  await page.evaluate(() => document.body.classList.remove('ol-v2-menubar-open'))
  await settle(page, 300)
  const toggle = page.locator('button[aria-label="Menu bar"]').first()
  if ((await toggle.count()) === 1) await toggle.click().catch(() => {})
  await settle(page, 400)
}

async function fileMenuItem(page, label) {
  // File menu (menu bar) → hover File, then click the item.
  await page.locator('text=File').first().click({ timeout: 15000 }).catch(() => {})
  await settle(page, 700)
  await page.locator(`text="${label}"`).first().click({ timeout: 15000 })
  await settle(page, 1500)
}

async function main() {
  const { browser, ctx } = await H.getContext()
  const { page, errors } = await H.openEditor(ctx, TEX)
  const PIX = TEX
  const scratch = []
  try {
    // ── AK-1: rail home entry painted ─────────────────────────────────────
    const home = await page.evaluate(() => {
      const b = document.querySelector('.ol-v2-rail-home-entry')
      if (!b) return { present: false }
      const r = b.getBoundingClientRect()
      const logo = b.querySelector('.ol-v2-rail-home-logo')
      const lr = logo ? logo.getBoundingClientRect() : null
      const lcs = logo ? getComputedStyle(logo) : null
      return {
        present: true,
        entry: { w: Math.round(r.width), h: Math.round(r.height) },
        logo: lr
          ? { w: Math.round(lr.width), h: Math.round(lr.height), bg: lcs ? lcs.backgroundImage.slice(0, 60) : '' }
          : null
      }
    })
    H.record(M, 'AK-1 rail home entry present, logo painted (24px bg)', home.present && home.logo && home.logo.w >= 16 && home.logo.bg.length > 4, JSON.stringify(home))
    // + it navigates to the hub projects view.
    await page.evaluate(() => {
      const b = document.querySelector('.ol-v2-rail-home-entry')
      if (b) b.click()
    })
    await page.waitForTimeout(4000)
    const hubUrl = page.url()
    const hubOk = /\/hub/.test(hubUrl)
    H.record(M, 'AK-1 rail home navigates to /hub', hubOk, hubUrl)
    if (hubOk) {
      await page.goto(`${H.BASE}/editor/${PIX}`, { waitUntil: 'domcontentloaded' })
      await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
      await settle(page, 2000)
    }

    // ── AK-8: File → Download group (zip/pdf/docx/md/html) ────────────────
    const dl = await (async () => {
      await openMenuBar(page)
      await fileMenuItem(page, 'Download')
      await settle(page, 1200)
      const out = await page.evaluate(() => {
        const items = Array.from(document.querySelectorAll('[role="menuitem"], [role="menuitemcheckbox"], [role="menuitemradio"], .mantine-Menu-item, .mantine-Dropdown-item, [class*="menu"] [class*="item"]'))
          .map(e => (e.textContent || '').trim())
          .filter(t => t && t.length < 60)
        const seen = new Set()
        return Array.from(new Set(items)).filter(t => (seen.add(t), t))
      })
      await closeMenuBar(page)
      return out
    })()
    const want = [/zip|source/i, /pdf/i, /docx/i, /markdown|md\b/i, /html/i]
    const dlOk = want.every(re => dl.some(t => re.test(t)))
    H.record(M, 'AK-8 File→Download: zip/pdf/docx/markdown/html all present', dlOk, JSON.stringify(dl).slice(0, 300))

    // ── AK-4 + AK-5 + AK-6: the widened settings modal ───────────────────
    await openMenuBar(page)
    await fileMenuItem(page, 'Settings')
    await settle(page, 2500)
    const settingsModal = await page.evaluate(() => {
      const modal = Array.from(document.querySelectorAll('[role="dialog"], .mantine-Modal-root, [class*="modal"]')).find(el => {
        const r = el.getBoundingClientRect()
        return r.width > 300 && /settings/i.test(el.textContent || '')
      })
      if (!modal) return { present: false }
      const r = modal.getBoundingClientRect()
      const text = modal.textContent || ''
      return {
        present: true,
        w: Math.round(r.width),
        h: Math.round(r.height),
        hasAccountSettings: /account settings/i.test(text),
        hasCompiler: /compiler/i.test(text),
        hasTexLiveVersion: /tex\s*live\s*version/i.test(text)
      }
    })
    await page.screenshot({ path: '/var/tmp/agg8-settings.png' })
    H.record(M, 'AK-4 settings modal is wide (>=900px)', settingsModal.present && settingsModal.w >= 900, JSON.stringify(settingsModal).slice(0, 220))
    H.record(M, 'AK-6 "Account settings" section removed from editor settings', settingsModal.present && !settingsModal.hasAccountSettings, JSON.stringify({ account: settingsModal.hasAccountSettings }))
    // AK-5: the image select options — the select must sit INSIDE the
    // "TeX Live Version" setting row (a naive first-<select> grabs the PDF
    // viewer theme select: Overleaf/Browser).
    const imgSel = await page.evaluate(() => {
      const labels = Array.from(document.querySelectorAll('label, [class*="setting"], [class*="label"], dt, span, div'))
        .filter(e => /tex\s*live\s*version/i.test((e.textContent || '').trim()) && (e.textContent || '').trim().length < 80)
      for (const lbl of labels) {
        let row = lbl.closest('[class*="setting"]') || lbl.parentElement
        for (let hop = 0; row && hop < 5; hop++) {
          const sel = row.querySelector('select')
          const opts = sel ? Array.from(sel.options).map(o => o.textContent) : []
          if (sel && opts.some(o => /texlive/i.test(o))) {
            return { found: true, anchor: (lbl.textContent || '').trim().slice(0, 30), options: opts, value: sel.value, disabled: sel.disabled }
          }
          if (row.parentElement) row = row.parentElement
        }
      }
      return { found: false, labels: labels.slice(0, 4).map(l => (l.textContent || '').trim().slice(0, 40)) }
    })
    H.record(M, 'AK-5 Compiler: TeX Live Version select with instance images', imgSel.found && imgSel.options.some(o => /rolling/i.test(o)) && imgSel.options.some(o => /2025/i.test(o)), JSON.stringify(imgSel).slice(0, 240))
    // Close the modal.
    await page.keyboard.press('Escape')
    await settle(page, 1200)
    await closeMenuBar(page)

    // ── AK-3: hotkeys modal widened ───────────────────────────────────────
    // Open via the menu bar's Help menu → "Keyboard shortcuts".
    await openMenuBar(page)
    await page.locator('text=Help').first().click({ timeout: 15000 }).catch(() => {})
    await settle(page, 900)
    await page.locator('text=Keyboard shortcuts').first().click({ timeout: 15000 }).catch(() => {})
    await settle(page, 2500)
    const hotkeys = await page.evaluate(() => {
      const candidates = Array.from(document.querySelectorAll('[role="dialog"], .mantine-Modal-root, [class*="modal"]'))
      const modal = candidates.find(el => {
        const r = el.getBoundingClientRect()
        return r.width > 250 && /shortcut|hotkey/i.test(el.textContent || '')
      })
      if (!modal) return { opened: false, w: 0 }
      const r = modal.getBoundingClientRect()
      return { opened: true, w: Math.round(r.width), snippet: (modal.textContent || '').slice(0, 60) }
    })
    await closeMenuBar(page)
    await page.screenshot({ path: '/var/tmp/agg8-hotkeys.png' })
    H.record(M, 'AK-3 hotkeys modal widened (>=800px)', hotkeys.opened && hotkeys.w >= 800, JSON.stringify(hotkeys).slice(0, 200))
    await page.keyboard.press('Escape')


    // ── AK-9: image editor stage-2 modal has no double scrollbar ─────────
    const imgFile = page.locator('[data-testid="file-tree"] .entity-name', { hasText: 'frog.jpg' }).first()
    let imgModal = { opened: false }
    for (let i = 0; i < 3 && !imgModal.opened; i++) {
      await imgFile.dblclick({ force: true }).catch(() => {})
      await settle(page, 3500)
      const btn = page.locator('button:has-text("Edit image")').first()
      if ((await btn.count()) === 0) continue
      await btn.click().catch(() => {})
      await settle(page, 4000)
      imgModal = await page.evaluate(() => {
        const modal = document.querySelector('.toast-image-editor-modal, [class*="toast-image"]')
        if (!modal) return { opened: false }
        const r = modal.getBoundingClientRect()
        const body = modal.querySelector('.modal-body, [class*="body"]') || modal
        const scrollables = []
        modal.querySelectorAll('*').forEach(el => {
          const c = getComputedStyle(el)
          if ((c.overflowY === 'auto' || c.overflowY === 'scroll') && el.scrollHeight > el.clientHeight + 2) {
            scrollables.push({ cls: (el.className || '').toString().slice(0, 40), h: Math.round(el.clientHeight) })
          }
        })
        return {
          opened: true,
          w: Math.round(r.width),
          h: Math.round(r.height),
          bodyOverflow: body ? getComputedStyle(body).overflowY : null,
          scrollableCount: scrollables.length,
          scrollables: scrollables.slice(0, 6)
        }
      })
      if (imgModal.opened) await page.screenshot({ path: '/var/tmp/agg8-image-modal.png' })
      const closeBtn = page.locator('.toast-image-editor-modal button:has-text("Close"), [class*="toast-image"] button:has-text("Close")').first()
      if ((await closeBtn.count()) > 0) await closeBtn.click().catch(() => {})
      await settle(page, 1500)
    }
    H.record(M, 'AK-9 image editor modal: at most one scrollable region (no double scrollbar)', imgModal.opened && imgModal.scrollableCount <= 1, JSON.stringify(imgModal).slice(0, 300))

    // ── AK-10: .tikz re-mounts on switch (one.tikz → two.tikz) ───────────
    const s1 = await H.newScratch(ctx, `akk-tikz-${Date.now() % 100000}`)
    scratch.push(s1.pid)
    const cs1 = await H.csrfOf(page)
    await H.ensureFile(ctx, s1.pid, 'one.tikz', '\\documentclass{standalone}\n\\usepackage{tikz}\n\\begin{document}\n\\begin{tikzpicture}\n\\node[draw] {AKK-ONE-MARKER};\n\\end{tikzpicture}\n\\end{document}\n', 'application/octet-stream', cs1)
    await H.ensureFile(ctx, s1.pid, 'two.tikz', '\\documentclass{standalone}\n\\usepackage{tikz}\n\\begin{document}\n\\begin{tikzpicture}\n\\node[draw] {AKK-TWO-MARKER};\n\\end{tikzpicture}\n\\end{document}\n', 'application/octet-stream', cs1)
    await settle(page, 2000)
    // open one.tikz in the live editor tab (the AG fixture) via a fresh page.
    const tikzPage = await ctx.newPage()
    tikzPage.on('pageerror', e => errors.page.push(e.message.slice(0, 200)))
    await tikzPage.goto(`${H.BASE}/editor/${s1.pid}`, { waitUntil: 'domcontentloaded' })
    await tikzPage.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
    await settle(tikzPage, 1500)
    const openTikz = async (name) => {
      const row = tikzPage.locator('[data-testid="file-tree"] .entity-name', { hasText: name }).first()
      await row.dblclick({ force: true }).catch(() => {})
      for (let i = 0; i < 14; i++) {
        await settle(tikzPage, 1000)
        const done = await tikzPage.evaluate(n => {
          const v = document.querySelector('.tikz-viewer')
          if (!v) return false
          const doc = v.querySelector('iframe')
          if (doc && doc.contentDocument) {
            try { if ((doc.contentDocument.body && doc.contentDocument.body.textContent || '').includes(n)) return true } catch (e) {}
          }
          return (v.textContent || '').includes(n)
        }, name + '-MARKER')
        if (done) return true
      }
      return false
    }
    const viewerSig = async (pg) => {
      const v = await pg.evaluate(() => {
        const el = document.querySelector('.tikz-viewer')
        if (!el) return null
        const ifr = el.querySelector('iframe')
        let inner = ''
        if (ifr) {
          try { inner = (ifr.contentDocument ? (ifr.contentDocument.title || '') + '|' + (ifr.contentDocument.body ? ifr.contentDocument.body.querySelectorAll('*').length : 0) : 'x') }
          catch (e) { inner = 'x' }
        }
        return { src: ifr ? ifr.src : null, inner, sig: (el.outerHTML || '').length }
      })
      return v
    }
    const oneLoaded = await openTikz('one')
    H.record(M, 'AK-10 one.tikz opens in the TikZ visual editor', oneLoaded, '')
    const viewerOne = await viewerSig(tikzPage)
    const twoLoaded = await openTikz('two')
    const viewerTwo = await viewerSig(tikzPage)
    const remounted = twoLoaded && ( !viewerOne || !viewerTwo || (viewerOne && viewerTwo && (viewerOne.inner !== viewerTwo.inner || viewerOne.sig !== viewerTwo.sig)) )
    H.record(M, 'AK-10 two.tikz switch RE-MOUNTS the visual editor (not stale source)', twoLoaded && remounted, JSON.stringify({ viewerOne, viewerTwo }).slice(0, 300))
    await tikzPage.screenshot({ path: '/var/tmp/agg8-tikz.png' })
    await tikzPage.close().catch(() => {})

    // ── AK-11: .drawio boots the draw.io canvas editor ────────────────────
    const s2 = await H.newScratch(ctx, `akk-drawio-${Date.now() % 100000}`)
    scratch.push(s2.pid)
    const cs2 = await H.csrfOf(page)
    const drawioXml = '<mxfile host="app.diagrams.net"><diagram id="akk" name="Page-1"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="AKK-DRWIO" style="rounded=0" vertex="1" parent="1"><mxGeometry x="40" y="40" width="120" height="60" as="geometry"/></mxCell></root></mxGraphModel></diagram></mxfile>'
    await H.ensureFile(ctx, s2.pid, 'fig.drawio', drawioXml, 'application/octet-stream', cs2)
    await settle(page, 2000)
    const drwPage = await ctx.newPage()
    drwPage.on('pageerror', e => errors.page.push(e.message.slice(0, 200)))
    await drwPage.goto(`${H.BASE}/editor/${s2.pid}`, { waitUntil: 'domcontentloaded' })
    await drwPage.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
    await settle(drwPage, 1500)
    const row2 = drwPage.locator('[data-testid="file-tree"] .entity-name', { hasText: 'fig.drawio' }).first()
    await row2.dblclick({ force: true }).catch(() => {})
    let drawio = { booted: false }
    for (let i = 0; i < 20; i++) {
      await settle(drwPage, 1000)
      drawio = await drwPage.evaluate(() => {
        const v = document.querySelector('.drawio-viewer')
        if (!v) return { booted: false }
        const ifr = v.querySelector('iframe')
        if (!ifr) return { booted: false }
        const r = ifr.getBoundingClientRect()
        let appReady = false
        try {
          const d = ifr.contentDocument
          appReady = !!(d && d.querySelector('[id="editor"], #graph, .ge diagramEditor, mxgraph'))
        } catch (e) { appReady = true }
        return { booted: true, iframeW: Math.round(r.width), iframeH: Math.round(r.height), appReady }
      })
      if (drawio.booted && (drawio.appReady || i > 4)) break
    }
    await drwPage.screenshot({ path: '/var/tmp/agg8-drawio.png' })
    H.record(M, 'AK-11 .drawio double-click boots the draw.io canvas editor', drawio.booted && drawio.iframeW > 300, JSON.stringify(drawio))
    await drwPage.close().catch(() => {})

    // ── AK-7: Python Runner split editor + output pane ────────────────────
    const s3 = await H.newScratch(ctx, `akk-py-${Date.now() % 100000}`)
    scratch.push(s3.pid)
    const cs3 = await H.csrfOf(page)
    await H.ensureFile(ctx, s3.pid, 'script.py', 'print("AKK-PY-OK", 20 * 21)\n', 'application/octet-stream', cs3)
    await settle(page, 2000)
    const pyPage = await ctx.newPage()
    pyPage.on('pageerror', e => errors.page.push(e.message.slice(0, 200)))
    await pyPage.goto(`${H.BASE}/editor/${s3.pid}`, { waitUntil: 'domcontentloaded' })
    await pyPage.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
    await settle(pyPage, 1500)
    const pyRow = pyPage.locator('[data-testid="file-tree"] .entity-name', { hasText: 'script.py' }).first()
    await pyRow.dblclick({ force: true }).catch(() => {})
    let py = { split: false }
    for (let i = 0; i < 8; i++) {
      await settle(pyPage, 1000)
      py = await pyPage.evaluate(() => {
        const split = document.querySelector('.ide-redesign-python-editor-split')
        if (!split) return { split: false }
        const out = split.querySelector('[class*="output"]')
        const run = Array.from(split.querySelectorAll('button')).find(b => /run/i.test(b.textContent || ''))
        return { split: true, hasOutputPane: !!out, hasRun: !!run }
      })
      if (py.split) break
    }
    await pyPage.screenshot({ path: '/var/tmp/agg8-python.png' })
    H.record(M, 'AK-7 .py document renders the Python split editor (source + output + Run)', py.split && py.hasOutputPane && py.hasRun, JSON.stringify(py))
    await pyPage.close().catch(() => {})

    // ── AK-12: the editor e2e runs on BOTH tex and typst files ────────────
    // Same compile→PDF contract as agg1 (proven), pinned for the typst
    // fixture so the matrix explicitly covers typst end-to-end.
    const typPage = await ctx.newPage()
    typPage.on('pageerror', e => errors.page.push(e.message.slice(0, 200)))
    await typPage.goto(`${H.BASE}/editor/${TYPST}`, { waitUntil: 'domcontentloaded' })
    await typPage.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30000 })
    await settle(typPage, 1500)
    const compileBtn = await typPage.evaluate(() => {
      const b = Array.from(document.querySelectorAll('button')).find(x => /compile/i.test(x.getAttribute('aria-label') || x.textContent || ''))
      if (b) b.click()
      return !!b
    })
    const typOk = compileBtn && (await typPage
      .locator('.pdf-view, [class*="pdf"] canvas, iframe[src*="pdf"], object[type*="pdf"], .pdf-js-container')
      .first()
      .waitFor({ state: 'visible', timeout: 90000 })
      .then(() => true)
      .catch(() => false))
    H.record(M, 'AK-12 typst fixture: compile → PDF renders (e2e on both tex+typst)', typOk, '')
    await typPage.close().catch(() => {})

    H.record(M, 'no page errors across the AK module', errors.page.length === 0, errors.page.slice(0, 4).join(' | ').slice(0, 220))
  } finally {
    for (const p of scratch) await H.trash(ctx, p).catch(() => {})
    await browser.close().catch(() => {})
  }
  H.report(M)
}

main()
  .then(() => process.exit(0))
  .catch(e => {
    console.error('FATAL', e)
    process.exit(1)
  })
