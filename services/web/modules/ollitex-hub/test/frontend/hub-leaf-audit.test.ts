import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'
import { describe, expect, it } from 'vitest'
import { HUB_NAV } from '../../frontend/js/hub/nav-tree'

/**
 * Leaf COMPLETENESS audit (#2, 2026-09-08 — "no silent stubs").
 *
 * Contract: every nav leaf must render a real surface. A leaf is "handled"
 * iff the renderer in hub/leaves.tsx maps it to one of:
 *   * a shared render kind (projects / template-cat / site-sec / overview /
 *     library — declared on the nav node),
 *   * an explicit `case '<id>'` in leaves.tsx,
 *   * the documented mysettings.* fallback (split milestone), or
 *   * the templates.* category wildcard (dynamic categories).
 *
 * Any leaf that matches NONE of those currently renders the "…is being
 * built" placeholder. This test makes that a FAILED BUILD instead of a
 * silent stub: new leaves added to nav-tree without a handler (or a
 * documented entry in KNOWN_STUBS) fail CI here — the PG-AP-1 regression
 * (a placeholder shipped as a "real" section) cannot reappear silently.
 */
const here = path.dirname(fileURLToPath(import.meta.url))
const root = path.join(here, '..', '..', 'frontend', 'js', 'hub')

type NavLeaf = { id: string; kind?: string }

// Leaves DELIBERATELY rendered as the honest "being built" placeholder.
// Empty list = zero stubs (the 13/13 parity end state). Add an entry here
// (with a reason comment) only when shipping a nav leaf ahead of its page.
const KNOWN_STUBS = new Set<string>([])

describe('hub leaf completeness (#2: no silent stubs)', () => {
  const leavesTs = fs.readFileSync(path.join(root, 'leaves.tsx'), 'utf8')
  const handledByKind = new Set<string>(['projects', 'projects-tags', 'template-cat', 'site-sec', 'overview', 'library'])
  const caseIds = new Set<string>([...leavesTs.matchAll(/case '([^']+)'/g)].map(m => m[1]))
  const mysettingsFallback = /startsWith\('mysettings\.'\)/.test(leavesTs)
  const templatesWildcard = leavesTs.includes("templates.")

  const allLeaves: NavLeaf[] = []
  for (const n of HUB_NAV) {
    const walk = (x: any) => {
      if (!x.children || x.children.length === 0) allLeaves.push({ id: x.id, kind: x.render })
      for (const c of x.children || []) walk(c)
    }
    walk(n)
  }

  it('the nav has leaves to audit (guard against an empty tree)', () => {
    expect(allLeaves.length).toBeGreaterThan(20)
  })

  it('every leaf label and icon is present (rail completeness)', () => {
    const walk = (nodes: any[]) => {
      for (const n of nodes) {
        expect(n.label, `leaf ${n.id} has a label`).toBeTruthy()
        if (!n.children || n.children.length === 0) {
          expect(n.icon, `leaf ${n.id} has an icon for the rail`).toBeTruthy()
        }
        if (n.children?.length) walk(n.children)
      }
    }
    walk(HUB_NAV as any)
  })

  it('every leaf is handled by a real renderer or intentionally stubbed (KNOWN_STUBS)', () => {
    const unhandled = allLeaves.filter(l => {
      if (l.kind && handledByKind.has(l.kind)) return false
      if (caseIds.has(l.id)) return false
      if (mysettingsFallback && l.id.startsWith('mysettings.')) return false
      if (templatesWildcard && l.id.startsWith('templates.')) return false
      return true
    })
    const unexpected = unhandled.filter(l => !KNOWN_STUBS.has(l.id))
    expect(
      unexpected.map(l => l.id).sort(),
      'these nav leaves render the "…is being built" placeholder without a documented KNOWN_STUBS entry — implement them or document why they stay stubs',
    ).toEqual([])
  })

  it('KNOWN_STUBS contains only real, currently-unhandled leaves (no stale entries)', () => {
    const handledIds = allLeaves.filter(l => {
      if (l.kind && handledByKind.has(l.kind)) return true
      if (caseIds.has(l.id)) return true
      if (mysettingsFallback && l.id.startsWith('mysettings.')) return true
      if (templatesWildcard && l.id.startsWith('templates.')) return true
      return false
    }).map(l => l.id)
    const stale = [...KNOWN_STUBS].filter(id => handledIds.includes(id))
    expect(stale, 'KNOWN_STUBS entries that now have a real renderer (remove them)').toEqual([])
  })

  it('current end state: the 13 parity leaves have no stubs at all', () => {
    expect(KNOWN_STUBS.size, 'add new intentional stubs here WITH a reason comment').toBe(0)
  })
})
