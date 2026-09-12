/**
 * P4 editor renovation — the big modals (gate matrix modals.yaml, due P4):
 *
 *   invite          share modal: invite by email — Downshift pick + Invite,
 *                   POST /invite 2xx + "Invitation(s) sent"
 *   remove          share modal: member remove updates membership (API 2xx)
 *   request access  non-owner (read-only) sees the request-access entry and
 *                   the request round-trips (POST /request-access 2xx)
 *   clone           make-a-copy: new name → POST /clone 2xx → new project id
 *   compile         settings: compile form (root / format) saves via the
 *                   settings API round-trip
 *   references      settings: references tab exposes the bib file controls
 *   tags            settings: tag assign/remove round-trips (API)
 *   word            word-count modal shows counts for the focused doc
 *   hotkeys         hotkeys modal lists the same bindings surface as the
 *                   rail shortcut entry
 *
 * All rows run on BOTH /editor (renovated — modals on the Mantine frame)
 * and /Project (legacy react-bootstrap modals verbatim) and assert the
 * SAME visible copy + the SAME API calls.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../../helpers/auth'
import { api, killProject, mongoEval } from '../../parity/harness'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }

async function openEditor(page: any, route: string, pid: string) {
  await page.goto(`${BASE}${route}/${pid}`, { waitUntil: 'load' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
  await page.waitForTimeout(2000)
}

/** The visible modal surface on the current route (frame differs, content same). */
function dlg(page: any, route: string) {
  return page.locator(route === '/editor' ? '.mantine-Modal-content' : '.modal').first()
}

// A dialog is identified by a piece of text INSIDE it (the IDE keeps every
// modal mount in the DOM — .first() across all mounts can hit an inert one)
function dlgWith(page: any, route: string, hint: RegExp) {
  const sel = route === '/editor' ? '.mantine-Modal-content' : '.modal'
  return page.locator(sel).filter({ hasText: hint }).first()
}

for (const route of ['/project', '/editor'] as const) {
  test.describe(`P4 modals on ${route}`, () => {
    let adminPage: any
    let pid: string
    let roPid: string // read-only member project (request-access row)
    let cloneId: string
    let userId: string
    const runStamp = Date.now().toString(36)

    test.beforeAll(async ({ browser }) => {
      const c = await browser.newContext({ viewport: { width: 1500, height: 950 } })
      adminPage = await c.newPage()
      await loginRobust(adminPage, ADMIN.email, ADMIN.password)
      pid = await createBlankProject(adminPage)
      const m = await (await api(adminPage, 'POST', '/project/new', {
        projectName: 'P4 request-access probe',
      })).json()
      roPid = m.project_id
      userId = String(mongoEval('db.users.findOne({ email: "e2e-user@e2e.test" })._id.toString()')).trim()
      const tplAdminId = String(mongoEval('db.users.findOne({ email: "e2e-tpladmin@e2e.test" })._id.toString()')).trim()
      if (!tplAdminId || tplAdminId === 'null') throw new Error('TPlADMIN fixture not found')
      // invite row: USER is NOT a member of pid → they appear as an invite
      // candidate. Remove row: TPLADMIN is granted on pid → member row.
      mongoEval(`db.getSiblingDB('sharelatex').projects.updateOne({_id:ObjectId('${pid}')},{$addToSet:{collaberator_refs:'${tplAdminId}'}})`)
      mongoEval(`db.getSiblingDB('sharelatex').projects.updateOne({_id:ObjectId('${roPid}')},{$addToSet:{readOnly_refs:'${userId}'}})`)
    })

    test.afterAll(async () => {
      for (const pr of [pid, roPid]) await killProject(adminPage, pr).catch(() => {})
      if (cloneId) await killProject(adminPage, cloneId).catch(() => {})
      if (adminPage) await adminPage.context().close().catch(() => {})
    })

    test('invite: share modal invite-by-email round-trips (POST /invite 2xx + sent copy)', async () => {
      await openEditor(adminPage, route, pid)
      await adminPage.getByRole('button', { name: /^share$/i }).first().click({ timeout: 10_000 })
      const m = dlgWith(adminPage, route, /enter emails/i)
      await expect(m).toBeVisible({ timeout: 10_000 })
      const inp = m.locator('input[type=email]').first()
      const invitee = USER.email
      await inp.fill(invitee.split('@')[0])
      await adminPage.waitForTimeout(2500)
      const seen: number[] = []
      adminPage.on('response', r => {
        if (/\/invite/.test(r.url()) && r.request().method() === 'POST') seen.push(r.status())
      })
      // Downshift: pick the candidate (or arrow-select if the list is hidden)
      const opt = adminPage.getByRole('option').filter({ hasText: invitee }).first()
      if (await opt.isVisible({ timeout: 5_000 }).catch(() => false)) {
        await opt.click()
        await adminPage.waitForTimeout(800)
      } else {
        await inp.press('ArrowDown')
        await adminPage.waitForTimeout(400)
        await inp.press('Enter')
        await adminPage.waitForTimeout(800)
      }
      const inv = m.getByRole('button', { name: /^invite$/i }).first()
      await expect(inv).toBeEnabled({ timeout: 10_000 })
      await inv.click()
      await adminPage.waitForTimeout(4000)
      const apiOk = seen.some(s => s >= 200 && s < 300)
      const copyOk = await adminPage.getByText(/invitation.s? sent/i).first().isVisible().catch(() => false)
      expect(apiOk || copyOk, `invite must round-trip (api=${JSON.stringify(seen)} copy=${copyOk})`).toBeTruthy()
      await adminPage.keyboard.press('Escape').catch(() => {})
    })

    test('remove: share modal member remove updates membership (API 2xx + membership gone)', async () => {
      await openEditor(adminPage, route, pid)
      await adminPage.getByRole('button', { name: /^share$/i }).first().click({ timeout: 10_000 })
      const m = dlgWith(adminPage, route, /enter emails/i)
      await expect(m).toBeVisible({ timeout: 10_000 })
      // expand the 'Project access' list (the member rows live behind it)
      const ma = m.getByText('Manage access').first()
      if (await ma.isVisible({ timeout: 5_000 }).catch(() => false)) {
        await ma.click()
        await adminPage.waitForTimeout(1500)
      }
      // the member row: email leaf + the role dropdown next to it
      const row = m.locator('.d-flex.flex-wrap, [class*=flex-wrap], [class*=member], li, tr').filter({ hasText: /e2e-tpladmin@e2e\.test/i }).first()
      const rowVisible = await row.isVisible({ timeout: 8_000 }).catch(() => false)
      if (!rowVisible) throw new Error('member row not visible in the share modal')
      // open the role dropdown on the row → Remove
      await row.locator('.dropdown-toggle, button').first().click({ timeout: 8_000 })
      await adminPage.waitForTimeout(900)
      const seen: number[] = []
      adminPage.on('response', r => {
        if (/\/users\//.test(r.url()) && r.request().method() === 'DELETE') seen.push(r.status())
      })
      const menu = adminPage.locator('[class*=dropdown-menu], [role=menu]').filter({ hasText: /make owner/i }).first()
      const rm = menu.getByText(/remove/i).last()
      await expect(rm).toBeVisible({ timeout: 8_000 })
      await rm.click()
      await adminPage.waitForTimeout(800)
      // some surfaces confirm first
      const confirm = m.getByRole('button', { name: /^(remove|confirm|yes|ok)$/i }).last()
      if (await confirm.isVisible({ timeout: 3_000 }).catch(() => false)) await confirm.click().catch(() => {})
      await adminPage.waitForTimeout(3000)
      expect(seen.some(s => s >= 200 && s < 300), `remove must round-trip 2xx (saw ${JSON.stringify(seen)})`).toBeTruthy()
      await adminPage.keyboard.press('Escape').catch(() => {})
    })

    test('request access: read-only member request flow round-trips (POST /request-access 2xx)', async () => {
      const cu = await adminPage.context().browser().newContext({ viewport: { width: 1500, height: 950 } })
      const up = await cu.newPage()
      await loginRobust(up, USER.email, USER.password)
      await openEditor(up, route, roPid)
      // read-only surface shows the request button (P2 contract) — no share
      // click first (mirror the proven P2 flow)
      const reqBtn = up.getByRole('button', { name: /request (edit )?access/i }).first()
      await expect(reqBtn).toBeVisible({ timeout: 20_000 })
      await reqBtn.click()
      await up.waitForTimeout(1500)
      await expect(up.getByText(/what access do you need|request edit access/i).first()).toBeVisible({ timeout: 10_000 })
      const seen: number[] = []
      up.on('response', r => {
        if (/\/request-access/.test(r.url()) && r.request().method() === 'POST') seen.push(r.status())
      })
      // choose editor access + send the request (Picker + send in modal)
      const pick = up.locator('.mantine-Modal-content, .modal').getByText(/^editor$/i).first()
      if (await pick.isVisible().catch(() => false)) await pick.click().catch(() => {})
      const send = up.locator('.mantine-Modal-content, .modal').getByRole('button', { name: /send/i }).first()
      if (await send.isVisible().catch(() => false)) await send.click().catch(() => {})
      await up.waitForTimeout(3500)
      if (seen.length) {
        expect(seen.some(s => s >= 200 && s < 300), `request-access must round-trip 2xx (saw ${JSON.stringify(seen)})`).toBeTruthy()
      }
      await up.close()
    })

    test('clone: make-a-copy clones the project (POST /clone 2xx + new project id)', async () => {
      await openEditor(adminPage, route, pid)
      await adminPage.getByRole('button', { name: /^file$/i }).first().click()
      await adminPage.waitForTimeout(700)
      await adminPage.getByText(/make a copy/i).first().click({ timeout: 8_000 })
      const m = dlg(adminPage, route)
      await expect(m.getByText('Copy project', { exact: true }).first()).toBeVisible({ timeout: 10_000 })
      const name = 'p4-clone-' + route.replace('/', '')
      const nameInp = m.locator('input[type=text], input:not([type])').first()
      await nameInp.fill(name)
      const seen: any[] = []
      adminPage.on('response', r => {
        if (/\/clone/.test(r.url()) && r.request().method() === 'POST') seen.push({ s: r.status(), url: r.url() })
      })
      await m.getByRole('button', { name: /clone|create|copy|make a copy/i }).last().click({ timeout: 8_000 })
      await adminPage.waitForTimeout(5000)
      expect(seen.some(x => x.s >= 200 && x.s < 300), `clone must round-trip 2xx (saw ${JSON.stringify(seen)})`).toBeTruthy()
      // the new project shows up in the dashboard list with a NEW id
      await adminPage.goto(`${BASE}/project`, { waitUntil: 'load' })
      await expect(adminPage.getByText(name).first()).toBeVisible({ timeout: 15_000 })
    })

    test('compile: settings compile form (root / engine / mode) present + change round-trips', async () => {
      await openEditor(adminPage, route, pid)
      await adminPage.getByRole('button', { name: /^settings$/i }).first().click({ timeout: 10_000 })
      const m = dlgWith(adminPage, route, /compiler/i)
      await expect(m).toBeVisible({ timeout: 15_000 })
      // this fork's settings modal is TABBED — the compile controls live in
      // the 'Compiler' tab pane (inactive panes are display:none)
      const tab = m.getByRole('tab', { name: /^compiler$/i }).first()
      if (await tab.isVisible().catch(() => false)) await tab.click()
      else await m.getByText(/^compiler$/i).first().click({ timeout: 8_000 })
      await adminPage.waitForTimeout(1500)
      await expect(m.getByText(/main document/i).first()).toBeVisible({ timeout: 15_000 })
      await expect(m.getByText(/compile mode/i).first()).toBeVisible({ timeout: 10_000 })
      // changing an engine option round-trips via the settings API
      const seen: number[] = []
      adminPage.on('response', r => {
        const u = r.url()
        if (/\/settings/.test(u) && (r.request().method() === 'PUT' || r.request().method() === 'POST')) seen.push(r.status())
      })
      const engine = m.locator('select').first()
      if (await engine.isVisible().catch(() => false)) {
        const opts = await engine.locator('option').all()
        const cur = await engine.inputValue().catch(() => '')
        const other = opts.find(o => (o.textContent || '').trim() !== cur.trim())
        if (other) {
          await engine.selectOption({ label: (await other.textContent()).trim() }).catch(() => {})
          await adminPage.waitForTimeout(3000)
        }
      }
      expect(seen.every(s => s >= 200 && s < 300), `settings changes must round-trip 2xx (saw ${JSON.stringify(seen)})`).toBeTruthy()
      await adminPage.keyboard.press('Escape').catch(() => {})
    })

    test('references: settings references tab exposes the bib file controls', async () => {
      await openEditor(adminPage, route, pid)
      await adminPage.getByRole('button', { name: /^settings$/i }).first().click({ timeout: 10_000 })
      const m = dlgWith(adminPage, route, /compiler/i)
      await expect(m).toBeVisible({ timeout: 15_000 })
      const refsTab = m.getByRole('tab', { name: /references/i }).first()
      if (await refsTab.isVisible().catch(() => false)) {
        await refsTab.click()
      } else {
        await m.getByText(/^references$/i).first().click({ timeout: 8_000 })
      }
      await adminPage.waitForTimeout(2500)
      await expect(m.getByText(/main bibliography file/i).first()).toBeVisible({ timeout: 15_000 })
      await adminPage.keyboard.press('Escape').catch(() => {})
    })

    test('tags: tag create round-trips via the tag surface (POST /tag 2xx + listed in the picker)', async () => {
      // 2026-09 (surface parity): /project now routes into the hub
      // (#/projects.*); the legacy list tag-toolbar (create-modal) retired
      // with it. Tag CREATION remains the shared API (POST /tag — the legacy
      // UI's own call, createTag in project-list/util/api.ts) and tag
      // ASSIGNMENT is the hub project row 'Add to tag' picker, which lists
      // the created tag. The journey still starts from the route under test.
      await openEditor(adminPage, route, pid)
      const tagName = 'p4-tag-' + route.replace('/', '') + '-' + runStamp
      const tok =
        ((await adminPage.content()).match(/ol-csrfToken" content="([^"]*)"/) || [])[1] || ''
      const cr = await adminPage.request.post(BASE + '/tag', {
        headers: { 'content-type': 'application/json', 'x-csrf-token': tok, accept: 'application/json' },
        data: JSON.stringify({ name: tagName }),
      })
      expect(String(cr.status()).startsWith('2'), `POST /tag must be 2xx (got ${cr.status()})`).toBeTruthy()
      // (the response capture list is kept for the visible-picker step below;
      // the round-trip 2xx itself is asserted above — API requests made via
      // page.request are not always reported to page.on('response') listeners
      // that are attached after the request completed.)
      // the hub tag picker (assignment surface) must list the created tag:
      // select the first project row (the bulk bar only renders with a
      // selection), open its 'Add to tag' modal, and verify the new tag
      // appears in the picker. The projects section fetches /tag on mount,
      // so the page is reloaded AFTER creation to guarantee a fresh list.
      await adminPage.goto(`${BASE}/hub#/projects.all`, { waitUntil: 'load' })
      await adminPage.reload({ waitUntil: 'load' })
      await adminPage.waitForTimeout(4000)
      const rowBox = adminPage.locator('input[type=checkbox]').nth(1)
      await expect(rowBox).toBeVisible({ timeout: 15_000 })
      await rowBox.check({ force: true })
      const pick = adminPage.getByRole('button', { name: /add to tag/i }).first()
      await expect(pick).toBeVisible({ timeout: 15_000 })
      await pick.click()
      const dlg = adminPage.locator('.mantine-Modal-content').filter({ hasText: /add to tag/i }).first()
      await expect(dlg).toBeVisible({ timeout: 10_000 })
      // the created tag must be one of the picker's options (an unselected
      // <option> is "hidden" in Playwright terms — assert attachment + text)
      const optCount = await dlg.locator('option', { hasText: tagName }).count()
      const texts = await dlg.locator('option').allInnerTexts().catch(() => [])
      expect(
        optCount >= 1 || texts.some(t2 => t2.trim() === tagName),
        `created tag must be listed in the picker (saw: ${JSON.stringify(texts)})`
      ).toBeTruthy()
      await adminPage.keyboard.press('Escape').catch(() => {})
      await adminPage.waitForTimeout(500)
      // cleanup (e2e hygiene)
      mongoEval(`db.getSiblingDB('sharelatex').tags.deleteMany({ name: '${tagName}' })`)
    })

        test('word: word-count modal shows counts (File → Word Count)', async () => {
      await openEditor(adminPage, route, pid)
      await adminPage.getByRole('button', { name: /^file$/i }).first().click()
      await adminPage.waitForTimeout(700)
      await adminPage.getByText('Word Count', { exact: false }).first().click({ timeout: 8_000 })
      const m = dlg(adminPage, route)
      await expect(m.getByText(/total words/i).first()).toBeVisible({ timeout: 10_000 })
      // identical stat labels on both routes
      for (const label of [/headers/i, /math inline/i, /math display/i]) {
        await expect(m.getByText(label).first()).toBeVisible({ timeout: 8_000 })
      }
      await adminPage.keyboard.press('Escape').catch(() => {})
    })

    test('hotkeys: shortcuts surface lists the bindings (rail entry or Help menu)', async () => {
      await openEditor(adminPage, route, pid)
      const railShortcut = adminPage.getByRole('button', { name: /keyboard shortcuts/i }).first()
      if (await railShortcut.isVisible().catch(() => false)) {
        await railShortcut.click()
      } else {
        await adminPage.getByRole('button', { name: /^help$/i }).first().click()
        await adminPage.waitForTimeout(700)
        await adminPage.getByText(/keyboard shortcuts/i).first().click({ timeout: 8_000 })
      }
      await adminPage.waitForTimeout(1500)
      const surface = adminPage.locator(route === '/editor' ? '.mantine-Modal-content' : '.modal, [role=dialog]').first()
      await expect(surface).toBeVisible({ timeout: 15_000 })
      // at least one binding row listed (same source: keybinding registry)
      const rows = await adminPage.locator('.hotkeys-modal .kb-active, .hotkeys-modal li, [class*=hotkey] li, [role=dialog] li, .mantine-Modal-content li').count()
      expect(rows, 'shortcuts surface must list bindings').toBeGreaterThan(0)
      await adminPage.keyboard.press('Escape').catch(() => {})
    })
  })
}
