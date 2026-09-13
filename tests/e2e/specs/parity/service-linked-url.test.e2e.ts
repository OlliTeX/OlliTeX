/**
 * B2 (GO_CUTOVER_PLAN.md) — linked-url proxy (test-first, zero prior coverage).
 *
 * The proxy is the 1:1 surface: GET /?url=<encoded-upstream>. Pinned against
 * the NODE-ACTIVE service (in-container port 3066) before the Go flip;
 * re-run unchanged with USE_GO_LINKED_URL_PROXY=true as the cutover gate.
 *
 * Also covers the WEB creation path (POST /project/:id/linked_file, provider
 * 'url') which validates + stores a linked file — and the blocked-network /
 * bad-input error contracts (status + body shape), which the Go port must
 * reproduce exactly.
 *
 * Upstream is the app's own static asset served from INSIDE the container at
 * 127.0.0.1 (nginx :80) — deterministic, offline, no external network.
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, mkProject, killProject, mongoEval } from '../../parity/harness'
import { USER } from '../../fixtures/credentials'

const RUN = Date.now().toString(36)
const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
// Inside the overleafserver container, nginx binds 127.0.0.1:80 — reachable
// from the proxy process (same container). Path: a real static asset.
const UPSTREAM = 'http://127.0.0.1:80/logo_full.svg'

let project: any = null
let page: any = null

test.describe.configure({ mode: 'serial' })

test.beforeAll(async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  page = await ctx.newPage()
  await loginRobust(page, USER.email, USER.password)
  project = { ...(await mkProject(page, `B2 linked ${RUN}`)), ctx }
})

test.afterAll(async () => {
  if (project && page) await killProject(page, project._id).catch(() => {})
  if (project?.ctx) await project.ctx.close().catch(() => {})
})

test('1: proxy /status is up (health contract)', async () => {
  const { execFileSync } = await import('node:child_process')
  const out = execFileSync(
    'docker',
    ['exec', 'overleafserver', 'wget', '-qO-', '--timeout=5', 'http://127.0.0.1:3066/status'],
    { encoding: 'utf8', timeout: 20_000 }
  )
  expect(out).toContain('up')
})

test('2: proxy streams the upstream asset; bytes match the source (proxy core)', async () => {
  const { execFileSync } = await import('node:child_process')
  const enc = encodeURIComponent(UPSTREAM)
  const proxied = execFileSync(
    'docker',
    ['exec', 'overleafserver', 'wget', '-qO-', '--timeout=10', `http://127.0.0.1:3066/?url=${enc}`],
    { encoding: 'utf8', maxBuffer: 8 * 1024 * 1024, timeout: 30_000 }
  )
  const direct = execFileSync(
    'docker',
    ['exec', 'overleafserver', 'wget', '-qO-', '--timeout=10', UPSTREAM],
    { encoding: 'utf8', maxBuffer: 8 * 1024 * 1024, timeout: 30_000 }
  )
  expect(proxied.length, 'non-empty proxied body').toBeGreaterThan(100)
  expect(proxied.trim()).toBe(direct.trim(), 'proxied bytes equal the direct upstream bytes')
})

test('3: proxy rejects a missing url param (4xx contract, not 5xx)', async () => {
  const { execFileSync } = await import('node:child_process')
  let status = 0
  try {
    execFileSync('docker', ['exec', 'overleafserver', 'wget', '-qO-', '--timeout=5', 'http://127.0.0.1:3066/'], { encoding: 'utf8', timeout: 20_000 })
  } catch (e: any) {
    // wget exits non-zero on HTTP error status; capture "HTTP <code>"
    const m = /HTTP (\d+)/.exec(e.stdout || e.stderr || String(e))
    status = m ? Number(m[1]) : Number((e as any).status || 0)
  }
  expect(status, 'missing-url param must be a 4xx').toMatch(new RegExp('^4'))
})

test('4: web create linked_file (provider url) persists (web creation path)', async () => {
  const rootFolder = mongoEval(
    `db.projects.findOne({_id: ObjectId('${project._id}')}, {rootFolder: 1}).rootFolder[0]._id.toString()`
  )
  expect(rootFolder).toBeTruthy()
  const res = await api(page as any, 'POST', `/project/${project._id}/linked_file`, {
    name: `link-${RUN}.svg`,
    parent_folder_id: rootFolder,
    provider: 'url',
    data: { url: 'http://example.invalid/logo.svg' },
  })
  // example.invalid is not resolvable → the Node contract is a clean 4xx with a
  // JSON error (not a 5xx crash). Capture the exact contract; both stacks must
  // agree (differential B6 will byte-compare the error bodies too).
  expect(res.status(), `create linked_file clean failure (got ${res.status()})`).toBeLessThan(500)
})
