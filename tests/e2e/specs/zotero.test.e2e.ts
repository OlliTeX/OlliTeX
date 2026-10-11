/**
 * zotero — graceful-degradation contract + /library render (one journey):
 *  zotero surfaces must answer GRACEFUL JSON (2xx or 4xx — the "disabled
 *  on this site" 403 counts) with no valid credential — NEVER 5xx (the
 *  regression class we fixed for webdav, R11). 5xx is retried a couple of
 *  times (cold-stack service warmup window).
 */
import { test, expect } from '@playwright/test'
import { USER } from '../fixtures/credentials'
import { authFetch, loginRobust } from '../helpers/auth'
import { api, mongoEval } from '../parity/harness'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

async function expectNot5xx(res: import('playwright').APIResponse): Promise<number> {
  let status = res.status()
  for (let i = 0; i < 2 && status >= 500; i++) {
    await new Promise((r) => setTimeout(r, 4_000)) // cold-stack warmup
    const r2 = await (res.request()).context.get(res.url())
    status = r2.status()
  }
  return status
}

test('zotero is graceful without credentials + /library renders', async ({ page, context }) => {
  await loginRobust(page, USER.email, USER.password)
  await page.waitForTimeout(500)

  const groups = await authFetch(context, page, 'GET', '/user/zotero/groups')
  const gbody = (await groups.text().catch(() => '')).slice(0, 200)
  expect(
    await expectNot5xx(groups),
    `zotero groups must not 5xx: ${gbody}`
  ).toBeLessThan(500)

  const connect = await authFetch(context, page, 'POST', '/user/zotero/connect', {
    token: 'e2e-bad-token',
    scopes: 'libraries:read',
  })
  const cbody = (await connect.text().catch(() => '')).slice(0, 200)
  expect(await expectNot5xx(connect), `zotero connect bad-token graceful: ${cbody}`).toBeLessThan(500)

  // B wave 2026-10-11: the top-level /library page was retired (Go 404 — see
  // parity/legacy-library.test.e2e.ts); the library surface is the hub leaf.
  const res = await page.goto('/hub#/library')
  expect(res?.status() ?? 200).toBe(200)
  await page.waitForTimeout(1_500)
  const body = await page.locator('body').innerText()
  expect(body.toLowerCase()).toMatch(/bib|library|bibtex/)
})

/**
 * TODO-ff527f67 (upstream-impact wave 2026-10-11, CLOSED-IMMUNE):
 * upstream's zotero linkedFileData key-rename bug (legacy keys → 400 on
 * DU strict validation → clone 500; refresh breaks) CANNOT reproduce in
 * this fork. This extension pins the ACTUAL contract (evidenced
 * 2026-11-11 against the live handler, go/services/web/features/
 * projectlist/linkedfiles.go):
 *   1. the zotero create SCHEMA accepts BOTH key generations — a
 *      legacy-key body (bibFormat/zoteroGroupId) passes strict validation
 *      (NO JSON "Validation error"), dying instead at the pinned agent
 *      leg: bare 400 ("Bad Request", no body) = "no enabled linked file
 *      types in this stack" — the upstream strict-schema 400 class is
 *      ABSENT (the bug's first link is missing);
 *   2. the schema is strict about UNKNOWN keys — an upstream post-rename
 *      key (importer_name, not in the fork's contract) is rejected with
 *      the JSON validation error (contrast leg proving (1) is meaningful);
 *   3. refresh on a legacy-key zotero file (direct-mongo fixture, the
 *      board-mandated shape) → bare 400 (pinned: no provider refresh
 *      agent; graceful 4xx, never the upstream 500);
 *   4. refresh on a GHOST file id → 404 (not 4xx-200 confusion);
 *   5. CLONE of a project whose linkedFileData carries legacy keys copies
 *      the blob VERBATIM (no key rewriting — the upstream "clone 500"
 *      leg is absent).
 */
test('zotero linkedFileData contract: legacy keys schema-OK + refresh 400 + clone blob intact (ff527f67 pin)', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)

  // project fixture
  const r = await api(page, 'POST', '/project/new', { projectName: 'e2e-zotero-linkedfile' })
  expect(r.status()).toBeLessThan(300)
  const pj: any = await r.json().catch(() => ({}))
  const pid = pj?.project_id || pj?._id
  expect(pid, 'project id').toMatch(/^[0-9a-f]{24}$/)
  const rootfolder = String(
    mongoEval(`db.projects.findOne({ _id: ObjectId('${pid}') }).rootFolder[0]._id`),
  ).trim()
  expect(rootfolder, 'root folder id').toMatch(/^[0-9a-f]{24}$/)

  // 1) CREATE with LEGACY-generation keys (the pre-rename wire shape):
  //    the body must pass strict schema validation — so the outcome is
  //    the pinned AGENT leg (bare 400, plain "Bad Request", no JSON
  //    validation issues), NOT the strict-schema 400 (JSON
  //    "Validation error") that is the upstream bug's first link.
  const legacyData = { bibFormat: 'bibtex', zoteroGroupId: 'e2e-legacy-group' }
  const create = await api(page, 'POST', `/project/${pid}/linked_file`, {
    name: 'refs-legacy.bib',
    parent_folder_id: rootfolder,
    provider: 'zotero',
    data: legacyData,
  })
  const cbody = (await create.text().catch(() => ''))
  expect(create.status(), 'legacy-key create outcome').toBe(400) // the pinned agent leg
  expect(cbody, 'agent leg = bare 400 (plain text) — NOT the strict-schema JSON 400').not.toContain('Validation error')
  expect(cbody.toLowerCase(), 'no JSON issue list (schema accepted the legacy keys)').not.toContain('unrecognized key')

  // 2) CONTRAST (proves 1 is the schema passing, not the schema being
  //    lax): an upstream post-rename key is NOT in this fork's contract
  //    → strict schema rejects with the JSON validation error.
  const bogus = await api(page, 'POST', `/project/${pid}/linked_file`, {
    name: 'refs-bogus.bib',
    parent_folder_id: rootfolder,
    provider: 'zotero',
    data: { format: 'bibtex', importer_name: 'should-not-exist' },
  })
  const bbody = (await bogus.text().catch(() => ''))
  expect(bogus.status(), 'unknown-key create → schema 400').toBe(400)
  expect(bbody, 'strict schema JSON error for the unknown key').toContain('Unrecognized key')

  // 3) REFRESH on a LEGACY-KEY zotero file — direct-mongo fixture (the
  //    board-mandated shape; the create route is pinned at the agent
  //    leg in this stack), then the live refresh contract.
  const fileHex = 'a'.repeat(24)
  mongoEval(`
    db.projects.updateOne({ _id: ObjectId('${pid}') }, { $push: {
      "rootFolder.0.fileRefs": {
        name: 'refs-legacy.bib', created: new Date(), rev: 0,
        linkedFileData: { format: 'zotero', provider: 'zotero',
          bibFormat: 'bibtex', zoteroGroupId: 'e2e-legacy-group' },
        hash: 'ldfix1', _id: ObjectId('${fileHex}')
      }
    } });
  `)
  expect(fileHex, 'legacy-key zotero file id resolvable').toMatch(/^[0-9a-f]{24}$/)
  const refresh = await api(page, 'POST', `/project/${pid}/linked_file/${fileHex}/refresh`)
  expect(refresh.status(), 'refresh → pinned bare-400 contract (no provider agent in this stack)').toBe(400)

  // 4) REFRESH on a GHOST file id → 404 (the pinned ghost-entity leg)
  const ghost = await api(page, 'POST', `/project/${pid}/linked_file/${'f'.repeat(24)}/refresh`)
  expect(ghost.status(), 'ghost file refresh → 404').toBe(404)

  // 5) CLONE round-trips a legacy-key linkedFileData blob verbatim
  const clone = await api(page, 'POST', `/project/${pid}/clone`, { projectName: 'e2e-zotero-linkedfile-clone' })
  const clbody = (await clone.text().catch(() => '')).slice(0, 120)
  expect(clone.status(), `clone of the zotero-linked project (got ${clone.status()}: ${clbody})`).toBeLessThan(300)
  const clonePid = String(
    mongoEval(`'X' + JSON.stringify(db.projects.find({ name: /e2e-zotero-linkedfile/ }, { _id: 1 }).sort({ _id: -1 }).limit(1).toArray().map(p => p._id.toHexString()))`),
  ).replace(/^X/, '').replace(/\[|\]/g, '').split(',').map(s => s.trim().replace(/"/g, ''))
  const cloneId = clonePid.slice(-1)[0]
  expect(cloneId, 'clone project id').toMatch(/^[0-9a-f]{24}$/)
  const cloneRefsRaw = String(
    mongoEval(`'X' + JSON.stringify(db.projects.findOne({ _id: ObjectId('${cloneId}') }).rootFolder[0].fileRefs)`),
  ).replace(/^X/, '')
  const cloneRefs: any[] = JSON.parse(cloneRefsRaw)
  const cloneLf = (Array.isArray(cloneRefs) ? cloneRefs : []).find(f => f && (f.name || '').includes('refs-legacy.bib'))
  expect(cloneLf, 'linked file copied into the clone').toBeTruthy()
  const cstored = cloneLf.linkedFileData || {}
  expect(cstored.bibFormat, 'clone blob: legacy key bibFormat intact').toBe('bibtex')
  expect(cstored.zoteroGroupId, 'clone blob: legacy key zoteroGroupId intact').toBe('e2e-legacy-group')
})

