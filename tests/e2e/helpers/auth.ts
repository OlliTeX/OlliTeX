/**
 * Auth helpers for the e2e suite (proven flows from the live batteries):
 *  - login: #email / #password, post-login lands /project
 *  - CSRF: X-CSRF-TOKEN from meta[name=ol-csrfToken] on any HTML page
 *  - registration: POST /user/registration (fresh stack bootstrap)
 */
import type { Page, APIRequestContext } from '@playwright/test'
import { expect } from '@playwright/test'
import type { Account } from './ctx'

export async function login(page: Page, account: Account): Promise<void> {
  await page.goto('/login')
  await page.fill('#email', account.email)
  await page.fill('#password', account.password)
  await page.click('button[type=submit]')
  await page.waitForURL(/\/project/, { timeout: 30_000 })
}

export async function csrfToken(page: Page): Promise<string> {
  const token = await page
    .locator('meta[name="ol-csrfToken"]')
    .getAttribute('content')
  if (!token) {
    throw new Error('ol-csrfToken meta not present on current page')
  }
  return token
}

/**
 * Authenticated JSON request that carries the current page's CSRF token.
 * `page` must be logged in (its cookies are shared via context.request).
 */
export async function authFetch(
  context: APIRequestContext,
  page: Page,
  method: 'GET' | 'POST' | 'PUT' | 'DELETE',
  url: string,
  body?: unknown
): Promise<import('playwright').APIResponse> {
  const headers: Record<string, string> = {}
  if (method !== 'GET') {
    headers['X-CSRF-TOKEN'] = await csrfToken(page)
    if (body !== undefined) headers['Content-Type'] = 'application/json'
  }
  return context.request.fetch(url, {
    method,
    headers,
    data: body === undefined ? undefined : JSON.stringify(body),
  })
}

export async function registerUser(
  context: APIRequestContext,
  page: Page,
  account: Account
): Promise<void> {
  const token = await csrfToken(page)
  await page.goto('/register')
  const tokenAfter = await csrfToken(page) // rotation-safe: re-read after navigation
  const res = await context.request.post('/user/registration', {
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-TOKEN': tokenAfter || token,
    },
    data: JSON.stringify({
      email: account.email,
      password: account.password,
      first_name: account.first_name,
      last_name: account.last_name,
    }),
  })
  if (res.status() >= 400 && !(res.status() === 401 || res.status() === 409)) {
    throw new Error(
      `registration failed (${res.status()}): ${await res.text().catch(() => '')}`
    )
  }
}

/**
 * Robust login: CE rate-limits login attempts (20/min/IP + 200/min /8 subnet;
 * on exceed → CAPTCHA). Suite + manual probes share the IP, so a transient
 * throttle mid-run is a realistic failure mode. Retry with backoff on any
 * login miss (page not on /project). 2 attempts max (keeps the run calm).
 */
export async function loginRobust(
  page: import('@playwright/test').Page,
  email: string,
  password: string
): Promise<void> {
  // CE rate-limits login attempts; the e2e suite + seed + manual probes
  // share one source IP, so a mid-run throttle (or CAPTCHA challenge page)
  // is realistic. 3 attempts with escalating cooldowns (40s / 80s) clear
  // the 20/min/IP window; keep the last throw for real credential errors.
  const cooldowns = [0, 40_000, 80_000]
  for (let attempt = 0; attempt < cooldowns.length; attempt++) {
    if (cooldowns[attempt] > 0) await page.waitForTimeout(cooldowns[attempt])
    await page.goto('/login', { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(700)
    await page.fill('#email', email)
    await page.fill('#password', password)
    await page.click('button[type=submit]')
    const ok = await page
      .waitForURL(/\/project/, { timeout: 12_000 })
      .then(() => true)
      .catch(() => false)
    if (ok) return
  }
  throw new Error(`login failed for ${email} (rate-limit or credentials)`)
}

/**
 * Open the "new project" control and pick "Blank project", from EITHER page
 * state (they differ — this bit us):
 *   - project list page  → "New project" button → .project-list-modal dialog
 *   - welcome/empty page → "Create a new project" → inline role=menu
 * Identical menu markup exists in BOTH layers (modal + welcome section) and
 * the background copy intercepts plain clicks (ghost click — verified). If the
 * UI path doesn't navigate within its budget, fall back to the deterministic
 * API (POST /project/new) and go straight to the editor — the golden path in
 * the smoke spec is login → editor → compile → PDF, not the menu UI.
 * Returns the new project id after the editor URL loads.
 */
export async function createBlankProject(
  page: import('@playwright/test').Page
): Promise<string> {
  await page.goto('/project')
  const trigger = page
    .locator(
      'button:has-text("New project"), button:has-text("Create a new project"), [aria-label="New project"]'
    )
    .first()

  // 2026-09-10 (legacy /project dashboard removed): the new-project surface
  // is now the hub (the legacy URL 301s to it). Hub flow:
  //   1. "New project" button opens the hub menu
  //   2. "Blank project" item opens the hub modal (it does NOT create)
  //   3. the hub modal's Create button POSTs /project/new and lands on
  //      /project/<id> (the editor route)
  await expect(trigger).toBeVisible({ timeout: 20_000 })
  await trigger.click({ timeout: 15_000 })
  const item = page.locator('text=Blank project').first()
  await expect(item).toBeVisible({ timeout: 15_000 })
  await item.click({ timeout: 15_000 })
  const nameField = page.locator('label:has-text("Project name") input, #hub-new-project-name').first()
  await expect(nameField).toBeVisible({ timeout: 20_000 })
  await nameField.fill('e2e-smoke-project')
  const createBtn = page
    .locator('[role="dialog"] button:has-text("Create"), .mantine-Modal-root button:has-text("Create")')
    .first()
  await createBtn.click({ force: true, timeout: 15_000 })
  const navigated = await page
    .waitForURL(/\/project\/[0-9a-f]{24}/, { timeout: 20_000 })
    .then(() => true)
    .catch(() => false)
  if (navigated) {
    return new URL(page.url()).pathname.split('/')[2]
  }
  await page.keyboard.press('Escape').catch(() => {})
  await page.waitForTimeout(800)

  // deterministic fallback: create via the JSON API and open the editor
  const context = page.context()
  const res = await context.request.post('/project/new', { data: { projectName: 'e2e-smoke' } })
  if (res.status() !== 200) {
    const csrf = await page
      .locator('meta[name="ol-csrfToken"]')
      .getAttribute('content')
      .catch(() => null)
    const res2 = await context.request.post('/project/new', {
      data: { projectName: 'e2e-smoke' },
      headers: csrf ? { 'x-csrf-token': csrf } : {},
    })
    if (res2.status() !== 200) {
      throw new Error(`project creation failed (ui + api ${res.status()}/${res2.status()})`)
    }
    await page.goto(`/project/${(await res2.json()).projectId}`)
  } else {
    await page.goto(`/project/${(await res.json()).projectId}`)
  }
  await page.waitForURL(/\/project\/[0-9a-f]{24}/, { timeout: 30_000 })
  return new URL(page.url()).pathname.split('/')[2]
}
