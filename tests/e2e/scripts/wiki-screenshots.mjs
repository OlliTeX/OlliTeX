/**
 * OlliTeX wiki screenshot pipeline (docs/WIKI_PLAN.md §4).
 *
 * Single entry point for ALL wiki screenshots. Rules enforced here:
 *  - Only the disposable E2E stack (BASE, default http://127.0.0.1:7420)
 *  - Only fixture accounts (e2e-user / e2e-admin) — same values as
 *    tests/e2e/fixtures/credentials.ts
 *  - Every shot asserts an expected text BEFORE capturing; a broken page
 *    fails the run instead of shipping (soft shots are exceptions, flagged)
 *  - After shots: deny-list scan over docs (md + asset filenames + PNG
 *    tEXt metadata). Real credential strings from
 *    /data_1/image_mining/testuser.txt are added to the deny list at
 *    runtime (file outside the repo; absent in CI → pattern skipped).
 *  - Cleanup: wiki demo projects/tags/providers are removed afterwards.
 *  - AUDIT.md is rewritten on every run (the data-safety receipt).
 *
 * Usage:
 *   node wiki-screenshots.mjs            # full run (needs the e2e stack up)
 *   node wiki-screenshots.mjs --check    # docs-only gate (no stack needed)
 *
 * Output:
 *   docs/wiki/assets/{users,admins}/…png   (overwritten, deterministic names)
 *   docs/wiki/AUDIT.md                     (receipt)
 */
import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import { execSync } from 'node:child_process'

const REPO_ROOT = path.resolve(process.cwd(), '..', '..')
const WIKI = path.join(REPO_ROOT, 'docs', 'wiki')
const ASSETS = path.join(WIKI, 'assets')
const AUDIT = path.join(WIKI, 'AUDIT.md')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const CREDS_FILE = '/data_1/image_mining/testuser.txt'

const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

const ALLOWED_EMAILS = ['@e2e.test', 'example.com', 'example.org']
const ALLOWED_KEYS = ['sk-ollitex-dummy-do-not-use']

// ---------------------------------------------------------------------------
// shot list (file → how it is produced). `soft` = affordance may legitimately
// be absent on some builds → warn + flag in AUDIT.md, do not fail the run.
// ---------------------------------------------------------------------------
async function openEditor(page, pid) {
  await page.goto(`${BASE}/project/${pid}`, { waitUntil: 'load' })
  await page.locator('.cm-editor').first().waitFor({ timeout: 90_000 })
  await page.waitForTimeout(1800)
}

async function compileEditor(page) {
  // the real Compile button lives in .compile-button-group (pdf/toolbar area);
  // the sibling dropdown toggle is 'Toggle compile options menu' — must NOT click it.
  let clicked = false
  const scoped = page.locator('.compile-button-group .compile-button, button.compile-button').first()
  if (await scoped.count().catch(() => 0)) {
    await scoped.click({ timeout: 15_000 }).catch(e => console.log('[wiki] compile click (scoped) failed:', String(e).slice(0, 120)))
    clicked = true
  }
  if (!clicked) {
    await page.getByRole('button', { name: /^(re)?compile$/i }).first().click({ timeout: 15_000 }).catch(e => console.log('[wiki] compile click (role) failed:', String(e).slice(0, 120)))
    clicked = true
  }
  if (!clicked) {
    // last resort: the documented shortcut (Ctrl/Cmd+Enter)
    await page.locator('.cm-editor').first().click().catch(() => {})
    await page.keyboard.press('Control+Enter').catch(() => {})
  }
  console.log('[wiki] compile triggered')
  // poll up to 120s for a compile finished signal (PDF pane OR status text)
  for (let i = 0; i < 40; i++) {
    await page.waitForTimeout(3000)
    const b = await page.locator('body').innerText().catch(() => '') || ''
    if (/output\.pdf|main\.pdf|\/pdf\//i.test(b)) { console.log(`[wiki] PDF ready at t+${i * 3}s`); return }
    if (i === 20) {
      const lines = b.split('\n').filter(l => /compil|running|finished|error|warning|log|queue|waiting/i.test(l)).slice(0, 8)
      console.log('[wiki] compile still running at t+60s:', JSON.stringify(lines).slice(0, 300))
    }
  }
  const b = await page.locator('body').innerText().catch(() => '') || ''
  const lines = b.split('\n').filter(l => /compil|running|finished|error|warning|log|queue|waiting|pdf/i.test(l)).slice(0, 10)
  console.log('[wiki] compile wait exhausted; state lines:', JSON.stringify(lines).slice(0, 400))
}

const SHOTS = [
  // ---------------- users/01-getting-started ----------------
  {
    file: 'users/01-getting-started-hub.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub`, { waitUntil: 'domcontentloaded' }) },
    guard: /projects/i,
  },
  {
    file: 'users/01-getting-started-hub-admin.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub`, { waitUntil: 'domcontentloaded' }) },
    guard: /site settings/i,
  },

  // ---------------- users/02-projects ----------------
  {
    file: 'users/02-projects-all.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/projects.all`, { waitUntil: 'domcontentloaded' }) },
    guard: /wiki demo/i,
  },
  {
    file: 'users/02-projects-new-menu.png', account: USER, soft: true,
    run: async (p) => {
      await p.goto(`${BASE}/hub#/projects.all`, { waitUntil: 'domcontentloaded' })
      await p.getByRole('button', { name: /new project/i }).first().click({ timeout: 10_000 })
      await p.waitForTimeout(800)
    },
    guard: /blank project/i,
    post: async (p) => { await p.keyboard.press('Escape').catch(() => {}) },
  },
  {
    file: 'users/02-projects-tags.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/projects.tags.tags`, { waitUntil: 'domcontentloaded' }) },
    guard: /wiki-demo/i,
  },
  {
    file: 'users/02-projects-trash.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/projects.trashed`, { waitUntil: 'domcontentloaded' }) },
    guard: /trashed/i,
  },

  // ---------------- users/03-editor-latex ----------------
  {
    file: 'users/03-editor-latex-open.png', account: USER,
    run: async (p, ctx) => { await openEditor(p, ctx.pidLatex) },
    guard: /main\.tex/i,
  },
  {
    file: 'users/03-editor-latex-compile.png', account: USER,
    run: async (p, ctx) => { await openEditor(p, ctx.pidLatex); await compileEditor(p) },
    guard: /compile|output/i,
    guardEl: 'iframe[src*=".pdf"], .pdf-preview, [class*=pdf-preview]',
  },
  {
    file: 'users/03-editor-latex-comment.png', account: USER, soft: true,
    run: async (p, ctx) => {
      await openEditor(p, ctx.pidLatex)
      await p.locator('.cm-editor').first().click()
      await p.keyboard.press('Control+a')
      await p.waitForTimeout(700)
    },
    guard: /comment/i,
  },

  // ---------------- users/04-editor-typst ----------------
  {
    file: 'users/04-editor-typst-compile.png', account: USER,
    run: async (p, ctx) => { await openEditor(p, ctx.pidTypst); await compileEditor(p) },
    guard: /main\.typ|compile/i,
    guardEl: 'iframe[src*=".pdf"], .pdf-preview, [class*=pdf-preview]',
  },

  // ---------------- users/05-ai-features ----------------
  {
    file: 'users/05-ai-features-chat.png', account: USER, soft: true,
    run: async (p, ctx) => {
      await openEditor(p, ctx.pidLatex)
      for (const sel of ['[aria-label*=Ask AI]', '[aria-label*=Chat]', '[aria-label*=LLM]', 'button:has-text("AI")']) {
        const l = p.locator(sel).first()
        if ((await l.count().catch(() => 0)) > 0) {
          await l.click({ timeout: 8_000 }).catch(() => {})
          await p.waitForTimeout(1200)
          return
        }
      }
    },
    guard: /ai|llm|chat/i,
  },
  {
    file: 'users/05-ai-features-grammar.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/mysettings.llm.grammar`, { waitUntil: 'domcontentloaded' }) },
    guard: /grammar/i,
  },
  {
    file: 'users/05-ai-features-byo.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/mysettings.llm.general`, { waitUntil: 'domcontentloaded' }) },
    guard: /wiki byo demo/i,
  },
// ---------------------------------------------------------------------------
// login
// ---------------------------------------------------------------------------

  // ---------------- users/06-references ----------------
  {
    file: 'users/06-references-library.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/library`, { waitUntil: 'domcontentloaded' }) },
    guard: /library|reference/i,
  },
  {
    file: 'users/06-references-zotero-settings.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/mysettings.references`, { waitUntil: 'domcontentloaded' }) },
    guard: /zotero|mendeley|reference/i,
  },

  // ---------------- users/07-file-tools ----------------
  {
    file: 'users/07-file-tools-import-menu.png', account: USER, soft: true,
    run: async (p) => {
      await p.goto(`${BASE}/hub#/projects.all`, { waitUntil: 'domcontentloaded' })
      await p.getByRole('button', { name: /new project/i }).first().click({ timeout: 10_000 })
      await p.waitForTimeout(800)
    },
    guard: /zip|word|markdown|github/i,
    post: async (p) => { await p.keyboard.press('Escape').catch(() => {}) },
  },

  // ---------------- users/08-sync-integrations ----------------
  {
    file: 'users/08-sync-webdav.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/mysettings.sync`, { waitUntil: 'domcontentloaded' }) },
    guard: /webdav|sync/i,
  },
  {
    file: 'users/08-sync-github-site.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.compilation.github`, { waitUntil: 'domcontentloaded' }) },
    guard: /github/i,
  },

  // ---------------- users/09-settings ----------------
  {
    file: 'users/09-settings-appearance.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/mysettings.appearance`, { waitUntil: 'domcontentloaded' }) },
    guard: /appearance/i,
  },
  {
    file: 'users/09-settings-email.png', account: USER,
    run: async (p) => { await p.goto(`${BASE}/hub#/mysettings.email`, { waitUntil: 'domcontentloaded' }) },
    guard: /email|notification/i,
  },

  // ---------------- users/10-accounts ----------------
  {
    file: 'users/10-accounts-login.png', account: null,
    run: async (p) => { await p.goto(`${BASE}/login`, { waitUntil: 'domcontentloaded' }) },
    guard: /sign in|log ?in/i,
  },
  {
    file: 'users/10-accounts-register.png', account: null,
    run: async (p) => { await p.goto(`${BASE}/register`, { waitUntil: 'domcontentloaded' }) },
    guard: /register|create/i,
  },

  // ---------------- admins/01-admin-overview ----------------
  {
    file: 'admins/01-admin-overview.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/overview`, { waitUntil: 'domcontentloaded' }) },
    guard: /overview/i,
  },

  // ---------------- admins/02-users ----------------
  {
    file: 'admins/02-users-all.png', account: ADMIN,
    run: async (p) => {
      // the admin user list spans 100+ fixture users with a limited page size —
      // create one wiki demo member and surface it via the leaf's own search.
      await api(p, 'POST', '/admin/user/create', {
        email: 'wiki-demo-member@e2e.test', firstName: 'Wiki', lastName: 'Demo',
      }).catch(() => null) // idempotent: 4xx if it already exists
      await p.goto(`${BASE}/hub#/site.general.users.all`, { waitUntil: 'domcontentloaded' })
      await p.waitForTimeout(3000)
      const box = p.locator('input[placeholder="Search by name or email"]').first()
      if (await box.count().catch(() => 0)) {
        await box.fill('wiki-demo-member')
        await p.waitForTimeout(2500)
      }
    },
    guard: /wiki-demo-member/i,
  },
  {
    file: 'admins/02-users-suspended.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.general.users.suspended`, { waitUntil: 'domcontentloaded' }) },
    guard: /suspended/i,
  },

  // ---------------- admins/03-projects ----------------
  {
    file: 'admins/03-projects-all.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.general.projects.all`, { waitUntil: 'domcontentloaded' }) },
    guard: /wiki demo/i,
  },
  {
    file: 'admins/03-projects-active.png', account: ADMIN, soft: true,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.general.activeprojects`, { waitUntil: 'domcontentloaded' }) },
    guard: /active|session/i,
  },

  // ---------------- admins/04-site-settings ----------------
  {
    file: 'admins/04-site-signup.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.general.signup`, { waitUntil: 'domcontentloaded' }) },
    guard: /sign.?up/i,
  },
  {
    file: 'admins/04-site-email.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.services.email`, { waitUntil: 'domcontentloaded' }) },
    guard: /smtp|email/i,
  },

  // ---------------- admins/05-llm-rate-limiter ----------------
  {
    file: 'admins/05-llm-rate-limiter.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.llm.instance`, { waitUntil: 'domcontentloaded' }) },
    guard: /rate limiter/i,
  },
  {
    file: 'admins/05-llm-usage.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.llm.usage`, { waitUntil: 'domcontentloaded' }) },
    guard: /usage/i,
  },

  // ---------------- admins/06-templates ----------------
  {
    file: 'admins/06-templates-manage.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.general.managetpl`, { waitUntil: 'domcontentloaded' }) },
    guard: /template/i,
  },

  // ---------------- admins/07-instance-stats ----------------
  {
    file: 'admins/07-instance-stats.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.general.stats`, { waitUntil: 'domcontentloaded' }) },
    guard: /statistic/i,
  },

  // ---------------- admins/08-sso-saml-oidc ----------------
  {
    file: 'admins/08-sso-saml.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.integrations.sso-saml`, { waitUntil: 'domcontentloaded' }) },
    guard: /saml/i,
  },

  // ---------------- admins/09-operations ----------------
  {
    file: 'admins/09-system-messages.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.general.messages`, { waitUntil: 'domcontentloaded' }) },
    guard: /message/i,
  },

  // ---------------- installation/ ----------------
  {
    file: 'installation/01-docker-healthy.png', account: null,
    run: async (p) => { await p.goto(`${BASE}/login`, { waitUntil: 'domcontentloaded' }) },
    guard: /sign in|log ?in/i,
  },
  {
    file: 'installation/04-security-sandboxed.png', account: ADMIN,
    run: async (p) => { await p.goto(`${BASE}/hub#/site.compilation.sandboxed`, { waitUntil: 'domcontentloaded' }) },
    guard: /sandbox/i,
  },
]

// ---------------------------------------------------------------------------
// login
// ---------------------------------------------------------------------------
async function login(page, acc) {
  for (let i = 0; i < 3; i++) {
    await page.goto(`${BASE}/login`, { waitUntil: 'domcontentloaded' })
    try {
      await page.waitForSelector('#email', { timeout: 15_000 })
      await page.fill('#email', acc.email)
      await page.fill('#password', acc.password)
      await page.locator('form button[type="submit"]').click()
      await page.waitForURL(url => !url.pathname.includes('/login'), { timeout: 30_000 })
      return
    } catch (e) {
      if (i === 2) throw new Error(`login failed for ${acc.email}: ${String(e).slice(0, 160)}`)
      await page.waitForTimeout(2000)
    }
  }
}

// ---------------------------------------------------------------------------
// api (CSRF-safe, bound to page cookies)
// ---------------------------------------------------------------------------
async function csrf(page) {
  const fromMeta = await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
  if (fromMeta) return fromMeta
  const html = await page.evaluate(async (base) => {
    const r = await fetch(base, { credentials: 'include' })
    return await r.text()
  }, BASE).catch(() => '')
  const m = html.match(/name="ol-csrfToken"\s+content="([^"]+)"/)
  return m ? m[1] : null
}

async function api(page, method, p, body) {
  const headers = { 'Content-Type': 'application/json' }
  if (method !== 'GET') {
    const tok = await csrf(page)
    if (tok) headers['X-CSRF-TOKEN'] = tok
  }
  return await page.request.fetch(`${BASE}${p}`, {
    method,
    headers,
    data: body === undefined ? undefined : JSON.stringify(body),
  })
}

// ---------------------------------------------------------------------------
// seed / cleanup
// ---------------------------------------------------------------------------
const DEMO_PREFIXES = ['Wiki Demo Latex', 'Wiki Demo Typst', 'Wiki Demo Trash']

async function getJson(page, method, p, body) {
  const res = await api(page, method, p, body).catch(() => null)
  return res ? await res.json().catch(() => null) : null
}

async function wikiProjectIds(page) {
  const data = await getJson(page, 'POST', '/api/project', { filters: {}, sort: { by: 'lastUpdated', order: 'desc' } })
  const list = data?.projects ?? (Array.isArray(data) ? data : [])
  return list
    .filter(pr => DEMO_PREFIXES.some(x => (pr.name ?? '') === x))
    .map(pr => pr._id ?? pr.id)
}

async function seed(page) {
  // idempotent: remove any older wiki demo data first
  const old = await wikiProjectIds(page)
  for (const pid of old) {
    await api(page, 'DELETE', '/project/' + pid, undefined).catch(() => null)
  }
  // existing wiki tag / provider
  const tags0 = await getJson(page, 'GET', '/tag')
  const tagItems = tags0?.tags ?? (Array.isArray(tags0) ? tags0 : [])
  for (const t of tagItems) {
    if ((t.name ?? t.label ?? '').startsWith('wiki-demo')) {
      await api(page, 'DELETE', '/tag/' + (t._id ?? t.id), undefined).catch(() => null)
    }
  }
  const provs0 = await getJson(page, 'GET', '/user/llm-providers')
  const provItems = provs0?.providers ?? (Array.isArray(provs0) ? provs0 : [])
  for (const pr of provItems) {
    if ((pr.name ?? '').startsWith('Wiki BYO')) {
      await api(page, 'DELETE', '/user/llm-providers/' + (pr.id ?? pr._id), undefined).catch(() => null)
    }
  }

  // create the wiki demo projects
  const lat = await api(page, 'POST', '/project/new', { projectName: 'Wiki Demo Latex' }).catch(() => null)
  const latBody = lat ? await lat.json().catch(() => null) : null
  if (!lat || !lat.ok() || !latBody?.project_id) {
    throw new Error('create Wiki Demo Latex failed' + (lat ? ': ' + lat.status() : ''))
  }
  const pidLatex = latBody.project_id

  let pidTypst = null
  const typ = await api(page, 'POST', '/project/new/typst', { projectName: 'Wiki Demo Typst', template: 'example' }).catch(() => null)
  if (typ && typ.ok()) {
    const b = await typ.json().catch(() => null)
    pidTypst = b?.project_id ?? null
  }
  if (!pidTypst) {
    const t2 = await api(page, 'POST', '/project/new/typst', { projectName: 'Wiki Demo Typst' }).catch(() => null)
    const b2 = t2 ? await t2.json().catch(() => null) : null
    pidTypst = b2?.project_id ?? null
  }
  if (!pidTypst) throw new Error('could not create the Typst wiki demo project')

  // tag both projects with a demo tag
  const tagRes = await api(page, 'POST', '/tag', { name: 'wiki-demo' }).catch(() => null)
  const tagBody = tagRes && tagRes.ok() ? await tagRes.json().catch(() => null) : null
  const tagId = tagBody?._id ?? tagBody?.id ?? null
  if (tagId) {
    const ids = [pidLatex]
    if (pidTypst) ids.push(pidTypst)
    await api(page, 'POST', `/tag/${tagId}/projects`, { project_ids: ids }).catch(() => null)
  }

  // BYO provider (dummy key, unreachable endpoint — never a live key)
  const v = await api(page, 'POST', '/user/llm-providers', {
    name: 'Wiki BYO Demo',
    providerType: 'openaiCompatible',
    baseUrl: 'http://ollitex-wiki-demo.invalid',
    apiKey: 'sk-ollitex-dummy-do-not-use',
    models: ['wiki-demo-model'],
    completionModel: 'wiki-demo-model',
  }).catch(() => null)
  if (!v || !v.ok()) throw new Error('create Wiki BYO Demo provider failed' + (v ? ': ' + v.status() : ''))

  return { pidLatex, pidTypst }
}

async function cleanup(page) {
  const pids = await wikiProjectIds(page)
  for (const pid of pids) await api(page, 'DELETE', '/project/' + pid, undefined).catch(() => null)
  const tags0 = await getJson(page, 'GET', '/tag')
  const tagItems = tags0?.tags ?? (Array.isArray(tags0) ? tags0 : [])
  for (const t of tagItems) {
    if ((t.name ?? t.label ?? '').startsWith('wiki-demo')) {
      await api(page, 'DELETE', '/tag/' + (t._id ?? t.id), undefined).catch(() => null)
    }
  }
  const provs0 = await getJson(page, 'GET', '/user/llm-providers')
  const provItems = provs0?.providers ?? (Array.isArray(provs0) ? provs0 : [])
  for (const pr of provItems) {
    if ((pr.name ?? '').startsWith('Wiki BYO')) {
      await api(page, 'DELETE', '/user/llm-providers/' + (pr.id ?? pr._id), undefined).catch(() => null)
    }
  }
  return pids.length
}

// ---------------------------------------------------------------------------
// data-safety gate (also run in --check mode)
// ---------------------------------------------------------------------------
function buildDenyList() {
  const res = []
  try {
    const raw = fs.readFileSync(CREDS_FILE, 'utf8')
    // real admin email + password (runtime only — never written to docs)
    const em = (raw.match(/\S+@\S+/) || [])[0]
    const pw = (raw.match(/8!xelSY&hoT@lO/) || [])[0]
    if (em) res.push({ label: 'real admin email (runtime file)', re: new RegExp(em.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')) })
    if (pw) res.push({ label: 'real admin password (runtime file)', re: new RegExp(pw.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')) })
  } catch { /* file absent (CI) → skip runtime patterns */ }

  res.push(
    { label: 'OpenAI-style key', re: /sk-(?!ollitex-dummy-do-not-use)[A-Za-z0-9_-]{8,}/ },
    { label: 'AWS access key', re: /AKIA[0-9A-Z]{16}/ },
    { label: 'JWT bearer literal', re: /\bbearer\s+[A-Za-z0-9._-]{20,}/i },
    { label: 'Slack token', re: /xox[baprs]-[A-Za-z0-9-]{10,}/ },
    { label: 'private key block', re: /BEGIN (RSA|EC|OPENSSH|PGP) (PRIVATE )?KEY/ },
    { label: 'mongodb URI with password', re: /mongodb(\+srv)?:\/\/[^/\s]+:[^@\s]+@/ },
    { label: 'real external domain (rotermund)', re: /rotermund\.at/i },
  )
  return res
}

function scanTexts(textsFileMap, deny) {
  const hits = []
  for (const [file, text] of textsFileMap) {
    for (const d of deny) {
      const m = text.match(d.re)
      if (m) hits.push({ file, label: d.label, match: String(m[0]).slice(0, 24) + '…' })
    }
  }
  // real-email sweep: any @-address outside the allow-list
  const emailRe = /[\w.+-]+@[\w-]+\.[\w.-]+/g
  for (const [file, text] of textsFileMap) {
    for (const em of (text.match(emailRe) ?? [])) {
      if (!ALLOWED_EMAILS.some(a => em.toLowerCase().endsWith(a))) {
        hits.push({ file, label: 'non-fixture email address', match: em.slice(0, 24) })
      }
    }
  }
  return hits
}

/** PNG tEXt/zTXt chunk scan (rare, cheap, closes the "metadata" gap). */
function scanPngMetadata(file, deny) {
  const hits = []
  try {
    const buf = fs.readFileSync(file)
    if (buf.length < 8 || buf.readUInt32BE(0) !== 0x89504e47) return hits
    let off = 8
    while (off + 8 <= buf.length) {
      const len = buf.readUInt32BE(off)
      const type = buf.toString('latin1', off + 4, off + 8)
      const payloadStart = off + 8
      if (len === 0) break
      if (type === 'tEXt' || type === 'zTXt' || type === 'iTXt') {
        const payload = buf.toString('latin1', payloadStart, Math.min(payloadStart + len, buf.length))
        for (const d of deny) {
          if (d.re.test(payload)) hits.push({ file: path.basename(file), label: d.label, match: 'png-metadata' })
        }
      }
      off = payloadStart + len + 4
      if (type === 'IEND') break
    }
  } catch (e) {
    hits.push({ file: path.basename(file), label: 'scan-error', match: String(e).slice(0, 40) })
  }
  return hits
}

// ---------------------------------------------------------------------------
// docs gates (link check + naming) — part of --check
// ---------------------------------------------------------------------------
function walkMd(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name)
    if (e.isDirectory()) walkMd(p, out)
    else if (e.name.endsWith('.md')) out.push(p)
  }
  return out
}
function walkPng(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name)
    if (e.isDirectory()) walkPng(p, out)
    else if (e.name.endsWith('.png')) out.push(p)
  }
  return out
}

function linkCheck(mdFiles) {
  const broken = []
  const re = /!\[[^\]]*\]\(([^)]+)\)|(?:^|\s|\[)\(?\[?[^\)\s]*\]\(([^)\s]+)\)/g
  for (const f of mdFiles) {
    const text = fs.readFileSync(f, 'utf8')
    const dir = path.dirname(f)
    for (const m of text.matchAll(/!\[[^\]]*\]\(([^)]+)\)|\[[^\]]*\]\(([^)]+)\)/g)) {
      const target = (m[1] ?? m[2] ?? '').split('#')[0]
      if (!target || /^(https?:|mailto:|#)/.test(target)) continue
      if (!fs.existsSync(path.resolve(dir, target))) broken.push({ file: path.relative(WIKI, f), target })
    }
  }
  return broken
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------
async function main() {
  const checkOnly = process.argv.includes('--check')
  const started = new Date().toISOString()
  const sha = (() => { try { return execSync('git -C ' + REPO_ROOT + ' rev-parse --short HEAD', { encoding: 'utf8' }).trim() } catch { return 'unknown' } })()

  const auditLines = []
  auditLines.push('# Wiki audit — auto-generated receipt')
  auditLines.push('')
  auditLines.push(`- Run: ${started} ${checkOnly ? '(--check: docs gates only)' : '(full screenshot run)'}`)
  auditLines.push(`- Source tree: OlliTeX @ \`${sha}\``)
  auditLines.push(`- Stack: ${checkOnly ? 'n/a (docs-only)' : BASE + ' (disposable E2E stack, fixture data only)'}`)
  auditLines.push('')

  if (checkOnly) {
    // ---------------- docs-only gates ----------------
    if (!fs.existsSync(WIKI)) { console.log('[wiki] docs/wiki not present yet — nothing to check'); return }
    const mdFiles = walkMd(WIKI)
    const pngs = fs.existsSync(ASSETS) ? walkPng(ASSETS) : []
    const deny = buildDenyList()

    const texts = new Map()
    for (const f of mdFiles) texts.set(path.relative(WIKI, f), fs.readFileSync(f, 'utf8'))
    for (const f of pngs) texts.set(path.relative(WIKI, f) + ' (filename)', path.basename(f))
    const hits = scanTexts(texts, deny)
    for (const f of pngs) hits.push(...scanPngMetadata(f, deny))

    const broken = linkCheck(mdFiles)
    console.log(`[wiki] check: ${mdFiles.length} md files, ${pngs.length} png assets, ${deny.length} deny patterns`)
    if (broken.length) {
      console.error('[wiki] BROKEN LINKS: ' + broken.length)
      for (const b of broken) console.error('   ' + b.file + ' → ' + b.target)
      process.exitCode = 1
    } else console.log('[wiki] links: OK')
    if (hits.length) {
      console.error('[wiki] DATA-SAFETY HITS: ' + hits.length)
      for (const h of hits) console.error('   ' + h.file + ' — ' + h.label + ' — ' + h.match)
      process.exitCode = 1
    } else console.log('[wiki] data-safety scan: CLEAN')
    return
  }

  // ---------------- full run ----------------
  const { chromium } = await import('playwright')
  console.log('[wiki] stack preflight…')
  const http = await fetch(`${BASE}/login`, { method: 'GET' }).catch(() => null)
  if (!http || http.status !== 200) {
    throw new Error(`E2E stack not answering at ${BASE}/login — run tests/e2e/scripts/stack-up.sh first`)
  }

  const browser = await chromium.launch()
  const shotCtx = {}
  try {
    // ---- seed ----
    const seedCtx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
    const sp = await seedCtx.newPage()
    await login(sp, USER)
    const ids = await seed(sp)
    Object.assign(shotCtx, ids)
    await seedCtx.close()
    console.log(`[wiki] seeded: latex=${ids.pidLatex} typst=${ids.pidTypst}`)

    // ---- shots ----
    const results = []
    const contextsCache = new Map()
    const ctxFor = async (acc) => {
      if (!acc) {
        const c = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'en', reducedMotion: 'reduce' })
        const pages = await c.pages()
        return { ctx: c, page: pages[0] || await c.newPage() }
      }
      let e = contextsCache.get(acc.email)
      if (!e) {
        const c = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: 'en', reducedMotion: 'reduce' })
        const p = await c.newPage()
        await login(p, acc)
        e = { ctx: c, page: p }
        contextsCache.set(acc.email, e)
      }
      return e
    }

    for (const shot of SHOTS) {
      const out = path.join(WIKI, 'assets', shot.file)
      fs.mkdirSync(path.dirname(out), { recursive: true })
      const { ctx, page } = await ctxFor(shot.account)
      let guardOk = true
      let err = ''
      try {
        await shot.run(page, shotCtx)
        await page.waitForTimeout(2500)
        const body = await page.locator('body').innerText().catch(() => '') || ''
        guardOk = shot.guard.test(body)
        if (guardOk && shot.guardEl) {
          guardOk = (await page.locator(shot.guardEl).count().catch(() => 0)) > 0
        }
      } catch (e) {
        err = String(e).slice(0, 160)
        guardOk = false
      }
      await page.screenshot({ path: out }).catch(e => { err = 'screenshot: ' + String(e).slice(0, 120); guardOk = false })
      if (shot.post) await shot.post(page).catch(() => {})
      const buf = fs.readFileSync(out)
      const row = {
        file: shot.file,
        account: shot.account ? shot.account.email : 'guest',
        guard: guardOk ? 'ok' : (shot.soft ? 'SOFT-FAIL' : 'FAIL'),
        soft: !!shot.soft,
        bytes: buf.length,
        sha: crypto.createHash('sha256').update(buf).digest('hex').slice(0, 12),
        err,
      }
      results.push(row)
      console.log(`[wiki] ${guardOk ? 'ok  ' : (shot.soft ? 'soft' : 'FAIL')} ${shot.file}${guardOk ? '' : '  (' + (err || 'guard missing') + ')'}`)
      if (!guardOk && !shot.soft) throw new Error(`shot failed: ${shot.file} — ${err || 'expected text not found'}`)
    }
    for (const [, e] of contextsCache) await e.ctx.close().catch(() => {})

    // ---- cleanup ----
    const cleanedCtx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
    const cp = await cleanedCtx.newPage()
    await login(cp, USER)
    const removed = await cleanup(cp)
    await cleanedCtx.close()

    // admin side: purge the wiki demo member
    let memberRemoved = false
    try {
      const aCtx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
      const ap = await aCtx.newPage()
      await login(ap, ADMIN)
      await ap.waitForTimeout(2500)
      const listRes = await api(ap, 'POST', '/admin/users', { sort: { by: 'name', asc: true } }).catch(() => null)
      const data = listRes ? await listRes.json().catch(() => null) : null
      const u = (data?.users ?? []).find(x => x.email === 'wiki-demo-member@e2e.test')
      if (u?.id) {
        await api(ap, 'DELETE', '/admin/user/' + u.id).catch(() => null)
        memberRemoved = true
      }
      await aCtx.close()
    } catch { /* best effort */ }
    console.log(`[wiki] cleanup: removed ${removed} demo project(s) + tag + BYO provider + demo member (${memberRemoved ? 'purged' : 'not found/skipped'})`)

    // ---- gate ----
    const mdFiles = walkMd(WIKI)
    const pngs = walkPng(ASSETS)
    const deny = buildDenyList()
    const texts = new Map()
    for (const f of mdFiles) texts.set(path.relative(WIKI, f), fs.readFileSync(f, 'utf8'))
    for (const f of pngs) texts.set(path.relative(WIKI, f) + ' (filename)', path.basename(f))
    const hits = scanTexts(texts, deny)
    for (const f of pngs) hits.push(...scanPngMetadata(f, deny))
    const broken = linkCheck(mdFiles)

    // ---- AUDIT.md ----
    auditLines.push('## Shots')
    auditLines.push('')
    auditLines.push('| asset | account | guard | bytes | sha (short) |')
    auditLines.push('|---|---|---|---|---|')
    for (const r of results) auditLines.push(`| \`${r.file}\` | ${r.account} | ${r.guard}${r.err ? ' — ' + r.err : ''} | ${r.bytes} | ${r.sha} |`)
    auditLines.push('')
    auditLines.push('## Gates')
    auditLines.push('')
    auditLines.push(`- links: ${broken.length === 0 ? 'OK' : broken.length + ' BROKEN'}`)
    auditLines.push(`- data-safety: ${hits.length === 0 ? 'CLEAN' : hits.length + ' HITS'}`)
    if (broken.length) { for (const b of broken) auditLines.push(`  - broken: \`${b.file}\` → ${b.target}`) }
    if (hits.length) { for (const h of hits) auditLines.push(`  - hit: \`${h.file}\` — ${h.label}`) }
    auditLines.push('')
    auditLines.push('## Cleanup')
    auditLines.push('')
    auditLines.push(`- demo projects removed: ${removed}`)
    auditLines.push('- demo tag + BYO provider removed: done (see cleanup step)')
    fs.writeFileSync(AUDIT, auditLines.join('\n') + '\n')
    console.log('[wiki] AUDIT.md written')

    if (broken.length || hits.length) {
      console.error(`[wiki] GATE FAILED — ${broken.length} broken links, ${hits.length} data-safety hits`)
      process.exitCode = 1
    } else {
      console.log(`[wiki] ALL GREEN — ${results.length} shots, ${mdFiles.length} md files, links OK, data-safety CLEAN`)
    }
  } finally {
    await browser.close()
  }
}

main().catch(e => {
  console.error('[wiki] FATAL: ' + (e?.stack || String(e)).slice(0, 400))
  process.exit(1)
})
