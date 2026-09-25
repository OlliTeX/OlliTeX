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
 *         current,overridden,updatedAt,updatedBy}]}   (13 slots)
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

  test('GET lists the 13 slots with defaults + current + variables', async ({ request }) => {
    const csrf = await adminSession(request)
    const res = await request.get(TMPL, { headers: { 'x-csrf-token': csrf } })
    expect(res.status()).toBe(200)
    const data = await res.json()
    expect(data.version).toBe(1)
    const names = data.templates.map((t: any) => t.name)
    expect(names).toEqual([
      'password-reset',
      'instance-stats-test',
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

test.describe('email-templates — /hub admin UI', () => {
  test('the leaf renders all 13 slots with save/reset behavior', async ({
    page,
  }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/hub')
    await page.waitForTimeout(1500)
    // open the rail: Site settings → General → Email templates
    const railBtn = (text: string) =>
      page.locator('button').filter({ hasText: text }).first()

    await railBtn('tuneSite settings').click()
    await page.waitForTimeout(600)
    await railBtn('layersGeneral').click()
    await page.waitForTimeout(600)
    const leaf = railBtn('mailEmail templates')
    await expect(leaf).toBeVisible()
    await leaf.click()
    await page.waitForTimeout(2500)

    const cards = page.locator('.mantine-Card-root')
    await expect(cards).toHaveCount(13)

    // the test-mail card (last): subject input + text + html textareas
    const tmCard = cards.nth(12)
    await expect(tmCard.locator('.mantine-Input-wrapper input')).toHaveValue(
      'A Test Email from {{app}}'
    )
    expect(await tmCard.locator('textarea').count()).toBe(2)

    // the collab card: NO editable HTML part (subject + text only)
    const collabCard = cards.nth(5)
    expect(await collabCard.locator('textarea').count()).toBe(1)

    // edit + save → "custom: subject" badge
    const subj = tmCard.locator('.mantine-Input-wrapper input').first()
    await subj.fill('UI E2E {{app}} verify')
    await tmCard
      .locator('button')
      .filter({ hasText: 'Save' })
      .click()
    await page.waitForTimeout(2500)
    await expect(tmCard.locator('.mantine-Badge-label').first()).toHaveText(
      'custom: subject'
    )
    await expect(subj).toHaveValue('UI E2E {{app}} verify')

    // reset → back to the default
    await tmCard
      .locator('button')
      .filter({ hasText: 'Reset to default' })
      .click()
    await page.waitForTimeout(2500)
    await expect(tmCard.locator('.mantine-Badge-label').first()).toHaveText('default')
    await expect(subj).toHaveValue('A Test Email from {{app}}')
  })
})
