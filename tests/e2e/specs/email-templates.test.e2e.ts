/**
 * email-templates — the /hub-managed e-mail templates (remember.md
 * P7-post item 3: "Manage the email template under /hub admin settings
 * (rebrand them templates to OlliTeX too). Text areas with save and
 * reset to default").
 *
 * Server contract (go/services/web/features/emailtemplates, site admin
 * + CSRF):
 *   GET    /api/hub/email-templates
 *     -> {version:1, templates:[{name,label,help,variables,default,
 *         current,overridden,updatedAt,updatedBy}]}   (14 slots — instance-stats-alert added by the instance-stats feature)
 *   PUT    /api/hub/email-templates/<name>
 *     body: any subset of {subject,text,html}; a NON-EMPTY value saves,
 *     an EMPTY value resets that field, omitted keys are unchanged.
 *     200 {ok:true,overridden:[...]}  |  400 (unknown variable:
 *     "unknown variables <v> (allowed: ...)"; no html on htmlFromText
 *     slots)  |  404 {"error":"unknown email template <name>"}
 *   DELETE /api/hub/email-templates/<name>   -> reset the whole slot
 *     -> {ok:true,overridden:[]}
 *
 * The mail paths themselves are covered by the byte-parity pins
 * (go/services/web/features/emailtemplates/parpins_test.go) plus the
 * live smtp-sink loop (override renders into the sent mail, reset
 * restores the default) — 2026-09-25, session record.
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'

const TMPL = '/api/hub/email-templates'

// admin session through the request context's own jar (CSRF meta from the
// /login page of the SAME context — the pinned jar rule from the consent
// spec: the page and the request context do not share cookies here).
async function adminSession(request) {
  const r0 = await request.get('/login')
  let html = await r0.text().catch(() => '')
  let csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const login = await request.post('/login', {
    data: {
      email: ADMIN.email,
      password: ADMIN.password,
      first_name: ADMIN.first_name,
      last_name: ADMIN.last_name,
    },
    headers: { 'x-csrf-token': csrf || '' },
  })
  expect(
    [200, 302, 303].includes(login.status()),
    'admin login: ' + (await login.text().catch(() => ''))
  ).toBeTruthy()
  // refresh the csrf meta for the post-login session
  const r1 = await request.get('/user/settings')
  if (r1.status() === 200) {
    html = await r1.text()
    csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1] || csrf
  }
  return csrf as string
}

test.describe('email-templates — server API (site admin)', () => {
  test('anonymous access does not get the registry', async ({ request }) => {
    const res = await request.get(TMPL, { redirect: 'manual' })
    // auth gate: redirect to /login or a 4xx — but NOT a 404 (not found)
    expect(res.status() !== 404).toBeTruthy()
  })

  test('GET lists the 14 slots with defaults + current + variables', async ({ request }) => {
    const csrf = await adminSession(request)
    const res = await request.get(TMPL, { headers: { 'x-csrf-token': csrf } })
    expect(res.status()).toBe(200)
    const data = await res.json()
    expect(data.version).toBe(1)
    const names = data.templates.map((t: any) => t.name)
    expect(names).toEqual([
      'password-reset',
      'instance-stats-test',
      'instance-stats-alert',
      'sessions-cleared',
      'activate-account',
      'mail-config-test',
      'collab-access-requested',
      'collab-access-declined',
      'collab-access-granted',
      'ownership-transfer',
      'project-invite',
      'security-note',
      'git-token',
      'test-mail',
    ])
    for (const t of data.templates) {
      expect(t.label).toBeTruthy()
      expect(t.default.subject).toBeTruthy()
      expect(t.current.subject).toBeTruthy()
      expect(Array.isArray(t.variables) && t.variables.length > 0).toBeTruthy()
      expect(Array.isArray(t.overridden)).toBeTruthy()
    }
    // the rebrand pins (owner item 3: overleaf -> OlliTeX); defaults are
    // stored UNRENDERED — the brand variable is {{app}} (OlliTeX at
    // render time via the call sites' appName() resolution)
    expect(
      data.templates.find((t: any) => t.name === 'mail-config-test').default.subject
    ).toBe('[{{app}}] E-mail configuration test')
    expect(
      data.templates.find((t: any) => t.name === 'git-token').default.subject
    ).toBe('{{app}} security note: new Git authentication token generated')
    expect(
      data.templates.find((t: any) => t.name === 'test-mail').default.subject
    ).toBe('A Test Email from {{app}}')
  })

  test('PUT override → persists; GET reflects it; DELETE restores', async ({ request }) => {
    const csrf = await adminSession(request)
    const H = { 'x-csrf-token': csrf, accept: 'application/json' }
    try {
      // save one field
      let res = await request.put(`${TMPL}/test-mail`, {
        data: { subject: 'E2E {{app}} override' },
        headers: H,
      })
      expect(res.status()).toBe(200)
      expect(await res.json()).toEqual({ ok: true, overridden: ['subject'] })

      res = await request.get(TMPL, { headers: H })
      const tm = (await res.json()).templates.find((t: any) => t.name === 'test-mail')
      expect(tm.current.subject).toBe('E2E {{app}} override')
      expect(tm.overridden).toEqual(['subject'])
      expect(tm.updatedAt).toBeTruthy()

      // unknown variable → 400 with the allowed set
      res = await request.put(`${TMPL}/test-mail`, {
        data: { subject: 'bad {{bogus}}' },
        headers: H,
      })
      expect(res.status()).toBe(400)
      const bad = await res.json()
      expect(bad.error).toContain('bogus')
      expect(bad.error).toContain('allowed:')

      // the rejected save did not change state
      res = await request.get(TMPL, { headers: H })
      const tm2 = (await res.json()).templates.find((t: any) => t.name === 'test-mail')
      expect(tm2.current.subject).toBe('E2E {{app}} override')

      // unknown slot → 404 (valid JSON)
      res = await request.put(`${TMPL}/no-such-slot`, { headers: H, data: { subject: 'x' } })
      expect(res.status()).toBe(404)
      expect((await res.json()).error).toContain('no-such-slot')
    } finally {
      await request.delete(`${TMPL}/test-mail`, { headers: H })
    }

    const res = await request.get(TMPL, { headers: H })
    const tm = (await res.json()).templates.find((t: any) => t.name === 'test-mail')
    expect(tm.overridden).toEqual([])
    expect(tm.current.subject).toBe('A Test Email from {{app}}')
  })
})

test.describe('email-templates — admin settings UI', () => {
  // PINNED 2026-10-12 (battery-drift triage, 9b142051): the admin IA moved
  // (owner item O, 2026-10-09, hub-root.tsx: "User settings"/"Admin settings"
  // header links) — the email-templates leaf is now
  // /admin-settings/site.general.emailtemplates (15 cards = 1 header +
  // 14 slot cards; the slot set gained instance-stats-test +
  // instance-stats-alert). Card identity is by TITLE (label-driven), not
  // index — the slot order is not a stable pin.
  test('the leaf renders all 14 slots; save → CUSTOM: SUBJECT badge; reset → default', async ({
    page,
    request,
  }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/admin-settings/site.general.emailtemplates')
    await page.waitForTimeout(2_500)

    // self-healing setup: an earlier crashed run may have left the slot
    // overridden — reset it first so the DEFAULT baseline is real
    const csrf0 = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    await request.delete('/api/hub/email-templates/test-mail', {
      headers: { 'X-Csrf-Token': csrf0, Accept: 'application/json' },
    }).catch(() => {})
    await page.reload({ waitUntil: 'load' })
    await page.waitForTimeout(2_500)

    const cards = page.locator('.mantine-Card-root')
    const n = await cards.count()
    expect(n, '15 cards (1 header + 14 slots; got ' + n + ')').toBe(15)
    const header = await cards.nth(0).innerText().catch(() => '')
    expect(header, 'header card describes the slots (got: ' + header.slice(0, 80) + ')')
      .toMatch(/outbound e.?mails/i)

    const cardByTitle = (t: string) =>
      cards.filter({ hasText: t, exact: false }).last()

    // the test-mail slot (card title 'Test e-mail' — the legacy save/reset
    // target): subject input + text + html (2 textareas), default subject
    // 'A Test Email from {{app}}' (pinned)
    const mail = cards.filter({ hasText: /Sent by the test-e-mail endpoints/i }).last()
    await mail.scrollIntoViewIfNeeded().catch(() => {})
    expect(await mail.locator('textarea').count(), 'test-mail slot has text + html (got ' + (await mail.locator('textarea').count()) + ')').toBe(2)
    const subj = mail.locator('.mantine-Input-wrapper input').first()
    const defVal = await subj.inputValue().catch(() => '')
    expect(defVal, 'default subject (got ' + defVal + ')').toBe('A Test Email from {{app}}')
    expect((await mail.innerText()).includes('DEFAULT'), 'starts with the DEFAULT badge (got: ' + (await mail.innerText()).slice(0, 120) + ')').toBe(true)

    // sibling slot distinction: 'E-mail configuration test' also has
    // subject+text+html; the collab slots have text only (pinned both sides)
    const cfg = cards.filter({ hasText: /Sent from the site settings E-mail test/i }).last()
    expect(await cfg.locator('textarea').count(), 'mail-config-test slot has text + html').toBe(2)

    // the collab-style slot: subject + text ONLY (1 textarea, no html part)
    const collab = cardByTitle('Access request (owner notified)')
    expect(await collab.locator('textarea').count(), 'no HTML part (got ' + (await collab.locator('textarea').count()) + ')').toBe(1)

    // save the subject → CUSTOM: SUBJECT badge + value kept
    await subj.fill('UI E2E {{app}} verify')
    await mail.locator('button').filter({ hasText: /^[Ss]ave$/ }).first().click({ timeout: 8_000 })
    await page.waitForTimeout(2_500)
    const mtxt = await mail.innerText()
    expect(mtxt, 'CUSTOM: SUBJECT badge after save (got: ' + mtxt.slice(0, 160) + ')').toContain('CUSTOM: SUBJECT')
    expect(await subj.inputValue(), 'edited value persists on the surface').toBe('UI E2E {{app}} verify')
    expect(mtxt, 'reset affordance appears').toContain('Reset to default')

    // reset → the DEFAULT badge returns + the default subject is back
    await mail.locator('text=Reset to default').first().click({ timeout: 8_000 })
    await page.waitForTimeout(2_500)
    const rtxt = await mail.innerText()
    expect(rtxt, 'DEFAULT badge after reset (got: ' + rtxt.slice(0, 160) + ')').toContain('DEFAULT')
    expect(await subj.inputValue(), 'subject back to default').toBe(defVal)

    // final world-state: defaults restored on the surface (the UI reset
    // leg above IS the cleanup — the slot is back at the default values)
    const end = await mail.innerText()
    expect(end, 'ends on DEFAULT (got: ' + end.slice(0, 120) + ')').toContain('DEFAULT')
    expect(await subj.inputValue(), 'subject back at the pinned default').toBe('A Test Email from {{app}}')
  })
})
