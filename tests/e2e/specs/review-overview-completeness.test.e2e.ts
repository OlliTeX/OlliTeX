/**
 * review-overview-completeness — the review panel's Overview data source
 * is COMPLETE for multi-file projects: comments on a file that was never
 * opened "in this session" are listed with their doc + ranges, exactly
 * like a comment on the active file.
 *
 * TODO-04724e42 (upstream-impact wave 2026-10-11, CLOSED-IMMUNE): the
 * upstream bug — Overview tab only lists comments/changes for docs
 * CURRENTLY LOADED in document-updater (Redis), so comments on unopened
 * files are missing until the file is opened — CANNOT reproduce in this
 * fork: document-updater is RETIRED (D41, owner directive) and the
 * ranges/threads reads are docstore/mongo-direct (no loaded-only leg);
 * the comment store is server-side (threads wire), independent of any
 * client-side "open" state. This spec pins that completeness at the
 * data-wire level so a future "optimize ranges to a cache" cannot
 * silently regress Overview:
 *   - multi-file project (example template: main.tex + sample.bib);
 *   - comments on BOTH files (sample.bib never opened in the browser —
 *     the upstream "unloaded file" analog);
 *   - GET /project/:pid/threads lists BOTH with doc + ranges (complete);
 *   - GET /project/:pid/ranswers → the Overview wire answers well-formed
 *     (200 [{id, ranges?}]; no 500 class).
 *
 * Run alone: npx playwright test specs/review-overview-completeness.test.e2e.ts
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { api, mongoEval } from '../parity/harness'
import { USER } from '../fixtures/credentials'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

test.describe('review Overview completeness — multi-file, no loaded-only leg (04724e42 pin)', () => {
  let page: any
  let ctx: any
  let pid: string
  const stamp = Date.now().toString(36)
  const tidMain = 'e2et' + stamp + 'm1'
  const tidBib = 'e2et' + stamp + 'b1'
  const cidMain = 'e2em' + stamp + 'm1'
  const cidBib = 'e2em' + stamp + 'b1'

  test.beforeAll(async ({ browser }) => {
    ctx = await browser.newContext()
    page = await ctx.newPage()
    await loginRobust(page, USER.email, USER.password)
    // multi-file fixture: example template = main.tex + sample.bib (+ frog.jpg)
    const r = await api(page, 'POST', '/project/new', {
      projectName: 'e2e-review-overview-mf',
      template: 'example',
    })
    expect(r.status()).toBeLessThan(300)
    const j = await r.json().catch(() => ({}))
    pid = j?.project_id || j?._id
    expect(pid, 'project id').toMatch(/^[0-9a-f]{24}$/)

    // fixture sanity: the example template is multi-file (sample.bib
    // present in the root folder docs)
    const docsRaw = String(
      mongoEval(`'X' + JSON.stringify(db.projects.findOne({ _id: ObjectId('${pid}') }).rootFolder[0].docs.map(function(d){return d.name}))`),
    ).replace(/^X/, '')
    const names: string[] = JSON.parse(docsRaw)
    expect(names, 'example template ships sample.bib (multi-file fixture)').toContain('sample.bib')
  })

  test.afterAll(async () => {
    if (ctx) await ctx.close().catch(() => {})
  })

  test('comments on the never-opened file are listed with doc + ranges, same as the active file', async () => {
    // comments on BOTH files via the panel's own REST contract —
    // sample.bib is NEVER opened in any browser session (the upstream
    // "unloaded file" analog).
    const r1 = await api(page, 'POST', `/project/${pid}/thread/${tidMain}/messages`, {
      content: `overview comment on main ${stamp}`,
      id: cidMain,
      doc: 'main.tex',
      ranges: [{ pos: 0, len: 1 }],
    })
    expect([200, 201].includes(r1.status()), `main.tex comment create (got ${r1.status()})`).toBe(true)

    const r2 = await api(page, 'POST', `/project/${pid}/thread/${tidBib}/messages`, {
      content: `overview comment on sample ${stamp}`,
      id: cidBib,
      doc: 'sample.bib',
      ranges: [{ pos: 0, len: 1 }],
    })
    expect([200, 201].includes(r2.status()), `sample.bib comment create (got ${r2.status()})`).toBe(true)

    // the Overview data source (GET /threads) is COMPLETE: both comments
    // listed with their doc + ranges — nothing "loaded-only" filtered.
    const t = await api(page, 'GET', `/project/${pid}/threads`)
    expect(t.status(), 'threads wire 200').toBe(200)
    const rec: any = await t.json().catch(() => ({}))
    const m1 = (rec?.[tidMain]?.messages || []).find((m: any) => m?.id === cidMain)
    const b1 = (rec?.[tidBib]?.messages || []).find((m: any) => m?.id === cidBib)
    expect(m1, 'main.tex comment present').toBeTruthy()
    expect(b1, 'NEVER-OPENED sample.bib comment present (no loaded-only leg)').toBeTruthy()
    expect(String(m1.doc || rec[tidMain].doc || '').length, 'main message carries a doc anchor').toBeGreaterThan(0)
    expect(String(b1.doc || rec[tidBib].doc || '').length, 'bib message carries a doc anchor').toBeGreaterThan(0)
    expect(Array.isArray(b1.ranges) && (b1.ranges as any[]).length, 'bib comment carries its range (Overview can place it)').toBeGreaterThan(0)

    // the Overview wire (GET /project/:pid/ranges — use-project-ranges)
    // answers well-formed: 200 + array (the upstream 500/missing class is
    // absent even multi-file).
    const rw = await api(page, 'GET', `/project/${pid}/ranges`)
    expect(rw.status(), 'ranges wire 200 (no 500 class)').toBe(200)
    const rwj: any = await rw.json().catch(() => [])
    expect(Array.isArray(rwj), 'ranges wire is an array').toBe(true)
  })
})
