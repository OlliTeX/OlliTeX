/**
 * /hub parity — Phase 0 reference crawler.
 *
 * Crawls the 13 legacy pages as each role (site admin / template admin / user),
 * opens every NON-destructive control, follows modals recursively (depth <= 5 —
 * modals can be several layers deep), and freezes per-layer DOM + exact network
 * sequences into tests/e2e/parity/reference/<role>/<page>/.
 *
 * Safety: click blocklist (never fires Save/Submit/Delete/...); every click is
 * followed by a close (Escape) before the next branch. Mutating flows are
 * covered by the baseline suites via API contracts instead of live clicks.
 *
 * Usage:
 *   node parity/crawl/crawl.mjs [--page=/admin/user] [--role=admin] [--role=tpladmin] [--role=user]
 *   (defaults: all roles, all pages)
 */
import fs from 'node:fs'
import path from 'node:path'
import { createRequire } from 'node:module'
import { execSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const require2 = createRequire(new URL('package.json', new URL('.', import.meta.url)))
const { chromium } = require2('playwright')

const BASE = (process.env.OL_BASE || 'http://localhost:7420')
const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..') // tests/e2e
const REF = path.join(ROOT, 'parity', 'reference')
const DEPTH_CAP = Number(process.env.CRAWL_DEPTH || 5)
const T = (ms) => new Promise(r => setTimeout(r, ms))

// fixture dummies live in the repo (fixtures/credentials.ts) — parse them, no new secret files
const CRED = {}
{
  const txt = fs.readFileSync(path.join(path.resolve(ROOT), 'fixtures/credentials.ts'), 'utf8')
  const blocks = txt.split('export const ').slice(1)
  for (const b of blocks) {
    const name = b.split(' ')[0]
    const email = b.match(/email:\s*'([^']+)'/)?.[1]
    const pass = b.match(/password:\s*'([^']+)'/)?.[1]
    if (email && pass) CRED[email] = pass
  }
}

const ROLES = [
  { id: 'admin', email: 'e2e-admin@e2e.test' },
  { id: 'tpladmin', email: 'e2e-tpladmin@e2e.test' },
  { id: 'user', email: 'e2e-user@e2e.test' },
]

const PAGES = [
  '/project', '/library', '/templates', '/templates/manage',
  '/user/mysettings', '/user/llm-settings', '/user/notification-preferences',
  '/admin/instance-stats', '/admin/panel', '/admin/llm/settings',
  '/admin/site', '/admin/user', '/admin/project',
]

// never click anything that mutates shared state (or navigates away destructively)
const BLOCK = /save|submit|delete|remove|drop|publish|apply|reset|send|create|add|update|invite|share|confirm|yes|i'm sure|sure|connect|enable|disable|upload|download|leave|suspend|resume|restore|purge|transfer|clear|sign|log ?out|register|deactivate|force|revoke/i
const SKIP_HREF = /^(https?:|mailto:|#)/

const args = Object.fromEntries(
  process.argv.slice(2).map(a => { const [k, v] = a.replace(/^--/, '').split('='); return [k, v ?? true] })
)
const rolesArg = null // parser note: -​-role=a,b is a comma list
const roleIds = (args.role ? String(args.role).split(',') : null) || ROLES.map(r => r.id)
const pageSel = args.page ? String(args.page).split(',').map(p => p.startsWith('/') ? p : '/' + p) : PAGES

function stamp() {
  const commit = spawn('git', ['-C', path.resolve(ROOT, '..'), 'rev-parse', 'HEAD']).trim()
  return { commit, date: new Date().toISOString(), base: BASE, stack: 'ol-e2e-overleaf-1' }
}
function spawn(cmd, a) {
  try { return execSync([cmd, ...a].join(' '), { encoding: 'utf8', timeout: 10000 }) } catch { return '' }
}

/** Serialize the interactive + headline content of a DOM scope into a compact structure. */
async function serializeScope(page, _rootSel) {
  return page.evaluate((sel) => {
    // DEEPEST/LATEST open dialog: the last visible element matching the dialog selector set
    const all = [...document.querySelectorAll(sel)].filter(n => { try { return n.offsetParent !== null } catch { return true } })
    const scope = all.length ? all[all.length - 1] : document
    if (!scope) return { error: 'scope not found' }
    const clamp = (s, n = 90) => (s || '').replace(/\s+/g, ' ').trim().slice(0, n)
    const headings = [...scope.querySelectorAll('h1,h2,h3,[role=heading]')]
      .filter(h => h.offsetParent !== null)
      .map(h => clamp(h.textContent, 60)).slice(0, 40)
    const els = [...scope.querySelectorAll('button, input, textarea, select, a, [role=menuitem], [role=tab], [role=switch], [role=checkbox]')]
      .filter(e => { try { return e.offsetParent !== null } catch { return false } })
    const controls = els.map(e => {
      const r = e.getBoundingClientRect()
      const aria = e.getAttribute('aria-label') || ''
      const label = document.querySelector(`label[for="${e.id}"]`)?.textContent || ''
      return {
        tag: e.tagName.toLowerCase(),
        type: e.getAttribute('type') || e.getAttribute('role') || (e.tagName === 'A' ? 'link' : e.tagName === 'SELECT' ? 'select' : ''),
        name: clamp(aria || label || e.textContent || (e.attributes?.name?.value ? `[name=${e.name}]` : ''), 60),
        text: clamp(e.textContent, 50),
        value: (e.tagName === 'INPUT' && ['checkbox', 'radio'].includes(e.type) ? String(!!e.checked) : e.tagName === 'SELECT' ? (e.selectedOptions?.[0]?.textContent || '') : ''),
        disabled: e.disabled === true || e.getAttribute('aria-disabled') === 'true',
        y: Math.round(r.top),
        x: Math.round(r.left),
      }
    })
    const text = (scope.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 4000)
    return { headings, controls, text }
  }, scopeEl)
}

function dialogSelectors() {
  return '[role="dialog"], .ol-modal, [class*="Modal-content"], [class*="modal-content"], .mantine-Modal-content, .mantine-Menu-dropdown, [role="menu"], [role="tooltip"].mantine-Popover-dropdown'
}

/** Detect how many dialog-like layers are currently open + return the DEEPEST open layer's element handle. */
async function dialogState(page) {
  return page.evaluate((sel) => {
    const nodes = [...document.querySelectorAll(sel)].filter(n => n.offsetParent !== null || getComputedStyle(n).display !== 'none')
    return { count: nodes.length, sample: nodes.slice(-3).map(n => (n.getAttribute('aria-label') || n.className || n.tagName).toString().slice(0, 60)) }
  }, dialogSelectors())
}

async function main() {
  const browser = await chromium.launch({ args: ['--no-sandbox'] })
  const meta = stamp()
  const summary = []

  for (const role of ROLES.filter(r => roleIds.includes(r.id))) {
    const pass = CRED[role.email]
    if (!pass) { console.log(`SKIP role ${role.id} (no fixture password found)`); continue }
    const ctx = await browser.newContext({ viewport: { width: 1500, height: 900 } })
    const page = await ctx.newPage()
    const netLog = []
    page.on('response', async (r) => {
      const u = r.url()
      if (/(api|admin|user|project|library|template|upload|tag|session)/.test(u) && !/\.(js|css|png|svg|woff|ico)/.test(u)) {
        netLog.push({ t: Date.now(), m: r.request().method(), p: new URL(u, BASE).pathname, s: r.status() })
      }
    })

    // login (with CE rate-limit / CAPTCHA backoff, mirroring global-setup)
    async function tryLogin() {
      await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
      const cb = page.locator('button:has-text("Accept all cookies")')
      if ((await cb.count()) > 0) await cb.first().click().catch(() => {})
      await T(300)
      await page.fill('input[type="email"]', role.email)
      await page.fill('input[type="password"]', pass)
      let status = 0
      try {
        const [resp] = await Promise.all([
          page.waitForResponse(r => r.url().endsWith('/login') && r.request().method() === 'POST', { timeout: 20000 }),
          page.click('button[type="submit"]'),
        ])
        status = resp.status()
      } catch { /* body check below */ }
      await T(2500)
      if (/\/project/.test(page.url())) return true
      const body = (await page.locator('body').innerText().catch(() => '')) || ''
      if (status === 429 || /captcha|too many|invalid/i.test(body)) return 'ratelimit'
      throw new Error(`login failed (post=${status}, body="${body.replace(/\s+/g, ' ').slice(0, 160)}")`)
    }
    let attempt = await tryLogin()
    if (attempt === 'ratelimit') {
      console.log(`  [role ${role.id}] login rate-limited — waiting 75s, one retry (global-setup pattern)`)
      await T(75000)
      attempt = await tryLogin()
      if (attempt !== true) throw new Error(typeof attempt === 'string' ? 'login rate-limited twice' : attempt.message)
    } else if (attempt !== true) {
      summary.push({ role: role.id, page: 'login', ok: false, detail: String(attempt.message || attempt) }); await ctx.close(); continue
    }

    for (const pagePath of pageSel) {
      const dir = path.join(REF, role.id, pagePath.replace(/\//g, '_'))
      fs.mkdirSync(dir, { recursive: true })
      const fs2 = fs
      netLog.length = 0
      const entry = { role: role.id, page: pagePath, ok: false }
      try {
        const resp = await page.goto(BASE + pagePath, { waitUntil: 'domcontentloaded', timeout: 30000 })
        await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {})
        await T(500)
        const status = resp ? resp.status() : null
        const finalUrl = page.url()
        const redirected = finalUrl.replace(BASE, '') !== pagePath
        if (status === 403 || status === 404 || redirected) {
          fs2.writeFileSync(path.join(dir, 'ACCESS.json'), JSON.stringify({ status, finalUrl, body: (await page.evaluate(() => document.body.innerText.slice(0, 500))).catch(() => '') }, null, 2))
          fs2.writeFileSync(path.join(dir, 'access.png'), Buffer.from(await page.screenshot({ fullPage: false }), 'base64'))
          entry.ok = true; entry.detail = `denied: ${status} -> ${finalUrl.replace(BASE, '')}`
          summary.push(entry); continue
        }
        const initial = await serializeScope(page, null)
        fs2.writeFileSync(path.join(dir, 'initial.dom.json'), JSON.stringify({ ...meta, status, title: await page.title(), ...initial }, null, 2))
        fs2.writeFileSync(path.join(dir, 'network.log.json'), JSON.stringify(netLog.slice(), null, 2))
        fs2.writeFileSync(path.join(dir, 'initial.png'), Buffer.from(await page.screenshot({ fullPage: true }), 'base64'))
        entry.initialControls = initial.controls.length
        entry.initialHeadings = (initial.headings || []).join(' | ').slice(0, 200)

        // ---- deep modal BFS over root controls -------------------------------------
        const rootControls = (initial.controls || []).map((c, i) => ({ ...c, _i: i }))
        let modalCount = 0
        for (const c of rootControls) {
          if (modalCount >= 40) break // hard cap per page
          if (c.tag === 'a') continue // links navigate (recorded in DOM capture) — dialogs only open from buttons/menus
          if (c.disabled) continue
          const label = (c.name || c.text || '').trim()
          if (!label) continue
          if (BLOCK.test(label)) { entry[`blocked:${c._i}`] = label.slice(0, 40); continue }
          const before = await dialogState(page)
          const netBefore = netLog.length
          let clicked = false
          try {
            const loc = c.tag === 'select'
              ? page.locator('select').nth((c._i | 0) % 4)
              : page.getByText(label, { exact: false }).first()
            await loc.click({ timeout: 1500 })
            clicked = true
          } catch { continue }
          await T(450)
          const after = await dialogState(page)
          if (after.count > before.count) {
            modalCount++
            const layer = `m${String(modalCount).padStart(2, '0')}_${label.replace(/[^a-z0-9]+/gi, '_').slice(0, 30)}`
            const snap = await serializeScope(page, dialogSelectors()) // deepest open layer included
            fs2.writeFileSync(path.join(dir, `modal_${layer}.json`), JSON.stringify({ label, layer, netDelta: netLog.slice(netBefore).map(({ t, ...r }) => r), ...snap }, null, 2))
            fs2.writeFileSync(path.join(dir, `modal_${layer}.png`), Buffer.from(await page.screenshot({ fullPage: false }), 'base64'))
            // recurse into THIS dialog's controls (depth+1) — capped, non-destructive only
            let depth = 1
            while (depth < DEPTH_CAP) {
              const scope = page.locator(dialogSelectors()).last()
              const inner = await scope.evaluate((sc) => {
                return [...sc.querySelectorAll('button, select, input[type=checkbox], input[type=radio], [role=menuitem]')]
                  .filter(e => e.offsetParent !== null && !e.disabled)
                  .slice(0, 10)
                  .map(e => (e.getAttribute('aria-label') || e.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 50))
                  .filter(Boolean)
              }).catch(() => [])
              let descended = false
              for (const il of inner) {
                if (BLOCK.test(il)) continue
                const dBefore = await dialogState(page)
                try { await scope.locator(`:text-is("${il.replace(/"/g, '')}")`).first().click({ timeout: 1200 }) } catch { continue }
                await T(350)
                const dAfter = await dialogState(page)
                if (dAfter.count > dBefore.count) {
                  descended = true
                  const l2 = `${layer}_L${depth + 1}_${il.replace(/[^a-z0-9]+/gi, '_').slice(0, 24)}`
                  fs2.writeFileSync(path.join(dir, `modal_${l2}.json`), JSON.stringify({ parent: layer, label: il, depth: depth + 1, ...await serializeScope(page, dialogSelectors()) }, null, 2))
                  fs2.writeFileSync(path.join(dir, `modal_${l2}.png`), Buffer.from(await page.screenshot({ fullPage: false }), 'base64'))
                  // close the new layer before trying the next sibling
                  await page.keyboard.press('Escape'); await T(300)
                  break
                } else {
                  await page.keyboard.press('Escape'); await T(250)
                }
              }
              if (!descended) break
              depth++
            }
            // close back to root
            for (let i = 0; i < 3; i++) {
              await page.keyboard.press('Escape'); await T(250)
              if ((await dialogState(page)).count <= before.count) break
            }
          } else {
            await page.keyboard.press('Escape').catch(() => {}); await T(150)
          }
        }
        entry.ok = true
        entry.modals = modalCount
        summary.push(entry)
      } catch (e) {
        entry.detail = String(e).slice(0, 200)
        summary.push(entry)
      }
    }
    await ctx.close()
  }
  await browser.close()
  fs.mkdirSync(REF, { recursive: true })
  fs.writeFileSync(path.join(REF, 'summary.json'), JSON.stringify({ ...meta, at: new Date().toISOString(), entries: summary }, null, 2))
  console.log(JSON.stringify(summary, null, 1))
}
main().catch(e => { console.error('CRAWL ERROR', e); process.exit(1) })
