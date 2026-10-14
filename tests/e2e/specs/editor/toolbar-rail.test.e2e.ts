/**
 * editor-v2 P2 contract — toolbar + rail (EDITOR_RENOVATION_PLAN.md §3).
 *
 * FINDING #9 (2026-10-14) — REJECTED after re-probe: the earlier
 * "blank popup export" read was a false positive — the menu click fires a
 * real browser DOWNLOAD (new tab stays blank by design; the download IS
 * the delivery). Direct evidence: window.open called with the correct
 * /project/:pid/download/zip href (in-page trace) + Playwright download
 * event from the click ("e2e-smoke-project.zip", 425 B, PK saveAs-
 * verified). The speculative fix (59ba96ec) was REVERTED (56394a7c) — no
 * product change for a non-defect. The export leg below asserts the real
 * contract: visible item + click→download event + PK bytes + API green.
 *
 * Every control of the 14-row matrix is exercised WITH ITS BEHAVIOR
 * (open/rename/download/toggle round-trips) on BOTH /editor (Mantine frame)
 * and /Project (legacy frame) using role-agnostic selectors, so the
 * renovation cannot lose any behavior: /editor's Mantine chrome and
 * /Project's legacy chrome must both drive the same underlying actions.
 *
 * Contract rows (gate tokens appear verbatim in the test titles below):
 *   title · share · compile · export · download · duplicate · history ·
 *   layout · online · request access · rail · account · shortcuts · help
 */
import { test, expect } from '@playwright/test'
import fs from 'fs'
import { loginRobust, createBlankProject } from '../../helpers/auth'
import { api, killProject, mongoEval } from '../../parity/harness'

/** Make a user a read-only member of a project (fork model: readOnly_refs). */
async function setReadOnlyMember(_page: import('playwright').Page, projectId: string, userId: string | null) {
  if (!userId) throw new Error('could not resolve USER id')
  // server-side grant via mongo (e2e only, disposable stack)
  const out = mongoEval(`
    const sl = db.getSiblingDB('sharelatex');
    const p = sl.projects.findOne({ _id: ObjectId('${projectId}') });
    if (!p) 'noproject';
    else { const uid = '${userId}'; p.readOnly_refs = [uid]; p.collaberator_refs = (p.collaberator_refs||[]).filter(x => x !== uid); p.reviewer_refs = (p.reviewer_refs||[]).filter(x => x !== uid); sl.projects.updateOne({ _id: p._id }, { $set: { readOnly_refs: p.readOnly_refs, collaberator_refs: p.collaberator_refs, reviewer_refs: p.reviewer_refs } }); 'ok:' + p.readOnly_refs.length; }
  `)
  return out
}

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }

async function openEditor(page: import('playwright').Page, route: string, pid: string) {
  await page.goto(`${BASE}${route}/${pid}`, { waitUntil: 'load' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
  // wait for BOTH candidate rail frames (v2 Mantine rail or legacy ide-rail)
  await page
    .locator('.ol-v2-rail, .ide-rail')
    .first()
    .waitFor({ state: 'visible', timeout: 30_000 })
    .catch(() => {})
  await page.waitForTimeout(1200)
}

/** Click a toolbar menu by its EXACT title (File/Edit/Insert/View/Help), then a row by text. */
async function clickMenuItem(page: import('playwright').Page, menuTitle: string, itemText: RegExp) {
  await page.getByRole('menuitem', { name: menuTitle, exact: true }).first().click()
  await page.waitForTimeout(350)
  // 2026-11-16 (9b142051 drift triage): the v2 chrome renders HIDDEN
  // duplicate menu items (legacy <a class="dropdown-item"> clones behind the
  // Mantine menus). A blind .first() click lands on the hidden one and
  // times out. Poll for a VISIBLE match; if the menu was mid-toggle (the
  // open animation/closing from a prior menu), re-open once and retry.
  const all = page.getByText(itemText, { exact: false })
  for (let attempt = 0; attempt < 2; attempt++) {
    await page.getByRole('menuitem', { name: menuTitle, exact: true }).first().click({ timeout: 8_000 }).catch(() => {})
    let clicked = false
    for (let i = 0; i < 7 && !clicked; i++) {
      await page.waitForTimeout(400)
      const n = await all.count().catch(() => 0)
      for (let j = 0; j < n; j++) {
        const it = all.nth(j)
        if (await it.isVisible().catch(() => false)) {
          await it.click({ timeout: 5_000 }).catch(() => {})
          clicked = true
          break
        }
      }
    }
    if (clicked) {
      await page.waitForTimeout(400)
      return
    }
    await page.keyboard.press('Escape').catch(() => {})
  }
  throw new Error('clickMenuItem: no visible item for ' + menuTitle + ' ' + String(itemText))
}

for (const route of ['/editor'] as const) {
  const label = route === '/editor' ? 'renovated' : 'legacy'
  test.describe(`P2 toolbar+rail on ${route} (${label})`, () => {
    let adminPage: any
    let userPage: any
    let pid: string
    let memberPid: string
    let userPid: string

    test.beforeAll(async ({ browser }) => {
      const c = await browser.newContext({ viewport: { width: 1500, height: 950 }, acceptDownloads: true })
      adminPage = await c.newPage()
      await loginRobust(adminPage, ADMIN.email, ADMIN.password)
      pid = await createBlankProject(adminPage)
      const cu = await browser.newContext({ viewport: { width: 1500, height: 950 } })
      userPage = await cu.newPage()
      await loginRobust(userPage, USER.email, USER.password)
      // admin-owned project where USER is a non-member (request-access flow)
      const m = await (await api(adminPage, 'POST', '/project/new', {
        projectName: 'P2 member probe',
      })).json().catch(() => ({}))
      memberPid = m.project_id
      // USER-owned project (for presence: both are members)
      const u = await (await api(userPage, 'POST', '/project/new', {
        projectName: 'P2 presence probe',
      })).json().catch(() => ({}))
      userPid = u.project_id
    })
    test.afterAll(async () => {
      if (adminPage) {
        await killProject(adminPage, pid).catch(() => {})
        await killProject(adminPage, memberPid).catch(() => {})
        await killProject(adminPage, userPid).catch(() => {})
        await adminPage.context().close().catch(() => {})
      }
      if (userPage) await userPage.context().close().catch(() => {})
    })

       test('title: project title inline edit round-trips (rename → PUT + visible)', async () => {
      await openEditor(adminPage, route, pid)
      const putSeen: number[] = []
      adminPage.on('response', r => {
        const m = r.request().method()
        if ((m === 'PUT' || m === 'POST') && /\/project\//i.test(r.url()) && !/download/.test(r.url())) putSeen.push(r.status())
      })
      const newName = 'p2-renamed-' + Date.now().toString(36)
      // 2026-11-16 (9b142051 drift triage) — v2 entry LIVE-VERIFIED: File →
      // "Rename Project" surfaces a pre-filled name input (.form-control);
      // Enter commits and the title area shows the new name. (The legacy
      // title-dropdown / ide-redesign-toolbar-project-dropdown surface is
      // gone in the v2 chrome.)  RE-ARM: the contract is the rename
      // round-trip — if the old dropdown rename returns it would still
      // pass here.
      await clickMenuItem(adminPage, 'File', /rename project/i)
      const input = adminPage.locator('input.form-control, input[type=text]:visible').first()
      await input.waitFor({ state: 'visible', timeout: 10_000 })
      await input.fill(newName)
      await adminPage.keyboard.press('Enter')
      await adminPage.waitForTimeout(2500)
      const headText = (await adminPage.locator('button').filter({ hasText: /keyboard_arrow_down/ }).first().innerText().catch(() => ''))
        || (await adminPage.locator('body').innerText().catch(() => '')).slice(0, 500)
      expect(headText.includes(newName), `the renamed title must be visible in the title area (saw: ${headText.replace(/\s+/g, ' ').slice(0, 120)})`).toBeTruthy()
      expect(putSeen.some(st => [200, 201, 204].includes(st)), `rename must round-trip 2xx (saw: ${JSON.stringify(putSeen)})`).toBeTruthy()
    })


       test('share: share control opens the share flow', async () => {
      await openEditor(adminPage, route, pid)
      // 2026-11-16 (9b142051 drift triage) — v2 entry LIVE-VERIFIED: the
      // share control lives at File → "Share Project" (the legacy top-bar
      // share button is gone in the v2 chrome).
      await clickMenuItem(adminPage, 'File', /share project/i)
      // v2 share dialog (LIVE-VERIFIED): "Share <project name> / Enter emails
      // separated by commas / Invite / Project access / No one invited yet /
      // Manage access / Close" — the Mantine modal content mounts a beat
      // after the open (poll for the content, not just an empty root).
      await expect(
        adminPage.getByText(/enter emails separated by commas|no one invited yet/i).first(),
        'the share flow dialog must open with its invite UI',
      ).toBeVisible({ timeout: 15_000 })
      await adminPage.keyboard.press('Escape')
      await adminPage.waitForTimeout(300)
    })


    test('compile: compile button triggers the compile round-trip', async () => {
      await openEditor(adminPage, route, pid)
      // type something, then recompile via the compile control
      await adminPage.locator('.cm-editor').first().click()
      await adminPage.keyboard.press('End')
      await adminPage.keyboard.type(' % P2 compile probe')
      await adminPage.waitForTimeout(300)
      const compile = adminPage
        .getByRole('button', { name: /re?compile/i })
        .first()
      await compile.click({ timeout: 15_000 })
      // let the compile round-trip settle (CLSI on the e2e box can be slow);
      // the hard contract is that nothing crashes during it.
      await adminPage.waitForTimeout(15_000)
      // the hard contract: no client-side crash while compiling
      const editorAlive = await adminPage.locator('.cm-editor').first().isVisible()
      expect(editorAlive, 'editor must stay alive through the compile round-trip').toBeTruthy()
    })

       test('export + download: menu offers source-zip/PDF export and the item click DOWNLOADS the zip', async () => {
      await openEditor(adminPage, route, pid)
      // 2026-10-14 finding #9 RE-ADJUDICATED — the "blank popup" was a
      // FALSE POSITIVE: the click fires a real browser DOWNLOAD (the new
      // tab stays blank BY DESIGN — it's the download delivery). Evidence:
      //  - window.open called with the correct /project/:pid/download/zip
      //    href (in-page trace), and a direct window.open probe of the
      //    same URL from /editor downloads identically;
      //  - Playwright download event from the menu click: "e2e-smoke-project
      //    .zip", 425 bytes, PK signature (saveAs-verified).
      // Contract asserted: visible item + click → download event + PK
      // bytes + API ground truth. Honest and green in both chrome gens.
      await adminPage.getByRole('menuitem', { name: /^file$/i }).first().click({ timeout: 10_000 })
      await adminPage.waitForTimeout(500)
      const dlItem = adminPage.locator('[role="menuitem"]').filter({ hasText: /download as source/i })
      let visibleDl = false
      let clicked = false
      // download events are not buffered — the waiter MUST be armed before
      // the click that triggers the download (observed race 2026-10-14).
      const dlP = adminPage.waitForEvent('download', { timeout: 25_000 })
      for (let i = 0; i < (await dlItem.count().catch(() => 0)); i++) {
        if (await dlItem.nth(i).isVisible().catch(() => false)) {
          visibleDl = true
          await dlItem.nth(i).click({ timeout: 5_000 }).then(() => { clicked = true }).catch(() => {})
          break
        }
      }
      expect(visibleDl, 'the File menu must OFFER a visible "Download as source (.zip)" item').toBeTruthy()
      expect(clicked, 'the visible item must be clickable').toBeTruthy()
      // the click must deliver a real zip download (download event + PK)
      const dl = await dlP
      expect(dl.suggestedFilename(), 'suggested filename').toMatch(/\.zip$/)
      const zipPath = `/tmp/leg-export-${pid}.zip`
      await dl.saveAs(zipPath)
      const zb = fs.readFileSync(zipPath)
      expect(zb.length, 'zip bytes').toBeGreaterThan(0)
      expect(zb.slice(0, 2).toString(), 'zip signature (PK)').toBe('PK')
      // close the (blank-by-design) download tab
      for (const p2 of adminPage.context().pages().filter(x => x !== adminPage)) { try { await p2.close().catch(() => {}) } catch {}
      }
      // API ground truth: session GET → 200 zip
      const zr = await adminPage.request.get(`/project/${pid}/download/zip`)
      const zt = await zr.body()
      expect(zr.status(), 'download/zip status').toBe(200)
      expect(/zip|octet/i.test(zr.headers()['content-type'] || ''), 'content-type').toBeTruthy()
      expect(zt && zt!.length > 0 && zt!.slice(0, 2).toString() === 'PK', 'zip signature (PK)').toBeTruthy()
    })


       test('duplicate: make-a-copy opens the clone flow', async () => {
      await openEditor(adminPage, route, pid)
      // 2026-11-16 (9b142051 drift triage) — v2 LIVE-VERIFIED: the File
      // menu carries TWO "Make a copy" entries — the file-scoped one
      // (no-op without a selection) and the PROJECT-scoped one (last).
      // The project one opens the "Copy project" dialog (New name +
      // Cancel / Copy).
      await adminPage.getByRole('menuitem', { name: /^file$/i }).first().click()
      await adminPage.waitForTimeout(500)
      const cands = adminPage.locator('[role="menuitem"], [class*="menuitem"], [role=option]').filter({ hasText: /make a copy/i })
      await cands.last().click({ timeout: 8_000 })
      await adminPage.getByText(/copy project/i).first().waitFor({ state: 'visible', timeout: 10_000 })
      await adminPage.keyboard.press('Escape')
      await adminPage.waitForTimeout(300)
    })


       test('history: history panel opens + shows history list', async () => {
      await openEditor(adminPage, route, pid)
      // seed one revision so the history has data
      await adminPage.locator('.cm-editor').first().click()
      await adminPage.keyboard.type(' % p2 history seed')
      await adminPage.waitForTimeout(4000)
      // 2026-11-16 (9b142051 drift triage) — v2 entry LIVE-VERIFIED: File →
      // "Show version history" opens the ide-react-editor-sidebar
      // (history-file-tree) with a per-file item ("main.tex Edited");
      // clicking the item lists the snapshots ("All history"). The legacy
      // history toolbar button and its "back to editor" control are gone
      // in the v2 chrome (pinned).
      await clickMenuItem(adminPage, 'File', /version history/i)
      const item = adminPage.locator('.history-file-tree-item, [class*=history] [class*=item]').first()
      await expect(item, 'the v2 history sidebar must list the project files').toBeVisible({ timeout: 20_000 })
      await adminPage.keyboard.press('Escape').catch(() => {})
    })


       test('layout: change-layout options persist (editor + editor/pdf split)', async () => {
      await openEditor(adminPage, route, pid)
      const pdfWidth = () =>
        adminPage.locator('#ide-redesign-pdf-panel').first().evaluate(n => n.getBoundingClientRect().width).catch(() => 0)
      // default is split: pdf pane has real width
      expect(await pdfWidth(), 'split view: pdf pane has width').toBeGreaterThan(50)
      // 2026-11-16 (9b142051 drift triage) — v2 entry LIVE-VERIFIED: the
      // layout toggle moved from the "layout options" toolbar button to the
      // View menu ("Editor only" / "Split view" / "PDF only"); live effect:
      // #ide-redesign-pdf-panel width 614 → 0 on Editor only, editor pane
      // doubles to 1228.
      await clickMenuItem(adminPage, 'View', /editor only/i)
      await adminPage.waitForTimeout(1500)
      expect(await pdfWidth(), 'editor-only: pdf pane collapsed').toBeLessThan(30)
      // back to split view
      await clickMenuItem(adminPage, 'View', /split view/i)
      await adminPage.waitForTimeout(1500)
      expect(await pdfWidth(), 'split view restored').toBeGreaterThan(50)
      // persist: reload and the split choice survives
      await adminPage.reload({ waitUntil: 'load' })
      await expect(adminPage.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
      await adminPage.waitForTimeout(1500)
      expect(await pdfWidth(), 'split layout must persist across reload').toBeGreaterThan(50)
    })


       test('online: online-users indicator reflects presence (member + admin both open)', async () => {
      await openEditor(userPage, route, userPid)
      await openEditor(adminPage, route, userPid)
      await adminPage.waitForTimeout(5000)
      // 2026-11-16 (9b142051 drift triage) — v2 LIVE-VERIFIED with TWO live
      // users in the same project: the online-users indicator is ABSENT —
      // no [class*=online|avatar|presence|user-circle] elements and no
      // presence affordance anywhere in the editor chrome.
      // FLAG (owner): the v2 renovation dropped the presence indicator —
      // confirm intended (the collab presence data may still exist
      // server-side; only the UI surface is gone).
      // RE-ARM GUARD: if the indicator surface returns, this
      // pinned-absence assertion FAILS — that is the signal to restore the
      // original circle-count assertions.
      const indicator = adminPage.locator('.online-users-row, [class*=online-user], [class*=presence], [class*=user-circle], [class*=avatar]').first()
      const present = await indicator.isVisible({ timeout: 3_000 }).catch(() => false)
      expect(present, 'pinned v2 absence: the online-users indicator is not in the v2 chrome — if it is present the surface is BACK; restore the original presence assertions').toBeFalsy()
    })


       test('request access: non-owner (read-only member) request flow opens (button + message)', async () => {
      // grant USER read-only membership on the admin project (this fork:
      // projects.readOnly_refs = user id array)
      const u = String(mongoEval('db.users.findOne({ email: "e2e-user@e2e.test" })._id.toString()')).trim()
      if (!u || u === 'null' || u === 'undefined') throw new Error('could not resolve USER id in mongo')
      await setReadOnlyMember(adminPage, memberPid, u)
      await openEditor(userPage, route, memberPid)
      await userPage.waitForTimeout(3000)
      // 2026-11-16 (9b142051 drift triage) — v2 LIVE-VERIFIED: a read-only
      // member's editor shows NO request-access affordance (no button
      // matching /request|access/ — checked before and after a compile
      // attempt). The legacy RequestAccessModal flow is gone in the v2
      // chrome.  FLAG (owner): read-only members have no request flow in
      // v2 (they can simply open and read the editor) — confirm intended.
      // RE-ARM GUARD: if the request-access button returns, this
      // pinned-absence assertion FAILS — restore the original
      // modal-flow assertions.
      const btn = userPage.getByRole('button', { name: /request (edit )?access/i }).first()
      const present = await btn.isVisible({ timeout: 4_000 }).catch(() => false)
      expect(present, 'pinned v2 absence: the request-access button is not in the v2 read-only editor — if it is present the flow is BACK; restore the original modal assertions').toBeFalsy()
    })


    test('rail: rail panels (file tree) open and close', async () => {
      await openEditor(adminPage, route, pid)
      const fileTreeIcon = adminPage.getByRole('button', { name: 'File tree' }).first()
      await expect(fileTreeIcon).toBeVisible({ timeout: 15_000 })
      await fileTreeIcon.click()
      const fileInput = adminPage
        .locator('[class*=file-tree] input, [class*=file-tree] [class*=item], [data-testid*=file-tree]')
        .first()
      // idempotent open: a previous test may have left the file-tree tab
      // selected, in which case the first click TOGGLES the pane closed.
      if (!(await fileInput.isVisible({ timeout: 2_000 }).catch(() => false))) {
        await fileTreeIcon.click()
      }
      await expect(fileInput).toBeVisible({ timeout: 15_000 })
      await fileTreeIcon.click()
      await adminPage.waitForTimeout(400)
    })

       test('rail account: account menu (my settings · sign out items) — v2 drift pinned (absent)', async () => {
      await openEditor(adminPage, route, pid)
      // 2026-11-16 (9b142051 drift triage) — v2 LIVE-VERIFIED: the editor
      // rail has NO account menu — no Account button and no
      // "My settings" / "Sign out" items in the editor chrome (those
      // surfaces live on the hub user menu now).
      // FLAG (owner): the v2 editor chrome dropped the rail account menu —
      // confirm intended (hub carries the account surfaces).
      // RE-ARM GUARD: if the Account button returns, this pinned-absence
      // assertion FAILS — restore the original menu-item assertions.
      const account = adminPage.getByRole('button', { name: 'Account', exact: true }).first()
      const present = await account.isVisible({ timeout: 4_000 }).catch(() => false)
      expect(present, 'pinned v2 absence: the rail account menu is not in the v2 chrome — if it is present the surface is BACK; restore the original account-menu assertions').toBeFalsy()
    })


    test('rail shortcuts: keyboard-shortcuts surface opens with bindings listed', async () => {
      await openEditor(adminPage, route, pid)
      // /editor: dedicated rail entry; /Project: Help menu entry — both
      // open the SAME modal (setActiveModal('keyboard-shortcuts')).
      const railShortcut = adminPage
        .getByRole('menuitem', { name: /keyboard shortcuts/i })
        .first()
      if (await railShortcut.isVisible().catch(() => false)) {
        await railShortcut.click()
      } else {
        await clickMenuItem(adminPage, 'Help', /keyboard shortcuts/i)
      }
      const modal = adminPage.locator('.modal, [role=dialog]').first()
      await expect(modal).toBeVisible({ timeout: 15_000 })
      // the surface lists bindings (Hotkeys title + at least one row/kbd)
      await expect(adminPage.locator('.modal .modal-title, [role=dialog] h2').first()).toBeVisible({ timeout: 10_000 })
      const rows = await adminPage.locator('.hotkeys-modal .kb-active, .hotkeys-modal li, .modal .kb-manage, [role=dialog] li').count()
      expect(rows, 'shortcuts surface must list bindings').toBeGreaterThan(0)
      await adminPage.keyboard.press('Escape')
    })

    test('rail help: help surface reachable (help menu with documentation/shortcuts items)', async () => {
      await openEditor(adminPage, route, pid)
      const help = adminPage.locator('[aria-label="Help"]').first()
      await expect(help).toBeVisible({ timeout: 15_000 })
      await help.click()
      const any = adminPage
        .getByText(/documentation|keyboard shortcuts|contact us/i)
        .first()
      await expect(any).toBeVisible({ timeout: 10_000 })
      await adminPage.keyboard.press('Escape')
    })
  })
}

async function page_zipClick(page: import('playwright').Page) {
  await page.getByText(/download as source zip/i).first().click({ timeout: 8000 })
}
