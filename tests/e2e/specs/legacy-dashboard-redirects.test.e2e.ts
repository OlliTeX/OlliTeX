/**
 * Legacy project-dashboard redirects (P7 cutover contract).
 *
 * Node oracle (services/web/app/src/router.mjs projectDashboardRedirects,
 * owner queue 7 2026-09-10): the legacy project-list pages are removed —
 * every dashboard state renders in the hub (/hub#/projects.*). The routes
 * 301 so bookmarks and SSO deep links land in the hub; Project APIs
 * (POST /project/new*, /user/projects, /project/:id/entities) are untouched.
 *
 * Wire contract (captured 2026-09-22 on the e2e stack, Node v22.21.1):
 *   anonymous: 302 /login, text/plain "Found. Redirecting to /login"
 *   authed:    301, Location=<hub>, text/plain
 *              "Moved Permanently. Redirecting to <hub>"
 *
 * Framework-agnostic: the SAME contract must hold on the Node backend AND
 * the Go backend (1:1 drop-in). This spec is the durable gate — it passes
 * on either backend, so it survives the P7 Node de-commissioning.
 */
import { test, expect } from '@playwright/test'
import { login } from '../helpers/auth'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

const TARGETS: Array<[path: string, hub: string]> = [
  ['/project', '/hub#/projects.all'],
  ['/project/owned', '/hub#/projects.owned'],
  ['/project/shared', '/hub#/projects.shared'],
  ['/project/archived', '/hub#/projects.archived'],
  ['/project/trashed', '/hub#/projects.trashed'],
  ['/project/untagged', '/hub#/projects.all'],
  // Node's own literal target (its static string) — pinned 1:1, not "fixed".
  ['/project/tags/mytag', '/hub#/projects.tags.tags'],
]

test.describe.configure({ mode: 'serial' })

test('anonymous: each dashboard route bounces 302 → /login', async ({ context }) => {
  for (const [path] of TARGETS) {
    const r = await context.request.get(`${BASE}${path}`, { redirect: 'manual' })
    expect(r.status(), path).toBe(302)
    expect(r.headers()['location'], path).toBe('/login')
    const body = await r.body()
    expect(body.toString(), path).toBe('Found. Redirecting to /login')
  }
})

test('authed: each dashboard route 301s to its hub target (exact wire)', async ({ context, page }) => {
  await login(page, ADMIN)
  for (const [path, hub] of TARGETS) {
    const r = await context.request.get(`${BASE}${path}`, { redirect: 'manual' })
    expect(r.status(), path).toBe(301)
    expect(r.headers()['location'], path).toBe(hub)
    expect(r.headers()['content-type'], path).toBe('text/plain; charset=utf-8')
    const body = await r.body()
    expect(body.toString(), path).toBe(`Moved Permanently. Redirecting to ${hub}`)
  }
})
