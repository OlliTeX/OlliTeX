import { test, expect } from '@playwright/test'
import { loginRobust, authFetch, csrfToken } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { ADMIN } from '../fixtures/credentials'

/**
 * python-runner module (ported from ayakaleaf-pro, owner-provided 2026-09-06):
 * browser-side Python execution (Pyodide in a browser Worker) with a
 * split-editor output pane and Run/Stop controls.
 *
 * Gating (two independent gates, both asserted):
 *  1. USER GATE — the `overleaf-code` split test resolves to the `enabled`
 *     variant. In CE builds (no saas feature) SplitTestHandler short-circuits
 *     to `Settings.splitTestOverrides`, where this repo wires it to the
 *     `ENABLE_PYTHON_RUNNER` env (settings.defaults.js). The test stack sets
 *     it to 'true' (docker-compose.test.yml); a stock CE image without the
 *     env keeps the python UI hidden.
 *  2. FILE GATE — the pane only renders for an ACTIVATE .py document
 *     (editor.tsx: openEntity.type==='doc' && name.endsWith('.py')).
 *
 * This suite runs against the flag-ON stack, so it asserts the FILE gate
 * (python UI appears only for .py) plus the full feature round-trip
 * (Run → stdout captured → file written by the script lands in the project
 * tree). The flag-OFF behavior is the stock default (see above).
 *
 * Environment: ol-e2e stack at http://127.0.0.1:7420 with
 * ENABLE_PYTHON_RUNNER=true.
 */
const PY_CODE = [
  'print("hello-ollitex")',
  '',
  'with open("out.txt", "w") as fh:',
  '    fh.write("written from python")',
  '',
].join('\n')

/** 6.3.0: project.rootFolder is an ARRAY; the root folder id =
 *  rootFolder[0]._id (per-project ObjectId). */
function rootFolderIdOf(projectId: string): string {
  const out = mongoEval(
    'const p = db.getSiblingDB("sharelatex").projects.findOne({_id: ObjectId("' +
      projectId +
      '")});' +
      ' print(p && p.rootFolder && p.rootFolder[0] ? String(p.rootFolder[0]._id) : "none");'
  )
  const m = out.match(/([a-f0-9]{24})/)
  if (!m) throw new Error('root folder not found for project ' + projectId)
  return m[1]
}

function projectFileExists(projectId: string, fileName: string): boolean {
  const out = mongoEval(
    'const p = db.getSiblingDB("sharelatex").projects.findOne({_id: ObjectId("' +
      projectId +
      '")}); const s = JSON.stringify(p && p.rootFolder ? p.rootFolder : {});' +
      ' print(s.includes(' +
      JSON.stringify(fileName) +
      ') ? "yes" : "no");'
  )
  return /yes/.test(out)
}

/** Create a project containing main.py (upload via the app API). */
async function newProjectWithPython(
  context: import('playwright').APIRequestContext,
  page: import('playwright').Page,
  name: string
): Promise<string> {
  const created = await authFetch(context, page, 'POST', '/project/new', {
    projectName: name,
  })
  expect(created.status()).toBeLessThan(300)
  const projectId = (await created.json()).project_id as string
  const rootFolderId = rootFolderIdOf(projectId)
  const up = await context.request.post(
    `/project/${projectId}/upload?folder_id=${rootFolderId}`,
    {
      headers: { 'X-CSRF-TOKEN': await csrfToken(page) },
      multipart: {
        qqfile: {
          name: 'main.py',
          mimeType: 'text/x-python',
          buffer: Buffer.from(PY_CODE, 'utf8'),
        },
        name: 'main.py',
        relativePath: 'main.py',
      },
    }
  )
  expect(up.status(), `upload main.py (got ${await up.text()})`).toBeLessThan(300)
  return projectId
}

/**
 * admin/site control (owner 2026-09-06, D2 pattern): misc.pythonRunner is
 * the canonical switch — hydrated to ENABLE_PYTHON_RUNNER at boot
 * (EnvHydrator) and read by the splitTestOverrides['overleaf-code'] CE
 * flag. Assert the admin round-trip works (GET -> modify -> PUT -> GET).
 * NOTE: like every D2 section, the FUNCTIONAL flag switches on the next
 * container cycle — the test stack therefore also carries
 * ENABLE_PYTHON_RUNNER=true in compose (seed path); what this test proves
 * is the admin surface, not a live switch.
 */
async function adminPythonToggle(
  context: import('playwright').APIRequestContext,
  page: import('playwright').Page
): Promise<void> {
  const res = await authFetch(context, page, 'GET', '/admin/site-settings')
  expect(res.status()).toBe(200)
  const all = await res.json()
  const misc = { ...all.misc, pythonRunner: true }
  const put = await authFetch(context, page, 'PUT', '/admin/site-settings/misc', misc)
  expect(put.status(), `admin/site misc.pythonRunner save (got ${await put.text()})`).toBeLessThan(300)
  const after = await authFetch(context, page, 'GET', '/admin/site-settings')
  expect(((await after.json()).misc || {}).pythonRunner, 'admin/site round-trip').toBe(true)
}

test.describe.configure({ mode: 'serial' })

test('admin/site: python-runner toggle persists (misc.pythonRunner round-trip)', async ({
  page,
  context,
}) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  // 2026-09-10: legacy /project dashboard removed → hub surface (any logged-in page gives the CSRF meta)
  await page.goto('/hub#/projects.all', { waitUntil: 'domcontentloaded' })
  await adminPythonToggle(context, page)
})


test('python split view is NOT rendered for non-.py documents', async ({
  page,
  context,
}) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const projectId = await newProjectWithPython(
    context,
    page,
    `e2e-py-nongate-${Date.now().toString(36)}`
  )

  // the editor opens on main.tex by default (project template)
  await page.goto(`/project/${projectId}`, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(6000)

  // even in the ENABLED variant, opening a non-python document must not
  // show the python output pane (FILE gate in editor.tsx)
  await expect(page.locator('.ide-redesign-python-output-pane'), 'no python pane for main.tex').toHaveCount(0)
})

test('python split view RUNS, PRINTS and WRITES an output file for .py documents', async ({
  page,
  context,
}) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const projectId = await newProjectWithPython(
    context,
    page,
    `e2e-py-gate-${Date.now().toString(36)}`
  )

  // open the project in the ENABLED variant (flag-ON test stack)
  await page.goto(`/project/${projectId}`, { waitUntil: 'domcontentloaded' })

  // activate the .py document from the file tree
  const pyLeaf = page
    .locator('.file-tree-entity-details')
    .filter({ hasText: 'main.py' })
  await expect(pyLeaf, 'main.py visible in the file tree').toBeVisible({ timeout: 20_000 })
  await pyLeaf.first().click()
  await page.waitForTimeout(4000)

  // the split view + run controls appear (FILE gate satisfied + ENABLED variant)
  const pane = page.locator('.ide-redesign-python-output-pane')
  await expect(pane, 'python output pane must render for the .py document').toBeVisible({
    timeout: 30_000,
  })

  // the owner-requested run-button caveat tooltip
  const wrapper = page.locator('.ide-redesign-python-output-pane-run-button-wrapper')
  await expect(wrapper, 'run button wrapper visible').toBeVisible()
  await expect
    .poll(() => wrapper.getAttribute('title'), { timeout: 10_000 })
    .toMatch(/only run code you can read/i)

  // run the script and wait for the captured stdout
  await page.locator('button[aria-label="Run Python code"]').click()
  const stdout = page.locator('.ide-redesign-python-output-pane, [class*="python-output"]')
  await expect
    .poll(async () => (await stdout.first().innerText().catch(() => '')), {
      timeout: 60_000,
      message: 'stdout must contain hello-ollitex',
    })
    .toContain('hello-ollitex')

  // the script wrote out.txt — prove the UPLOAD round-trip at the source:
  // the file must land inside the project's embedded file tree (6.3.0 stores
  // it in projects.rootFolder).
  await expect
    .poll(() => projectFileExists(projectId, 'out.txt'), {
      timeout: 30_000,
      message: 'out.txt must land in the project file tree',
    })
    .toBe(true)
})
