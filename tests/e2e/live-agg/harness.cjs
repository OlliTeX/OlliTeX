/**
 * OlliTeX live e2e harness (AG owner suite, 2026-10-07).
 * Runs against the production base URL with the dedicated ag-e2e3 test user
 * (NEVER the owner's session — the owner's account is OIDC-only and the
 * owner's live project must stay untouched). Identity bootstrap (register
 * -> password-token -> set password -> login) is reproducible via
 * bootstrap.sh; the harness itself just logs in.
 *
 * Deterministic fixtures: 'agg-tex-fixture' (example template: main.tex +
 * sample.bib + frog.jpg) and 'agg-typst-fixture' owned by the test user.
 *
 * Usage:
 *   NODE_PATH=/root/.nvm/versions/node/v22.21.1/lib/node_modules \
 *   PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright \
 *   node agg<#>-<name>.cjs
 *
 * Matrix: every test appends {module, test, pass, detail} via harness.
 */
// Playwright: the repo's tests/e2e/node_modules (yarn-berry virtual store)
// resolves FIRST for anything run from under tests/e2e/ and its
// playwright-core bootstrap dies on modern Node ("require(...) is not a
// function"). Pin a known-good installation (global nvm tree) explicitly,
// falling back to the ordinary require.
let chromium
try {
  chromium = require('/root/.nvm/versions/node/v22.21.1/lib/node_modules/playwright').chromium
} catch (e) {
  chromium = require('playwright').chromium
}
const { execSync, execFileSync } = require('child_process')

const BASE = process.env.AGG_BASE || 'https://psintern.neuro.uni-bremen.de'
const TEST_EMAIL = process.env.AGG_EMAIL || 'ag-e2e3@ollitex.local'
const TEST_PASSWORD = process.env.AGG_PASSWORD || 'Agg-E2e-Pass-123'
const OWNER_PID = process.env.AGG_OWNER_PID || '6ac54f1acb0b784fbc32d3be' // informational only

function mongoEval(js) {
  try {
    // js must be single-quote-safe for the shell: pass it as an arg via ARGV
    // (never interpolate into a double-quoted shell string).
    const out = execFileSync('docker', [
      'exec', 'ollitex-mongo', 'mongosh', '--quiet',
      'mongodb://172.30.0.1:27017/ollitex', '--eval', js,
    ], { encoding: 'utf8', timeout: 20_000 })
    return out || ''
  } catch (e) {
    return ''
  }
}

// fixturePid — deterministic fixture by NAME (survives recreate).
function fixturePid(name) {
  const out = mongoEval(
    `const p = db.projects.findOne({name:"${name}"},{_id:1}); if (p) print(p._id.toString());`)
  return out.trim().split('\n').filter(x => /^[0-9a-f]{24}$/i.test(x)).pop() || ''
}

const matrix = []
function record(module, test, pass, detail = '') {
  matrix.push({ module, test, pass: !!pass, detail: String(detail).slice(0, 300) })
  console.log(`${pass ? 'PASS' : 'FAIL'}  [${module}] ${test}${detail ? ' — ' + String(detail).slice(0, 160) : ''}`)
}
function report(module) {
  const of = matrix.filter(m => m.module === module)
  const p = of.filter(m => m.pass).length
  console.log(`\n== MATRIX ${module}: ${p}/${of.length} passed ==`)
  of.filter(m => !m.pass).forEach(m => console.log(`   FAIL: ${m.test} — ${m.detail}`))
  return of
}

async function getContext() {
  const browser = await chromium.launch({ args: ['--no-sandbox'] })
  const ctx = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1500, height: 950 } })
  const boot = await ctx.newPage()
  // Real browser login with the ag-e2e3 test user (bootstrap.sh guarantees
  // the account + password exist; a missing session is the normal case).
  await boot.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  const emailInput = boot.locator('input[name="email"]').first()
  if (await emailInput.count()) {
    await emailInput.fill(TEST_EMAIL)
  } else {
    await boot.locator('input[type="email"]').first().fill(TEST_EMAIL)
  }
  await boot.locator('input[type="password"]').first().fill(TEST_PASSWORD)
  await boot.locator('button[type="submit"]').first().click()
  await boot.waitForFunction(() => !window.location.pathname.startsWith('/login'), { timeout: 25_000 })
    .catch(async () => {
      const u = boot.url()
      throw new Error(`login failed for ${TEST_EMAIL} (landed ${u}); run bootstrap.sh first`)
    })
  await boot.close()
  return { browser, ctx }
}

async function openEditor(ctx, pid) {
  const page = await ctx.newPage()
  const errors = { page: [], console: [] }
  page.on('pageerror', e => errors.page.push(e.message.slice(0, 200)))
  page.on('console', m => { if (m.type() === 'error') m.text().slice(0, 120) && errors.console.push(m.text().slice(0, 200)) })
  await page.goto(`${BASE}/editor/${pid}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().waitFor({ state: 'visible', timeout: 30_000 })
  await page.waitForTimeout(2500)
  return { page, errors }
}

async function newScratch(ctx, name) {
  const page = await ctx.newPage()
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  const csrf = await page.evaluate(() => {
    const m = document.querySelector('meta[name="ol-csrfToken"]')
    return m ? m.content : ''
  })
  const r = await ctx.request.post(BASE + '/project/new', {
    headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' },
    data: { projectName: name || `agg-${Date.now()}` },
  })
  const body = await r.json().catch(() => ({}))
  await page.close()
  return { status: r.status(), pid: body.project_id }
}

async function trash(ctx, pid) {
  const page = await ctx.newPage()
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  const csrf = await page.evaluate(() => {
    const m = document.querySelector('meta[name="ol-csrfToken"]')
    return m ? m.content : ''
  })
  const r = await ctx.request.post(`${BASE}/project/${pid}/trash`, {
    headers: { 'X-CSRF-Token': csrf },
    data: {},
  }).catch(e => ({ status: () => -1, err: e.message }))
  await page.close()
  return r.status ? r.status() : -1
}

function truthInsert(pid, needle) {
  try {
    const out = execSync(
      `docker exec ollitex-mongo mongosh --quiet ollitex --eval '
        const all = db.docs.find({ "project_id": ObjectId("${pid}") }).toArray();
        if (!all.length) { print("truthStart=-1"); process.exit(0); }
        const dd = all.sort((a,b) => b.version - a.version)[0];
        const t = dd.lines.join("\\n");
        const i = t.indexOf("${needle.replace(/'/g, "\\'")}");
        print("truthStart=" + i + " docLen=" + t.length);
        if (i >= 0) print("lineOfInsert=" + t.slice(0, i).split("\\n").length);
      '`, { encoding: 'utf8' })
    return out.trim()
  } catch (e) {
    return 'truth-error: ' + e.message.slice(0, 80)
  }
}


// ensureFile — upload a named file into the project via the pinned Go route
// POST /Project/:id/upload (multipart field 'qqfile' — pinned to Node oracle).
// rootFolderOf — root folder doc id for a project (upload folder_id,
// pinned Node oracle: absent/folder-not-found → 422 folder_not_found).
function rootFolderOf(pid) {
  // rootFolder is a 1-element array (Overleaf schema): [ {name, _id} ].
  const out = mongoEval(
    'const d = db.projects.findOne({_id: ObjectId("' + pid + '")});'
    + ' if (d && d.rootFolder && d.rootFolder[0]) print(d.rootFolder[0]._id.toString());')
  return out.trim().split('\n').filter(x => /^[0-9a-f]{24}$/i.test(x)).pop() || ''
}

async function ensureFile(ctx, pid, name, content, mime, csrf) {
  const buf = Buffer.from(content, 'utf8')
  const headers = csrf ? { 'x-csrf-token': csrf } : {}
  const folder = rootFolderOf(pid)
  const url = BASE + '/Project/' + pid + '/upload' + (folder ? '?folder_id=' + folder : '')
  // Go upload.go contract (pinned to Node oracle): ONE file part 'qqfile'
  // + form field 'name' + query folder_id (root folder doc id).
  const r = await ctx.request.post(url, {
    headers,
    multipart: {
      name: name,
      qqfile: { name: name, mimeType: mime || 'application/octet-stream', buffer: buf }
    }
  })
  if (r.status() !== 200) {
    const body = await r.text().catch(() => '')
    throw new Error('ensureFile ' + name + ' -> ' + r.status() + ' ' + body.slice(0, 160))
  }
  return true
}

async function csrfOf(page) {
  try {
    return await page.evaluate(() => {
      const m =
        document.querySelector('meta[name="ol-csrfToken"]') ||
        document.querySelector('[name="ol-csrfToken"]')
      return (m && (m.content || m.value)) || ''
    })
  } catch (e) {
    return ''
  }
}

module.exports = { BASE, OWNER_PID, TEST_EMAIL, TEST_PASSWORD, fixturePid, ensureFile, rootFolderOf, csrfOf, chromium, getContext, openEditor, newScratch, trash, truthInsert, record, report, matrix }
