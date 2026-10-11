/**
 * git-clone-bridge.test.e2e.ts — board TODO-ff7d6b1a (git integration):
 * a LIVE edited Overleaf project is reachable through the git-bridge.
 *
 * Root causes fixed (this wave):
 *   1. web /api/v0/docs (the bridge-facing snapshot API) was served from
 *      the OT V2 plane ONLY — whose flush chain had no producer since
 *      the D19 Yjs hard cut (yops→history 500/0-versions stack-wide).
 *      It is now served Yjs-first from the per-room version log (the
 *      same 024-Option-B unified-timeline machinery the history panel
 *      uses), with OT V2 as fallback.
 *   2. The Go bridge's snapshot API client DISCARDED the validated PAT
 *      (NetSnapshotApi sent no Authorization header at all) → every
 *      materialization attempt died with 401 → "project does not
 *      exist" / 403. It now sends `Authorization: Bearer <token>`
 *      (the data/oauth2.go contract).
 *
 * Pins:
 *   1. edit a project in the editor, then `git clone` it through the
 *      bridge at http://127.0.0.1:7420/git/<pid> (nginx /git/ →
 *      bridge) — real git + real materialization, the exact surface
 *      that was broken for every project before the fix.
 *   2. the cloned working tree's main.tex carries the typed editor
 *      marker (snapshot content from the Yjs timeline).
 *
 * Out of scope (tracked separately):
 *   - `git push` — the OT postSnapshot plane is absent in this stack;
 *     a Yjs apply path is needed (degraded-but-safe today).
 *   - cloning UNEDITED template projects (no yjs versions yet) and
 *     binary (non-text) files in the snapshot — v1.5 docstore/atts legs.
 */
import { test, expect } from '@playwright/test'
import { execSync } from 'node:child_process'
import { loginRobust } from '../helpers/auth'
import { api } from '../parity/harness'
import { USER } from '../fixtures/credentials'

const MARKER = 'OLPSINT_GIT_CLONE_PROBE_4242'

test.describe.configure({ mode: 'serial' })

test('edited project clones through the git-bridge with typed content', async ({ page, context }) => {
  await loginRobust(page, USER.email, USER.password)

  const r = await api(page, 'POST', '/project/new', { projectName: 'e2e-git-clone-bridge' })
  expect(r.status()).toBeLessThan(300)
  const pj: any = await r.json().catch(() => ({}))
  const pid = pj?.project_id || pj?._id
  expect(pid, 'project id').toMatch(/^[0-9a-f]{24}$/)

  const tk = await api(page, 'POST', '/git-bridge/personal-access-tokens')
  expect([200, 201, 202, 204].includes(tk.status()), JSON.stringify(await tk.json().catch(() => ({})))).toBe(true)
  const tkj: any = await tk.json().catch(() => ({}))
  const TOK: string = tkj.accessToken || ''
  expect(TOK, 'PAT plaintext').toMatch(/^olp_/)

  // ---- edit the project (the yjs room gets a version) ----
  await page.goto('/editor/' + pid, { waitUntil: 'commit' })
  const cm = page.locator('.cm-editor')
  await expect(cm.first(), 'cm6 editor mounts').toBeVisible({ timeout: 45_000 })
  await cm.first().click()
  await page.keyboard.press('ControlOrMeta+End')
  await page.keyboard.press('Enter')
  await page.keyboard.type('% ' + MARKER, { delay: 8 })
  // let collab persist the update to the ydoc room log
  await page.waitForTimeout(2_500)
  await context.close()

  // ---- CLONE through the bridge (host-reachable 7420/git/ surface),
  //      polling until the version plane has the edit. ----
  let markerFile: string | null = null
  let lastErr = ''
  for (let i = 0; i < 40; i++) {
    try {
      execSync('rm -rf /tmp/olpsint-gc-clone', { stdio: 'ignore' })
      execSync('git clone --quiet http://git:' + TOK + '@127.0.0.1:7420/git/' + pid + ' /tmp/olpsint-gc-clone', { stdio: 'pipe' })
      markerFile = execSync('grep -rl "' + MARKER + '" /tmp/olpsint-gc-clone --exclude-dir=.git', { encoding: 'utf8' }).trim().split('\n')[0] || null
      break
    } catch (e: any) {
      lastErr = String(e?.message || e).slice(0, 300)
      await new Promise((res) => setTimeout(res, 1_500))
    }
  }
  expect(markerFile, 'clone succeeds and main.tex carries the typed marker (last error: ' + lastErr + ')').toMatch(/main\.tex$/)
})

test.afterAll(async () => {
  try {
    execSync('rm -rf /tmp/olpsint-gc-clone', { stdio: 'ignore' })
  } catch {
    /* best-effort cleanup */
  }
})
