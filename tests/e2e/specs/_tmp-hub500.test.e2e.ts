import { test, expect } from '@playwright/test'
import { LOGIN } from '../helpers/auth'
import { loginRobust } from '../helpers/auth'
test('jit hub after patch', async ({ page }) => {
  await page.goto('http://127.0.0.1:7420/login', { waitUntil: 'domcontentloaded' }).catch(() => {})
  const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const r = await page.request.post('http://127.0.0.1:7420/sso/ldap/login', {
    data: JSON.stringify({ email: 'e2e-ldap@e2e.test', password: 'Ol-LdapUser-3m2Q' }),
    headers: { 'content-type': 'application/json', 'x-csrf-token': tok },
    maxRedirects: 0,
  }) as any
  console.log('LOGIN=' + r.status())
  const hub = await page.request.get('http://127.0.0.1:7420/hub', { maxRedirects: 0 }) as any
  const body = await hub.text().catch(() => '')
  console.log('HUB=' + hub.status() + ' ' + body.slice(0, 150).replace(/\n/g, ' '))
  const err500 = /Something went wrong|500/.test(body)
  console.log('HUB-ERROR-PAGE=' + err500)
})
