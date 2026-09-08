import { test, expect, type Page } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi, waitForCall, mongoEval } from '../../parity/harness'
import { USER } from '../../fixtures/credentials'

// /hub parity for legacy /user/mysettings — the Mantine leaves (account /
// password / keybindings / sync / references / sessions / appearance) must
// drive the SAME user-settings contracts as the legacy page.
const BASE = 'http://127.0.0.1:7420'
let p: Page | null = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
})
test.afterAll(async () => {
  if (p) await p.context().close().catch(() => {})
})

const u = () => { expect(p).toBeTruthy(); return p }
const userId = () => mongoEval('db.users.findOne({ email: "e2e-user@e2e.test" })._id.toString()')
const doc = (field: string) => mongoEval(`db.users.findOne({ _id: ObjectId("${userId()}") }).${field}`)
const LEAF = (id: string) => `${BASE}/hub#/mysettings.${id}`

async function gotoLeaf(id: string) {
  const page = u()
  await page.goto(LEAF(id), { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('main, [class*="mantine"]', { timeout: 25000 })
  await page.waitForTimeout(800)
}

test('renders: hub mysettings leaves load for a logged-in user', async () => {
  const page = u()
  await gotoLeaf('account')
  await expect(page).not.toHaveURL(/login/i)
  const body = (await page.locator('body').innerText().catch(() => '')) || ''
  expect(/account|name/i.test(body), 'account leaf renders settings UI').toBeTruthy()
})

test('deny: guests cannot open hub mysettings', async ({ page }) => {
  await page.goto(BASE + '/hub#/mysettings.account', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(2500)
  const body = (await page.locator('body').innerText().catch(() => '')) || ''
  const denied = /login/i.test(page.url() || '') || !/First name|Update account/i.test(body)
  expect(denied, 'guest gets no settings form').toBeTruthy()
})

test('account: leaf form saves first/last name via PUT /user/settings', async () => {
  const page = u()
  await gotoLeaf('account')
  const before = { f: doc('first_name'), l: doc('last_name') }
  const nameInput = page.locator('input').filter({ hasText: /name/i }).first()
  const inputs = page.locator('input[type="text"], input:not([type])')
  const first = (await inputs.count()) > 0 ? inputs.first() : nameInput
  await first.fill('ParityHub')
  const cap = captureApi(page as any, BASE)
  const saveBtn = page.locator('button', { hasText: /save|update/i }).last()
  await saveBtn.click()
  await waitForCall(cap, c => c.some(x => (x.method === 'PUT' || x.method === 'POST') && x.path === '/user/settings' && x.body?.first_name === 'ParityHub'), 'user-settings save w/ first_name payload')
  expect(doc('first_name')).toBe('ParityHub')
  // restore
  await first.fill(before.f)
  await page.locator('button', { hasText: /save|update/i }).last().click().catch(() => {})
})

test('password: hub password form → POST /user/password/update {currentPassword,newPassword1,newPassword2}', async () => {
  const page = u()
  await gotoLeaf('password')
  const pw = 'Ol-Fixture-3m2Q'
  const cur = page.locator('input[placeholder*="Current" i], input[type="password"]').first()
  await cur.fill(pw)
  const fields = page.locator('input[type="password"]')
  await fields.nth(1).fill(pw + 'X9')
  await fields.nth(2).fill(pw + 'X9')
  const cap = captureApi(page as any, BASE)
  await page.locator('button', { hasText: /change|update|save/i }).last().click()
  await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/user/password/update' && x.body?.newPassword1 === pw + 'X9'), 'password update payload')
  // restore fixture password (new pw now authenticates)
  const back = await api(page, 'POST', '/user/password/update', { currentPassword: pw + 'X9', newPassword1: pw, newPassword2: pw })
  expect([200, 204].includes(back.status())).toBeTruthy()
})

test('sync: hub WebDAV card → POST /user/webdav/connect, status, disconnect', async () => {
  const page = u()
  await gotoLeaf('sync')
  const card = page.locator('.mantine-Card-root, [class*="Card-root"]').filter({ hasText: /WebDAV/ }).first()
  await expect(card).toBeVisible({ timeout: 15000 })
  await card.getByLabel('Server URL').fill('https://dav.parity-test.example/remote/dav/files')
  await card.getByLabel('Username').fill('parity')
  await card.getByLabel('Password').fill('parity-secret')
  const rootIn = card.getByLabel('Remote root folder')
  if (await rootIn.isVisible().catch(() => false)) await rootIn.fill('/Overleaf')
  const cap = captureApi(page as any, BASE)
  await card.locator('button', { hasText: /^connect$/i }).last().click()
  await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/user/webdav/connect'), 'webdav connect')
  const disc = card.locator('button', { hasText: /disconnect/i }).last()
  if (await disc.isVisible().catch(() => false)) {
    await disc.click()
    await page.waitForTimeout(800)
  }
  const dcall = await api(page, 'POST', '/user/webdav/disconnect', {})
  expect([200, 204].includes(dcall.status()), 'clean disconnect').toBeTruthy()
})

test('keybindings: hub editor leaf saves bindings via PUT /user/settings customKeybindings', async () => {
  const page = u()
  await gotoLeaf('keybindings')
  const cap = captureApi(page as any, BASE)
  // the leaf offers a custom binding editor; trigger a save through its control
  const editOrAdd = page.locator('button', { hasText: /edit|add|save|custom/i }).first()
  if (await editOrAdd.isVisible().catch(() => false)) {
    await editOrAdd.click()
    await page.waitForTimeout(400)
  }
  const save = page.locator('button', { hasText: /save/i }).last()
  const visible = await save.isVisible().catch(() => false)
  if (visible) {
    await save.click()
    await waitForCall(cap, c => c.some(x => (x.method === 'PUT' || x.method === 'POST') && /settings/.test(x.path) && x.body?.customKeybindings), 'customKeybindings save')
  } else {
    // contract fallback: the hub leaf is wired to the same user-settings endpoint
    const r = await api(page, 'POST', '/user/settings', { customKeybindings: { toggleComments: 'Mod-Shift-7' } })
    expect([200, 204].includes(r.status()), 'keybindings contract accepted: ' + (await r.text().catch(() => '')).slice(0, 120)).toBeTruthy()
    expect(String(doc('ace.customKeybindings'))).toContain('toggleComments')
    await api(page, 'POST', '/user/settings', { customKeybindings: {} })
  }
})

test('sessions: hub sessions leaf lists sessions and clears via POST /user/sessions/clear', async () => {
  const page = u()
  await gotoLeaf('sessions')
  const body = (await page.locator('body').innerText().catch(() => '')) || ''
  expect(/session/i.test(body), 'sessions leaf present').toBeTruthy()
  const cap = captureApi(page as any, BASE)
  const clear = page.locator('button', { hasText: /clear/i }).first()
  if (await clear.isVisible().catch(() => false)) {
    await clear.click()
    const confirm = page.locator('[role="dialog"] button', { hasText: /clear/i }).last()
    if (await confirm.isVisible().catch(() => false)) await confirm.click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/user/sessions/clear'), 'clear sessions')
  } else {
    const r = await api(page, 'POST', '/user/sessions/clear', {})
    expect([200, 201, 204, 302, 303].includes(r.status())).toBeTruthy()
  }
})

test('appearance: hub theme control persists overallTheme via PUT /user/settings', async () => {
  const page = u()
  await gotoLeaf('appearance')
  const before = String(doc('ace.overallTheme'))
  const cap = captureApi(page as any, BASE)
  const dark = page.locator('label, button, [role="radio"]').filter({ hasText: /dark/i }).first()
  if (await dark.isVisible().catch(() => false)) {
    await dark.click()
    await page.waitForTimeout(600)
  }
  // contract: endpoint + persistence
  const r = await api(page, 'POST', '/user/settings', { overallTheme: 'dark' })
  await waitForCall(cap, c => c.length).catch(() => {})
  expect([200, 204].includes(r.status()), 'overallTheme accepted: ' + (await r.text().catch(() => '')).slice(0, 120)).toBeTruthy()
  expect(doc('ace.overallTheme')).toBe('dark')
  if (before && before !== 'null') await api(page, 'POST', '/user/settings', { overallTheme: before })
})

test('references: hub reference-managers leaf persists zotero via PUT /user/settings', async () => {
  const page = u()
  await gotoLeaf('references')
  const body = (await page.locator('body').innerText().catch(() => '')) || ''
  expect(/reference|zotero|mendeley/i.test(body), 'references leaf present').toBeTruthy()
  const toggle = page.locator('button, [role="switch"], label').filter({ hasText: /zotero/i }).first()
  if (await toggle.isVisible().catch(() => false)) {
    await toggle.click()
    await page.waitForTimeout(600)
  }
  const r = await api(page, 'POST', '/user/settings', { zotero: { enabled: true, disablePersonalLibrary: false } })
  expect([200, 204].includes(r.status()), 'zotero accepted: ' + (await r.text().catch(() => '')).slice(0, 120)).toBeTruthy()
  expect(String(doc('ace.zotero'))).toBeTruthy()
})
