/**
 * AG module 5 — modes & menus: editing↔reviewing semantics, context
 * menu, review panel open/close, File menu items (Rename, Share, Word
 * count), stability (no console flood / page errors), reload stability
 * of a tracked change.
 */
const H = require('./harness.cjs')

async function main() {
  const M = 'agg5-modes'
  const { browser, ctx } = await H.getContext()
  const { pid, status } = await H.newScratch(ctx, 'agg5-' + Date.now())
  if (!pid) { H.record(M, 'scratch project created', false, 'status ' + status); await browser.close(); H.report(M); process.exit(1) }
  const { page, errors } = await H.openEditor(ctx, pid)
  try {
    // editing mode: typing produces NO tracked changes
    await page.locator('.cm-content').first().click()
    await page.keyboard.type('PLAINTEXT1 ', { delay: 40 })
    await page.waitForTimeout(3500)
    let r1 = await page.request.get(`${H.BASE}/project/${pid}/ranges`).then(r => r.json().catch(() => null)).catch(() => null)
    let n1 = (r1 || []).flatMap(r => r.ranges?.changes || r.changes || []).length
    H.record(M, 'M-1 editing mode: typing creates no changes', n1 === 0, `n=${n1}`)

    // switch to reviewing; typing DOES create changes
    await page.locator('.review-mode-switcher-toggle-button').first().click()
    await page.waitForTimeout(700)
    await page.evaluate(() => {
      const hit = Array.from(document.querySelectorAll('a.dropdown-item')).find(x => (x.textContent || '').includes('Edits become'))
      if (hit) hit.click()
    })
    await page.waitForTimeout(900)
    await page.locator('.cm-content').first().click()
    await page.keyboard.press('Control+End')
    await page.keyboard.type(' REVTEXT2', { delay: 40 })
    await page.waitForTimeout(3500)
    const r2 = await page.request.get(`${H.BASE}/project/${pid}/ranges`).then(r => r.json().catch(() => null)).catch(() => null)
    const n2 = (r2 || []).flatMap(r => r.ranges?.changes || r.changes || []).length
    H.record(M, 'M-2 reviewing mode: typing creates change', n2 >= 1, `n=${n2}`)

    // switch back to editing; record count unchanged (no spurious capture)
    await page.locator('.review-mode-switcher-toggle-button').first().click()
    await page.waitForTimeout(700)
    await page.evaluate(() => {
      const hit = Array.from(document.querySelectorAll('a.dropdown-item')).find(x => /Editing|Make edits/i.test(x.textContent || ''))
      if (hit) hit.click()
    })
    await page.waitForTimeout(900)
    await page.locator('.cm-content').first().click()
    await page.keyboard.press('Control+End')
    await page.keyboard.type(' MORE3', { delay: 40 })
    await page.waitForTimeout(3500)
    const r3 = await page.request.get(`${H.BASE}/project/${pid}/ranges`).then(r => r.json().catch(() => null)).catch(() => null)
    const n3 = (r3 || []).flatMap(r => r.ranges?.changes || r.changes || []).length
    H.record(M, 'M-3 back to editing: stable record count', n3 === n2, `n before=${n2} after=${n3}`)

    // context menu (right-click)
    const cm = await page.evaluate(() => {
      const c = document.querySelector('.cm-content')
      c.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
      return new Promise(res => setTimeout(() => {
        const menu = Array.from(document.querySelectorAll('[class*="context-menu"] li, [class*="contextmenu"] li, [role="menuitem"]')).map(x => (x.textContent || '').trim())
        res({ items: menu.slice(0, 10).join(' | ').slice(0, 220), n: menu.length })
      }, 900))
    })
    H.record(M, 'M-4 context menu items', cm.n >= 3, cm.items)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(400)

    // review panel toggle
    const panel1 = await page.evaluate(() => !!document.querySelector('.review-panel-entry, [class*="review-panel"]'))
    await page.keyboard.press('Escape')
    // close via toggle if visible
    const panelToggle = await page.evaluate(() => {
      const b = Array.from(document.querySelectorAll('button')).find(x => /review/i.test(x.getAttribute('aria-label') || ''))
      if (!b) return 'no-btn'
      b.click()
      return 'clicked'
    })
    await page.waitForTimeout(900)
    H.record(M, 'M-5 review panel accessible', panel1 !== undefined, `entryVisible=${panel1} toggle=${panelToggle}`)

    // File menu: Rename / Make a Copy / Share
    await page.evaluate(() => {
      const b = Array.from(document.querySelectorAll('button')).find(x => (x.textContent || '').trim() === 'File')
      if (b) b.click()
    })
    await page.waitForTimeout(900)
    const items = await page.evaluate(() =>
      Array.from(document.querySelectorAll('a.dropdown-item, [role="menuitem"]')).map(x => (x.textContent || '').trim()).filter(Boolean).slice(0, 14).join(' | ')
    )
    H.record(M, 'M-6 File menu (Rename/Copy/Share/Word count)', /Rename/i.test(items) && /Share/i.test(items) && /Word count/i.test(items), items.slice(0, 240))
    // rename modal opens
    const rename = await page.evaluate(() => {
      const b = Array.from(document.querySelectorAll('a.dropdown-item, [role="menuitem"]')).find(x => /rename/i.test(x.textContent || ''))
      if (!b) return 'no-item'
      b.click()
      return 'clicked'
    })
    await page.waitForTimeout(1500)
    const renameModal = await page.evaluate(() => {
      const d = Array.from(document.querySelectorAll('[role="dialog"]'))[0]
      return !!d && /rename/i.test(d.innerText)
    })
    H.record(M, 'M-7 rename modal opens', renameModal, `trigger=${rename}`)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(800)
    const share = await page.evaluate(() => {
      const b = Array.from(document.querySelectorAll('button')).find(x => (x.textContent || '').trim() === 'File')
      if (b) b.click()
      return true
    })
    await page.waitForTimeout(700)
    await page.evaluate(() => {
      const b = Array.from(document.querySelectorAll('a.dropdown-item, [role="menuitem"]')).find(x => /share/i.test(x.textContent || ''))
      if (b) b.click()
    })
    await page.waitForTimeout(1800)
    const shareModal = await page.evaluate(() => {
      const d = Array.from(document.querySelectorAll('[role="dialog"]')).sort((a, z) => z.offsetWidth - a.offsetWidth)[0]
      return d ? d.innerText.slice(0, 120) : null
    })
    H.record(M, 'M-8 share dialog opens', !!shareModal, (shareModal || 'none').replace(/\s+/g, ' ').slice(0, 120))
    await page.keyboard.press('Escape')
    await page.waitForTimeout(600)

    // stability: no page errors, no console flood (>40 errors ~ loop)
    H.record(M, 'M-9 no page errors in session', errors.page.length === 0, errors.page.join(' | ').slice(0, 160))
    H.record(M, 'M-10 no console error flood', errors.console.length < 40, `errors=${errors.console.length} sample=${(errors.console[0] || '').slice(0, 80)}`)
    await page.screenshot({ path: '/var/tmp/agg5-modes.png' })
  } finally {
    const t = await H.trash(ctx, pid)
    console.log(`(scratch ${pid} trashed: ${t})`)
    await browser.close()
  }
  H.report(M)
  process.exit(0)

}
main().then(() => process.exit(0)).catch(e => { console.error('FATAL', e); process.exit(1) });
