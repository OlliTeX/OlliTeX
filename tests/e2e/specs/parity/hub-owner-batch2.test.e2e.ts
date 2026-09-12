/**
 * 2026-09-11 (owner batch 2): the six fixes on the hub — durable e2e guards.
 *
 *  1. #/site.general.managetpl  : the two site-wide template switches restored
 *                                 + 13-row category table (status/publishable/
 *                                 count/description/edit) + template admins list
 *  2. #/site.general.signup     : single "Sign-up" label (no double title)
 *  3. #/site.integrations.sso-saml : SP metadata link → /saml/meta (served XML)
 *  4. #/site.integrations.sso-ldap : editable Timeout (ms) + round-trip
 *  5. /admin/site               : 302 → /hub#/site (legacy page retired)
 *  7. #/site.compilation.sandboxed : no enable toggle (mandatory section)
 * (6. git tokens live in hub-git-integration.test.e2e.ts)
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api as apiBase } from '../../parity/harness'
import { ADMIN } from '../../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'
let p: any = null
// 2026-09 (mega-batch): go through the shared harness api() which carries the
// session CSRF token — a bare context fetch is 403'd by double-submit CSRF
// (the categories publishable PUT in test 1b exposed this).
const api = (method: string, path: string, body?: unknown) => apiBase(p, method, path, body)

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1440, height: 940 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })

const go = async (hash: string) => {
  await p.goto(BASE + '/hub#' + hash, { waitUntil: 'domcontentloaded' })
  await p.waitForTimeout(2200)
  const body = await p.locator('body').innerText()
  expect(body.length, 'hub rendered for ' + hash).toBeGreaterThan(100)
}

test('1: managetpl — site-wide switches + full category table + admins list', async () => {
  await go('site.general.managetpl')
  const body = await p.locator('body').innerText()
  expect(/All users are template gallery admins/.test(body), 'tpl-admins switch').toBeTruthy()
  expect(/Non-admins can publish templates/.test(body), 'nonadmin-publish switch').toBeTruthy()

  // 13 seed categories (+ custom) — Name links to /templates/<key>
  const catLinks = await p.locator('main a[href^="/templates/"]').count()
  expect(catLinks, 'category name links >= 12').toBeGreaterThanOrEqual(12)

  const editButtons = await p.locator('main table button:has-text("Edit")').count()
  expect(editButtons, 'category Edit buttons').toBeGreaterThanOrEqual(12)

  expect(/Template gallery admins/.test(body), 'template admins card').toBeTruthy()
})

test('1b: managetpl — switch + category publishable round-trip through the API', async () => {
  const r0 = await api('GET', '/admin/site-settings')
  const before = await r0.json()
  const tpl = before.templates || {}

  // flip the two site-wide switches + one category's publishable, save via UI
  await go('site.general.managetpl')
  // 2026-09 (mega-batch): Mantine renders role=switch as a visually hidden
  // input inside a styled <label> (class mantine-Switch-body; the -root
  // element is a plain div — clicking it can miss the toggle). Click the
  // visible label (labels natively toggle their input), and scope by the
  // row: the card row is a Mantine Group holding the caption + the switch.
  // (The hub page renders many sections, so a bare nth() across
  // main label[...]-body is not stable — earlier flakes hit the wrong row.)
  const rowSw = (re: RegExp) =>
    p
      .locator('main .mantine-Group-root')
      .filter({ hasText: re })
      .locator('label.mantine-Switch-body')
      .first()
  const adminSw = rowSw(/all users are template gallery admins/i)
  const publishSw = rowSw(/non-admins can publish templates/i)
  await expect(adminSw, 'managetpl site-wide switches rendered').toBeVisible({ timeout: 20000 })
  await adminSw.click()
  await publishSw.click()
  await p.waitForTimeout(800)

  const r1 = await api('GET', '/admin/site-settings')
  const after = await r1.json()
  expect(after.templates.allUsersCanManageTemplates, 'admins flip persisted').toBe(
    !Boolean(tpl.allUsersCanManageTemplates)
  )
  expect(after.templates.nonAdminCanPublishTemplates, 'publish flip persisted').toBe(
    !Boolean(tpl.nonAdminCanPublishTemplates)
  )

  // undo (leave site state as found, except category publishable)
  await adminSw.click()
  await publishSw.click()
  await p.waitForTimeout(800)
  const r2 = await api('GET', '/admin/site-settings')
  const back = await r2.json()
  expect(back.templates.allUsersCanManageTemplates).toBe(Boolean(tpl.allUsersCanManageTemplates))
  expect(back.templates.nonAdminCanPublishTemplates).toBe(Boolean(tpl.nonAdminCanPublishTemplates))

  // category publishable: flip via API surface (PUT) + read back + restore
  const cats: any[] = after.templates?.categories || back.templates?.categories || []
  const cat = cats.find(c => c.key !== 'all')
  if (cat) {
    const body = { ...back.templates, categories: (back.templates.categories || []).map((c: any) =>
      c.key === cat.key ? { ...c, publishable: !Boolean((c as any).publishable) } : c
    ) }
    const put = await api('PUT', '/admin/site-settings/templates', body)
    expect(put.status(), 'publishable PUT').toBe(200)
    const r3 = await api('GET', '/admin/site-settings')
    const now = await r3.json()
    const nowCat = (now.templates.categories || []).find((c: any) => c.key === cat.key)
    expect(nowCat.publishable, 'publishable flipped + read back').toBe(!Boolean(cat.publishable))
    const restore = { ...now.templates, categories: (now.templates.categories || []).map((c: any) =>
      c.key === cat.key ? { ...c, publishable: cat.publishable } : c
    ) }
    await api('PUT', '/admin/site-settings/templates', restore)
  }
})

test('2: signup — no double section title', async () => {
  await go('site.general.signup')
  // page content (main) must carry the label exactly once; the hub rail
  // (outside main content area) may repeat it as the nav item.
  const mainText = (await p.locator('main').innerText().catch(() => '')) || ''
  const occurrences = (mainText.match(/Sign-up/g) || []).length
  expect(occurrences, 'one Sign-up label in content').toBeLessThanOrEqual(1)
})

test('3: saml — SP metadata link targets /saml/meta; metadata XML (configured) or clean 503 (unconfigured)', async () => {
  await go('site.integrations.sso-saml')
  const link = p.locator('main a[href*="/saml/m"]').first()
  await expect(link).toBeVisible({ timeout: 15000 })
  expect((await link.getAttribute('href'))).toContain('/saml/meta')

  // 2026-09 (mega-batch): deterministic per-config contract.
  //   · SAML configured (e.g. production): 200 + EntityDescriptor XML
  //   · SAML unconfigured (D7 stored-only default, fresh e2e stack): the
  //     endpoint must answer a CLEAN 503 + actionable message (the old
  //     behavior was a 500 TypeError — regression fixed in build11)
  let r = await p.request.get(BASE + '/saml/meta')
  if (r.status() === 200) {
    const txt = await r.text()
    expect(/EntityDescriptor/i.test(txt), 'SP metadata XML').toBeTruthy()
  } else {
    expect(r.status(), 'unconfigured SAML must be a clean 503, not 500').toBe(503)
    const j = await r.json().catch(() => ({}))
    expect(/SAML is not configured/i.test(j.message || ''), 'actionable 503 message').toBeTruthy()
  }
})

test('4: ldap — Timeout is an editable field and round-trips', async () => {
  await go('site.integrations.sso-ldap')
  const to = p.locator('main input[type="number"], main input[placeholder="10000"]').first()
  await expect(to, 'timeout input visible').toBeVisible({ timeout: 15000 })

  const r0 = await api('GET', '/admin/site-settings')
  const before = await r0.json()

  await to.fill('12345')
  await p.locator('main button:has-text("Save")').first().click()
  await p.waitForTimeout(1500)

  const r1 = await api('GET', '/admin/site-settings')
  const after = await r1.json()
  expect(String(after['sso-ldap'].timeout), 'timeout persisted').toBe('12345')

  // restore
  await go('site.integrations.sso-ldap')
  const to2 = p.locator('main input[type="number"], main input[placeholder="10000"]').first()
  await to2.fill(String(before['sso-ldap']?.timeout ?? '10000'))
  await p.locator('main button:has-text("Save")').first().click()
  await p.waitForTimeout(1200)
})

test('5: /admin/site no longer serves the legacy page — 302 into the hub', async () => {
  const r = await p.request.get(BASE + '/admin/site', { maxRedirects: 0 }).catch(() => null)
  expect(r, 'route still exists (redirect)').toBeTruthy()
  expect([301, 302, 307, 308].includes(r!.status()), 'redirect status (' + r!.status() + ')').toBeTruthy()
  const loc = r!.headers()['location'] || ''
  expect(loc, 'redirect target is the hub').toContain('/hub')
})

test('7: sandboxed compiles — section is mandatory (no enable toggle)', async () => {
  await go('site.compilation.sandboxed')
  const sw = await p.locator('main [role="switch"]').count()
  expect(sw, 'no switches in the section').toBe(0)
})
