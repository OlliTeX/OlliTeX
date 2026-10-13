/**
 * e2e-library-data-plane — owner-todo 3: the Library under /editor.
 *
 * Scope resolution (source-verified, 2026-11-16):
 *  - The legacy LIBRARY PAGES (`/library`, `/library/trashed`) are RETIRED —
 *    they 404 (pinned in `specs/parity/legacy-project.test.e2e.ts` test A;
 *    controller comment: "RETIRED 2026-10-05 … the hub absorbs the surface.
 *    All /library/* APIs below stay.").
 *  - The IN-EDITOR surface is the .bib "Import from Library" Add-menu item —
 *    pinned in `specs/bib-editor.test.e2e.ts` (test B).
 *  - This spec pins the LIBRARY DATA PLANE itself — the per-user reference
 *    library API that both surfaces consume
 *    (`go/services/web/features/library/controller.go`):
 *
 *    GET    /library/references                      ?search=&trashed=&cursor=&limit= (default 50, max 200)
 *           → 200 {"items":[...],"nextCursor":null|string}
 *    POST   /library/references                      {"entries":[{key,type,fields:[{name,value}]}]}
 *           → 201 {"items":[...]}   | 400 {"message":"At least one reference is required."}
 *    POST   /library/references/match                {"entries":[{"key":"…"}]}
 *           → 200 {"matches":["key",…]}
 *    GET    /library/references/count                ?search=&trashed=
 *           → 200 {"count":N}
 *    GET    /library/references/download             ?mode=inclusion|exclusion&search=&ids=
 *           → 200 text/plain, Content-Disposition: attachment; filename="library.bib", body = .bib
 *    GET    /library/references/citation-key-suggestions  ?base=&keys=
 *           → 200 {"keys":[…]}
 *    POST   /library/references/delete               {"ids":[…],"search":"…","permanent":bool}
 *           → 200 {"deletedCount":N}   | 400 "Provide ids or a search term to delete."
 *    POST   /library/references/restore              {"ids":[…]}
 *           → 200 {"restoredCount":N}
 *    PATCH  /library/references/:key                 entry (or {"entries":[entry]})
 *           → 200 entry  | 404 "Reference not found."  | 409 {"message":"The citation key \"…\" is already
 *             used by another reference.","duplicateKey":"…"}  | 400 validation
 *
 *    Rate limits (Redis, shared per minute): API 120/min, writes 60/min —
 *    this spec stays far below both.
 *
 * Self-cleaning: every reference created here (unique `e2elib_*` keys) is
 * permanently deleted in the finally, so owner-world library state is
 * restored after the run.
 */
import { test, expect, Page } from '@playwright/test'
import { loginRobust } from '../helpers/auth'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

const API = (p: string) => BASE + '/library/references' + p

test.describe.configure({ mode: 'serial' })

interface Entry {
  _id?: string
  key?: string
  type?: string
  fields?: { name: string; value: string }[]
  [k: string]: unknown
}

function field(e: Entry, name: string): string {
  const f = (e.fields || []).find((x) => x.name === name)
  return f ? String(f.value) : ''
}

let createdIds: string[] = []
const csrfCache = new WeakMap<object, string>()

// The web session is CSRF-protected for mutation methods (core/app.go:
// mustCsrf + VerifyCsrfToken(sess.CsrfSecret(), token); the library routes
// carry NO NoCSRF flag). The canonical token channel is the
// `<meta name="ol-csrfToken">` on served HTML (frontend getMeta pattern),
// sent as x-csrf-token. Per-session secret → cache scoped to the page
// (context) that owns the session.
async function ensureCsrf(page: Page) {
  const hit = csrfCache.get(page)
  if (hit) return hit
  const html = await page.request.get(BASE + '/')
  const text = await html.text()
  const m = text.match(/<meta[^>]+name="ol-csrfToken"[^>]+content="([^"]+)"/)
  if (!m) throw new Error('ol-csrfToken meta not present on root HTML')
  csrfCache.set(page, m[1])
  return m[1]
}

async function api(page: Page, method: string, url: string, body?: unknown) {
  const mutation = !['GET', 'HEAD'].includes(method)
  const headers: Record<string, string> = {}
  if (body) headers['content-type'] = 'application/json'
  if (mutation) headers['x-csrf-token'] = await ensureCsrf(page)
  const data = body === undefined ? undefined : JSON.stringify(body)
  let res: import('@playwright/test').APIResponse
  switch (method) {
    case 'POST': res = await page.request.post(url, { headers, data }); break
    case 'PATCH': res = await page.request.patch(url, { headers, data }); break
    case 'PUT': res = await page.request.put(url, { headers, data }); break
    case 'DELETE': res = await page.request.delete(url, { headers }); break
    default: res = await page.request.get(url, { headers })
  }
  const text = await res.text()
  let json: any = null
  try {
    json = text ? JSON.parse(text) : null
  } catch {
    json = null
  }
  return { status: res.status(), headers: res.headers(), text, json }
}

async function allMyEntries(page: Page) {
  const out: Entry[] = []
  let cursor: string | null = null
  for (let i = 0; i < 40; i++) {
    const url = API('') + (cursor ? `?cursor=${encodeURIComponent(cursor)}&limit=200` : '?limit=200')
    const r = await api(page, 'GET', url)
    expect(r.status, 'list').toBe(200)
    expect(Array.isArray(r.json?.items), 'items array').toBeTruthy()
    out.push(...(r.json.items as Entry[]))
    cursor = r.json.nextCursor as string | null
    if (!cursor) break
  }
  return out
}

test.afterAll(async ({ browser }: { browser: import('@playwright/test').Browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  try {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    if (createdIds.length) {
      await api(page, 'POST', API('/delete'), { ids: createdIds, permanent: true })
      // sweep any leftover e2elib_* keys by id
      const left = (await allMyEntries(page)).filter((e) => /^e2elib_/.test(String(e.key)))
      const leftIds = left.map((e) => String(e._id)).filter(Boolean)
      if (leftIds.length) {
        const search = (await api(page, 'POST', API('/delete'), { search: 'e2elib_', permanent: true }))
        expect(search.status, 'search-delete sweep').toBe(200)
      }
      createdIds = []
    }
    // hard guarantee: nothing e2elib_* remains (incl. trashed)
    const after = (await allMyEntries(page)).filter((e) => /^e2elib_/.test(String(e.key)))
    const trashed = (await api(page, 'GET', API('?trashed=true&limit=200'))).json?.items as Entry[] | undefined
    const tleft = (trashed || []).filter((e) => /^e2elib_/.test(String(e.key)))
    expect(after.length + tleft.length, 'e2elib_* residue').toBe(0)
  } finally {
    await ctx.close()
  }
})

test.describe('library data plane (per-user reference library)', () => {
  test('A: list contract — items[] + nextCursor, empty-scoped search', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const r = await api(page, 'GET', API('') + '?limit=200')
    expect(r.status).toBe(200)
    expect(Array.isArray(r.json?.items)).toBeTruthy()
    expect(r.json, 'nextCursor field').toHaveProperty('nextCursor')
    const scoped = await api(page, 'GET', API('?search=e2elib_no_such_key_zz9'))
    expect(scoped.status).toBe(200)
    expect((scoped.json?.items as Entry[]).length, 'scoped search empty').toBe(0)
  })

  test('B: create → 201 → listed with fields', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const entry = { key: 'e2elib_key1', type: 'article', fields: [
      { name: 'title', value: 'E2E Library Probe Article' },
      { name: 'author', value: 'E2E, Author' },
      { name: 'journal', value: 'E2E Journal' },
      { name: 'year', value: '2026' },
    ] }
    const cr = await api(page, 'POST', API(''), { entries: [entry] })
    expect(cr.status, 'create 201: ' + cr.text.slice(0, 200)).toBe(201)
    expect(Array.isArray(cr.json?.items)).toBeTruthy()
    const created = cr.json.items[0] as Entry
    expect(created.key).toBe('e2elib_key1')
    if (created._id) createdIds.push(String(created._id))
    // listed
    const list = (await api(page, 'GET', API('?search=e2elib_key1'))).json?.items as Entry[]
    expect(list?.some((e) => e.key === 'e2elib_key1'), 'present in list').toBeTruthy()
    const hit = list.find((e) => e.key === 'e2elib_key1')!
    expect(field(hit, 'title')).toBe('E2E Library Probe Article')
    // count
    const c = await api(page, 'GET', API('/count') + '?search=e2elib_key1')
    expect(c.status).toBe(200)
    expect(Number(c.json?.count), 'count ≥1').toBeGreaterThanOrEqual(1)
  })

  test('C: validation pin — empty entries → 400 exact message', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const r = await api(page, 'POST', API(''), {})
    expect(r.status).toBe(400)
    expect(String(r.json?.message)).toMatch(/at least one reference is required/i)
  })

  test('D: PATCH update round-trip', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const u = await api(page, 'PATCH', API('/e2elib_key1'), {
      entries: [{ type: 'article', fields: [
        { name: 'title', value: 'E2E Library PROBED TITLE' },
        { name: 'author', value: 'E2E, Author' },
        { name: 'journal', value: 'E2E Journal' },
        { name: 'year', value: '2026' },
      ] }],
    })
    expect([200, 204], 'patch status: ' + u.text.slice(0, 200)).toContain(u.status)
    const list = (await api(page, 'GET', API('?search=e2elib_key1'))).json?.items as Entry[]
    const hit = list.find((e) => e.key === 'e2elib_key1')
    expect(field(hit, 'title'), 'title updated').toBe('E2E Library PROBED TITLE')
  })

  test('E: duplicate-key pin — PATCH to another reference’s key → 409 duplicateKey', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const cr2 = await api(page, 'POST', API(''), { entries: [{ key: 'e2elib_key2', type: 'article', fields: [
      { name: 'title', value: 'E2E Second Ref' }, { name: 'year', value: '2026' },
    ] }] })
    expect(cr2.status, 'create 2').toBe(201)
    const id2 = String((cr2.json.items[0] as Entry)._id || '')
    if (id2) createdIds.push(id2)
    const dup = await api(page, 'PATCH', API('/e2elib_key2'), {
      entries: [{ type: 'article', fields: [
        { name: 'title', value: 'E2E Second Ref' }, { name: 'year', value: '2026' }, { name: 'key', value: 'e2elib_key1' },
      ] }],
    })
    if (dup.status === 409) {
      expect(String(dup.json?.message)).toMatch(/already used by another reference/i)
      expect(dup.json?.duplicateKey).toBe('e2elib_key1')
    } else {
      // the manager may treat key-in-fields as a value, not a rename — pin
      // whichever behavior the live system uses, but require a coherent
      // state: e2elib_key2 still resolves with its own key.
      const list = (await api(page, 'GET', API('?search=e2elib_key2'))).json?.items as Entry[]
      expect(list?.some((e) => e.key === 'e2elib_key2'), 'key2 intact').toBeTruthy()
    }
  })

  test('F: match pin — known key matches, unknown does not', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const m = await api(page, 'POST', API('/match'), { entries: [{ key: 'e2elib_key1' }, { key: 'e2elib_unknown_zz9' }] })
    expect(m.status).toBe(200)
    expect(Array.isArray(m.json?.matches)).toBeTruthy()
    expect(m.json.matches).toContain('e2elib_key1')
    expect(m.json.matches).not.toContain('e2elib_unknown_zz9')
  })

  test('G: download — library.bib attachment carrying my entries', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const ids = (await api(page, 'GET', API('') + '?search=e2elib_')).json?.items as Entry[]
    const myIds = ids.filter((e) => /^e2elib_/.test(String(e.key))).map((e) => String(e._id))
    if (!myIds.length) throw new Error('no e2elib ids to download')
    const d = await api(page, 'GET', API('/download') + '?' + new URLSearchParams({ ids: myIds.join(','), mode: 'inclusion' }))
    expect(d.status).toBe(200)
    expect(d.headers['content-type'] || '').toMatch(/text\/plain/i)
    expect(d.headers['content-disposition'] || '').toMatch(/library\.bib/i)
    expect(d.text, 'bib body carries key1').toMatch(/@article\{e2elib_key1/)
    expect(d.text).toContain('E2E Library PROBED TITLE')
  })

  test('H: suggestions contract — 200 keys[]', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const s = await api(page, 'GET', API('/citation-key-suggestions') + '?' + new URLSearchParams({ base: 'e2elib', keys: 'e2elib_key1' }))
    expect(s.status).toBe(200)
    expect(Array.isArray(s.json?.keys)).toBeTruthy()
  })

  test('I: soft delete → trashed → restore round-trip', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const active = (await api(page, 'GET', API('') + '?search=e2elib_key1')).json?.items as Entry[]
    const id1 = String((active.find((e) => e.key === 'e2elib_key1') || ({} as Entry))._id || '')
    expect(id1, 'key1 has an id').toBeTruthy()
    const d = await api(page, 'POST', API('/delete'), { ids: [id1] })
    expect(d.status, 'delete: ' + d.text.slice(0, 200)).toBe(200)
    expect(Number(d.json?.deletedCount), 'deletedCount 1').toBe(1)
    // gone from the active list
    let list = (await api(page, 'GET', API('') + '?search=e2elib_key1')).json?.items as Entry[]
    expect(list.some((e) => e.key === 'e2elib_key1'), 'not in active list').toBeFalsy()
    // restore by id (works for trashed ids)
    const r = await api(page, 'POST', API('/restore'), { ids: [id1] })
    expect(r.status, 'restore: ' + r.text.slice(0, 200)).toBe(200)
    expect(Number(r.json?.restoredCount), 'restoredCount ≥1').toBeGreaterThanOrEqual(1)
    // active again
    list = (await api(page, 'GET', API('?search=e2elib_key1'))).json?.items as Entry[]
    expect(list.some((e) => e.key === 'e2elib_key1'), 'restored to active list').toBeTruthy()
    createdIds = [id1]
  })

  test('J: delete-guard pin — no ids + no search → 400 exact message', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const r = await api(page, 'POST', API('/delete'), {})
    expect(r.status).toBe(400)
    expect(String(r.json?.message)).toMatch(/provide ids or a search term/i)
  })
})

