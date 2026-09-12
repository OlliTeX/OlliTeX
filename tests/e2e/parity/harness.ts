import { execFileSync } from 'node:child_process'
import type { Page } from '@playwright/test'
import { login as _login, loginRobust } from '../helpers/auth'
import { mongoEval as _mongoEvalRaw } from '../helpers/host'

export const login = _login

const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'

/** mongoEval with smart scalar coercion (ObjectId()/numbers/null → typed). */
export function mongoEval(expr: string): any {
  const raw = String(_mongoEvalRaw(expr)).trim()
  const m = raw.match(/ObjectId\(["']([^"']+)["']\)/)
  if (m) return m[1]
  if (raw === 'null' || raw === 'undefined' || raw === '') return null
  const num = Number(raw)
  if (!Number.isNaN(num)) return num
  try { return JSON.parse(raw) } catch { return raw }
}

/** CSRF-safe request helper bound to the page's session. */
export async function api(p: Page, method: string, path: string, body?: unknown) {
  const headers: Record<string, string> = {}
  if (method !== 'GET') {
    const tok = await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
    if (tok) headers['X-CSRF-TOKEN'] = tok
    if (body !== undefined) headers['Content-Type'] = 'application/json'
  }
  const opts = { headers, data: body === undefined ? undefined : JSON.stringify(body) } as any
  if (method === 'GET') return p.request.get(BASE + path, opts)
  if (method === 'DELETE') return p.request.delete(BASE + path, opts)
  if (method === 'PUT') return p.request.put(BASE + path, opts)
  if (method === 'PATCH') return p.request.patch(BASE + path, opts)
  return p.request.post(BASE + path, opts)
}

/** Assert a call was captured (throws with the captured list on failure). */
export function expectCalled(cap: { calls: any[] }, fn: (c: any[]) => boolean, label: string) {
  if (!fn(cap.calls)) {
    throw new Error(`expected ${label}; saw: ` + cap.calls.map((c: any) => `${c.method} ${c.path} -> ${c.status}`).join(' | '))
  }
}

/**
 * Parity harness — shared by the LEGACY baseline specs and the HUB parity specs.
 *
 * The owner's equivalence definition: the /hub feature does what the legacy
 * feature did — same endpoints + payloads, same resulting visible state, same
 * role/flag/state sensitivity. This helper captures the API contract while a
 * real browser flow runs, so specs assert on behaviour, not on brittle DOM.
 */
export type ApiCall = { method: string; path: string; body?: any; status?: number }

/**
 * Start capturing API calls (method + pathname + JSON body + response status).
 * Returns { calls, stop }. Ignores static assets and the socket.io websocket.
 */
export function captureApi(page: any, baseURL: string) {
  const calls: ApiCall[] = []
  const interesting = (p: string) =>
    !/\.(js|css|png|jpe?g|gif|svg|woff2?|ico|map)(\?|$)/i.test(p) &&
    !/socket\.io|__webpack|favicon/i.test(p)
  const reqMap = new WeakMap<any, { method: string; path: string; body?: any }>()

  page.on('request', (req: any) => {
    let p = ''
    try {
      const u = new URL(req.url(), baseURL)
      p = u.pathname
    } catch { return }
    if (!interesting(p)) return
    let body
    try {
      const raw = req.postData()
      if (raw) body = raw.startsWith('{') ? JSON.parse(raw) : raw
    } catch { body = req.postData() }
    reqMap.set(req, { method: req.method(), path: p, body })
  })

  page.on('response', async (res: any) => {
    const meta = reqMap.get(res.request())
    if (!meta) return
    calls.push({ ...meta, status: res.status() })
  })

  const find = (method: string, pathRe: RegExp) =>
    calls.find(c => c.method === method && pathRe.test(c.path))
  const stop = () => {
    page.removeAllListeners('request')
    page.removeAllListeners('response')
  }
  return { calls, find, stop }
}

/** Poll until a predicate over the captured calls is true (UI latency). */
export async function waitForCall(cap: { calls: ApiCall[] }, fn: (c: ApiCall[]) => boolean, label: string, timeoutMs = 8000): Promise<void> {
  const start = Date.now()
  for (;;) {
    if (fn(cap.calls)) return
    if (Date.now() - start > timeoutMs) {
      throw new Error(`timeout waiting for ${label}; saw:\n` + cap.calls.slice(0, 40).map(c => `  ${c.method} ${c.path} -> ${c.status}`).join('\n'))
    }
    await new Promise(r => setTimeout(r, 120))
  }
}

/** Create a throwaway project via the standard CE endpoint (POST /project/create). */
export async function mkProject(p: any, name: string) {
  const r = await api(p, 'POST', '/project/new', { projectName: name })
  const j = await r.json().catch(() => ({}))
  const id = j.project_id ?? j.project?._id ?? j._id
  if (!id) throw new Error('mkProject failed: ' + r.status() + ' ' + JSON.stringify(j).slice(0, 160))
  return { _id: id } as { _id: string }
}

/** Soft-delete + hard-purge a throwaway project (admin rights required). */
export async function killProject(p: any, pid: string) {
  try {
    await api(p, 'POST', `/admin/project/${pid}/trash`, { userId: 'null' })
    await api(p, 'DELETE', `/admin/project/${pid}`)
    await api(p, 'DELETE', `/admin/project/${pid}/purge`)
  } catch { /* best effort cleanup */ }
}

/**
 * Ensure the persistent fixture template "Parity Fixture Template" exists
 * (idempotent). Fresh stacks wipe mongo and the two gallery parity specs
 * depend on this long-lived fixture. Logs in as ADMIN in its own context so
 * it works no matter which role the calling spec runs as. A 409 means a
 * parallel worker created it in the meantime — fine.
 */
export async function ensureFixtureTemplate(browser: import('@playwright/test').Browser): Promise<void> {
  const { ADMIN } = (await import('../fixtures/credentials')) as any
  const c = await browser.newContext()
  const q = await c.newPage()
  // 2026-09-12 (green gate): hoisted so the finally-block category self-heal
  // can see it (declared inside `try` would be out of scope there).
  let csrf: string | null = null
  try {
    await loginRobust(q, ADMIN.email, ADMIN.password)
    // CSRF token comes from a rendered page's meta tag (this fork's POST
    // guard: X-CSRF-TOKEN is required, like the manage-spec's api() helper).
    await q.goto(BASE + '/project', { waitUntil: 'domcontentloaded' }).catch(() => {})
    csrf = await q.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
    const j = await (async () => {
      const r = await q.request.get(BASE + '/api/templates', csrf ? { headers: { 'X-CSRF-TOKEN': csrf } } : {})
      return r.json().catch(() => ({}))
    })()
    const arr: any[] = j.templates ?? (Array.isArray(j) ? j : [])
    const existing = arr.find(x => (x.name || '') === 'Parity Fixture Template')
    if (existing) {
      // 2026-09-12 (green gate): the gallery filters by the PATH form
      // (category = '/templates/<key>'); a fixture seeded via
      // POST /template/new keeps the bare key and never matches the filter —
      // normalize it once (edit accepts { category }).
      const cat: string = String(existing.category || '')
      if (cat && !cat.startsWith('/templates/')) {
        const r2 = await q.request.post(
          BASE + '/template/' + (existing.id || existing._id) + '/edit',
          { headers: csrf ? { 'X-CSRF-TOKEN': csrf, 'Content-Type': 'application/json' } : { 'Content-Type': 'application/json' }, data: JSON.stringify({ category: '/templates/' + cat }) }
        )
        if (![200, 204].includes(r2.status())) {
          console.warn('ensureFixtureTemplate: category normalize returned ' + r2.status())
        }
      }
      return
    }
    const fs = await import('node:fs')
    const src = '/tmp/tplfix'
    if (!fs.existsSync(src + '/source.zip') || !fs.existsSync(src + '/output.pdf')) {
      // first stack: build the bundle assets once
      if (!fs.existsSync(src)) fs.mkdirSync(src, { recursive: true })
      const doc = '\\documentclass{article}\\begin{document}Parity Fixture Body\\end{document}\n'
      fs.mkdirSync(src + '/srcdir', { recursive: true })
      fs.writeFileSync(src + '/srcdir/main.tex', doc)
      execFileSync('zip', ['-q', 'source.zip', 'main.tex'], { cwd: src + '/srcdir' })
      // /tmp/tplfix/source.zip was created inside srcdir — move it up
      fs.renameSync(src + '/srcdir/source.zip', src + '/source.zip')
      fs.writeFileSync(src + '/output.pdf', '%PDF-1.5 fake fixture pdf\n%%EOF\n')
    }
    const dir = '/tmp/tplfix_seed' + Date.now()
    fs.mkdirSync(dir, { recursive: true })
    fs.copyFileSync(src + '/source.zip', dir + '/source.zip')
    fs.copyFileSync(src + '/output.pdf', dir + '/output.pdf')
    fs.writeFileSync(dir + '/template.json', JSON.stringify({ name: 'Parity Fixture Template', version: '1.0.0', category: 'academic-journal', author: 'e2e-fixture', description: 'persistent fixture template for gallery parity specs' }))
    execFileSync('zip', ['-q', dir + '/bundle.zip', 'template.json', 'source.zip', 'output.pdf'], { cwd: dir })
    const data = fs.readFileSync(dir + '/bundle.zip').toString('base64')
    const r = await q.request.post(BASE + '/template/bundle/import', {
      headers: { 'Content-Type': 'application/json', ...(csrf ? { 'X-CSRF-TOKEN': csrf } : {}) },
      data: JSON.stringify({ data, override: false }),
    })
    if (![200, 201, 409].includes(r.status())) {
      throw new Error(`ensureFixtureTemplate: import failed (${r.status()})`)
    }
  } finally {
    await ensureTemplateCategories(q, csrf)
    await c.close().catch(() => {})
  }
}

/**
 * 2026-09-12 (green gate): the `templates` site-settings section must carry a
 * non-empty `categories` array — GET /api/template/categories is served ONLY
 * from that array (TemplateGalleryManager.getEnabledCategories), and the
 * legacy-templates-manage "persists and restores" spec used to REPLACE the
 * section with { enabled } only, wiping it — order-dependent flake for both
 * template-gallery parity specs. Self-heal here: every spec that ensures the
 * fixture template also re-asserts the canonical category list, regardless of
 * what ran before it.
 */
async function ensureTemplateCategories(q: import('@playwright/test').Page, csrf: string | null): Promise<void> {
  const h = csrf ? { 'X-CSRF-TOKEN': csrf } : {}
  try {
    const all = await q.request.get(BASE + '/admin/site-settings', { headers: h })
    const j = (await all.json().catch(() => ({}))) as any
    const sec = (j.templates ?? j.sections?.templates) as any
    const cats = Array.isArray(sec?.categories) ? sec.categories : []
    if (cats.length > 0) return // already intact
  } catch {
    // section not readable (shouldn't happen: admin session) → re-seed anyway
  }
  const canonical = [
    { key: 'academic-journal', name: 'Academic journals', enabled: true },
    { key: 'book', name: 'Books', enabled: true },
  ]
  const base = (async () => {
    try {
      const j = (await (await q.request.get(BASE + '/admin/site-settings', { headers: h })).json()) as any
      const sec = j.templates ?? j.sections?.templates ?? {}
      return { enabled: sec.enabled !== false, ...sec }
    } catch {
      return { enabled: true }
    }
  })()
  const merged = { ...(await base), categories: canonical }
  const r = await q.request.put(BASE + '/admin/site-settings/templates', { headers: h, data: JSON.stringify(merged) })
  if (![200, 204].includes(r.status())) {
    // non-fatal: the categories spec will tell the story if this ever regresses
    console.warn('ensureFixtureTemplate: categories re-seed returned ' + r.status())
  }
}
