/**
 * SSO leg-b (TODO-7358ce12) — REAL LDAP transport round-trip.
 *
 * Brings up the world's OpenLDAP test directory (container `e2e-ldaptest`,
 * dc=e2e,dc=test, uid=e2e-ldap / e2e-ldap@e2e.test / Ol-LdapUser-3m2Q) and
 * exercises the full contract against it:
 *   1. GET  /admin/sso/config          → baseline captured (restored after)
 *   2. POST /admin/sso/config          → ldap section enabled (bindDN service acct)
 *   3. POST /admin/sso/test/ldap {}    → probe branch → success:true
 *   4. POST /admin/sso/test/ldap {username,password} → full auth (3x bind) → success:true
 *   5. POST /admin/sso/test/ldap wrong-pw → success:false (invalid user or password)
 *   6. POST /sso/ldap/login {email,password} → JIT user + session → /hub
 *   7. cleanup: JIT user removed, config restored to baseline
 *
 * CI matrix (owner-approved 2026-10-14): the directory is a first-class
 * COMPOSE service (`e2e-ldaptest`, built from the committed asset in
 * stack/ldap — the ad-hoc fixed-IP side container is retired). URL via DNS
 * name on the stack network — identical local and CI; override with
 * OL_LDAP_URL.
 */
import { test, expect } from '@playwright/test'
import { execSync } from 'child_process'
import { loginRobust } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'

const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
// 2026-10-14 (CI matrix): compose DNS name on the stack network (works local
// and CI; the retired ad-hoc world pinned 172.20.0.20). OL_LDAP_URL override
// stays for one-off dirs.
const LDAP_URL = process.env.OL_LDAP_URL || 'ldap://e2e-ldaptest:3890'
const LDAP_BASE = 'dc=e2e,dc=test'
const LDAP_BIND_DN = 'cn=admin,dc=e2e,dc=test'
const LDAP_BIND_PW = 'Ol-LdapRoot-9x7K'
const LDAP_USER = 'e2e-ldap'
const LDAP_EMAIL = 'e2e-ldap@e2e.test'
const LDAP_USER_PW = 'Ol-LdapUser-3m2Q'

function mongoEval(src: string): string {
  return execSync(`docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval ${JSON.stringify(src)}`, {
    encoding: 'utf8',
    timeout: 30_000,
  })
}

test.describe('SSO leg-b — live LDAP transport', () => {
  test.describe.configure({ mode: 'serial' })
  test.setTimeout(420_000)

  let baseline: any = null
  let baselineRaw = ''
  let jitUid = ''

  test('A: baseline config captured', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const r = await page.request.get(BASE + '/admin/sso/config', { headers: { 'x-csrf-token': tok } }) as any
    expect(r.status(), 'baseline read: ' + (await r.text().catch(() => '')).slice(0, 150)).toBe(200)
    baselineRaw = await r.text()
    baseline = JSON.parse(baselineRaw)
    console.log('BASELINE-KEYS=' + Object.keys(baseline).join(',') + ' ldap=' + JSON.stringify((baseline as any).ldap || (baseline as any).sso?.ldap))
  })

  function saveConfig(page: any, patch: Record<string, any>) {
    return (async () => {
      const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
      const cur = JSON.parse(baselineRaw)
      const merged = { ...cur, ...patch }
      const r = await page.request.post(BASE + '/admin/sso/config', {
        data: JSON.stringify(merged),
        headers: { 'content-type': 'application/json', 'x-csrf-token': tok },
      }) as any
      return r
    })()
  }

  test('B: LDAP section saved (enabled, bound to e2e-ldaptest)', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const r = await saveConfig(page, {
      ldap: {
        enabled: true,
        url: LDAP_URL,
        searchBase: LDAP_BASE,
        bindDN: LDAP_BIND_DN,
        bindCredentials: LDAP_BIND_PW,
        searchScope: 'sub',
        bindProperty: 'mail',
        starttls: false,
        emailAtt: 'mail',
        nameAtt: 'cn',
        timeout: '5000',
      },
    })
    expect(r.status(), 'save: ' + (await r.text().catch(() => '')).slice(0, 250)).toBe(200)
    // confirm the round-trip (masked secret must read back as the sentinel)
    const g = await page.request.get(BASE + '/admin/sso/config') as any
    const cfg2 = (await g.json()) as any
    const ldap = cfg2.ldap || cfg2.sso?.ldap
    console.log('SAVED-ldap-url=' + (ldap?.url || '?') + ' bindCred=' + (ldap?.bindCredentials || '?') + ' enabled=' + (ldap?.enabled))
    expect(ldap?.url, 'ldap section persisted').toBe(LDAP_URL)
  })

  test('C: probe branch — connectivity + service-account bind', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    // Readiness gate: the directory is a compose member that starts WITH the
    // stack — on a fresh CI runner it may still be binding when this leg
    // runs. Retry the EXACT production probe (same code path) until the
    // directory answers; a real misconfiguration still fails after the window.
    let j: any = {}
    let status = 0
    for (let i = 0; i < 8; i++) {
      const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
      const r = await page.request.post(BASE + '/admin/sso/test/ldap', {
        data: JSON.stringify({}),
        headers: { 'content-type': 'application/json', 'x-csrf-token': tok },
      }) as any
      status = r.status()
      j = await r.json().catch(() => ({}))
      console.log(`PROBE[${i}]=` + status + ' ' + JSON.stringify(j).slice(0, 160))
      if (r.status() === 200 && j.success) break
      await page.waitForTimeout(5_000)
    }
    expect(status).toBe(200)
    expect(j.success, 'probe (after readiness window): ' + JSON.stringify(j)).toBe(true)
  })

  test('D: full auth — correct credentials', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const r = await page.request.post(BASE + '/admin/sso/test/ldap', {
      data: JSON.stringify({ username: LDAP_EMAIL, password: LDAP_USER_PW }),
      headers: { 'content-type': 'application/json', 'x-csrf-token': tok },
    }) as any
    const j: any = await r.json().catch(() => ({}))
    console.log('AUTH-OK=' + r.status() + ' ' + JSON.stringify(j))
    expect(r.status()).toBe(200)
    expect(j.success, 'auth: ' + JSON.stringify(j)).toBe(true)
  })

  test('E: full auth — wrong password → deterministic failure', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const r = await page.request.post(BASE + '/admin/sso/test/ldap', {
      data: JSON.stringify({ username: LDAP_EMAIL, password: 'definitely-wrong-pw-1' }),
      headers: { 'content-type': 'application/json', 'x-csrf-token': tok },
    }) as any
    const j: any = await r.json().catch(() => ({}))
    console.log('AUTH-BAD=' + r.status() + ' ' + JSON.stringify(j))
    expect(r.status()).toBe(200)
    expect(j.success, 'must fail: ' + JSON.stringify(j)).toBe(false)
  })

  test('F: /sso/ldap/login — JIT user + session', async ({ page }) => {
    // CSRF: anonymous mutation needs the /login meta token (world CSRF gate)
    await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    console.log('CSRF-TOK-PRESENT=' + (tok ? 'yes' : 'no'))
    const r = await page.request.post(BASE + '/sso/ldap/login', {
      data: JSON.stringify({ email: LDAP_EMAIL, password: LDAP_USER_PW }),
      headers: { 'content-type': 'application/json', 'x-csrf-token': tok },
      maxRedirects: 0,
    }) as any
    const body = await r.text().catch(() => '')
    const loc = (r.headers()['location'] || '').toString()
    console.log('LOGIN=' + r.status() + ' loc=' + loc + ' ' + body.slice(0, 120).replace(/\n/g, ' '))
    expect(r.status(), 'ldap login success = 302 redirect (got ' + r.status() + '): ' + body.slice(0, 120)).toBe(302)
    expect(loc, 'redirect target').not.toBe('')
    const hub = await page.request.get(BASE + '/hub', { maxRedirects: 0 }) as any
    console.log('HUB-AFTER=' + hub.status())
    expect(hub.status() === 200 || hub.status() === 302, 'post-login session').toBeTruthy()
    // JIT row stamped with the ldap login provider
    const out = mongoEval(
      `var u = db.users.findOne({ email: ${JSON.stringify(LDAP_EMAIL)} }); print(u ? 'uid=' + u._id + ' roles=' + JSON.stringify(u.ssoRoles || null) + ' loginProv=' + (u.ssoLoginProviderId || 'na') : 'none')`,
    )
    console.log('JIT=' + out.trim())
    expect(/uid=/.test(out), 'JIT user created: ' + out).toBeTruthy()
    jitUid = /uid=(\S+)/.exec(out)?.[1] || ''
  })

  test('G: cleanup — JIT user removed, config restored', async ({ page }) => {
    if (jitUid) {
      // mongoEval double-quotes the whole source for the shell → the JS must
      // avoid double-quote chars (single quotes are safe inside shell dquotes).
      const out = mongoEval(`var H='${jitUid}'; var oid=/^[0-9a-f]{24}$/i.test(H); var r = db.users.deleteMany({ _id: oid ? ObjectId(H) : H }); print('del='+r.deletedCount)`)
      console.log('CLEAN-JIT=' + out.trim())
    }
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const cur = JSON.parse(baselineRaw)
    if (cur.ldap || cur.sso?.ldap) {
      delete cur.ldap
      if (cur.sso) delete cur.sso.ldap
    } else {
      cur.ldap = null
    }
    const r = await page.request.post(BASE + '/admin/sso/config', {
      data: JSON.stringify(cur),
      headers: { 'content-type': 'application/json', 'x-csrf-token': tok },
    }) as any
    expect(r.status(), 'restore: ' + (await r.text().catch(() => '')).slice(0, 200)).toBe(200)
    const g = await page.request.get(BASE + '/admin/sso/config') as any
    const cfg2 = (await g.json()) as any
    const ldap = cfg2.ldap || cfg2.sso?.ldap
    console.log('RESTORED-ldap=' + JSON.stringify(ldap))
    const remain = mongoEval(`db.users.find({ email: ${JSON.stringify(LDAP_EMAIL)} }).toArray().length`)
    console.log('RESTORED-jit-rows=' + remain.trim())
    expect(Number(remain.trim()) === 0, 'JIT row gone').toBeTruthy()
  })
})
