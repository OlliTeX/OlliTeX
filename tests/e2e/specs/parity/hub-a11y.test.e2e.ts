import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER } from '../../fixtures/credentials'
import AxeBuilder from '@axe-core/playwright'

/**
 * ACCESSIBILITY SCAN (#10, 2026-09-08 — "a11y: axe-core scan of the 13 leaves").
 *
 * 2026-10-11 (owner /hub badge-contrast incident) — the gate was widened so
 * this class of bug can never ship silently again. The incident: hub.css
 * pinned light-variant Badge labels to #1b1c1d UNCONDITIONALLY; correct in
 * the LIGHT scheme (dark text on light tints), but in the DARK scheme it
 * overrode Mantine's dark-pill + color-0 pair down to ≈ 1.0:1 (unreadable
 * badges). It slipped through because:
 *   1. this spec audited the DEFAULT (light) scheme only — no dark run;
 *   2. axe reports text contrast at `moderate` impact by default and this
 *      spec failed on critical/serious only (moderate → console backlog);
 *   3. axe `incomplete` contrast results were dropped.
 * The three fixes here, per-leaf:
 *   A. the leaf is audited in BOTH schemes (dark forced via
 *      POST /user/settings overallTheme — '' = Dark, 'light-' = Light);
 *   B. a `color-contrast` VIOLATION now fails at ANY impact after one
 *      rescan (the transient-hydrate allowance stays);
 *   C. a deterministic WCAG check computes text-vs-effective-background
 *      for every visible light-variant Badge label + Alert title and
 *      requires ≥ 4.5:1 — this is computed math, so it catches cases axe
 *      returns as `incomplete` (effective bg uncertain).
 */
const BASE = 'http://127.0.0.1:7420'

interface Case {
  role: 'user' | 'admin'
  leaf: string
  creds: any
}

const CASES: Case[] = [
  { role: 'user', leaf: '/hub#/projects.all', creds: USER },
  { role: 'user', leaf: '/hub#/library', creds: USER },
  { role: 'user', leaf: '/hub#/templates.all', creds: USER },
  { role: 'user', leaf: '/user-settings/mysettings.account', creds: USER },
  { role: 'user', leaf: '/user-settings/mysettings.llm.usage', creds: USER },
  { role: 'admin', leaf: '/admin-settings/site.llm.usage', creds: ADMIN },
  { role: 'admin', leaf: '/hub#/overview', creds: ADMIN },
  { role: 'admin', leaf: '/admin-settings/site.general.users.all', creds: ADMIN },
  { role: 'admin', leaf: '/admin-settings/site.general.messages', creds: ADMIN },
  { role: 'admin', leaf: '/admin-settings/site.general.health', creds: ADMIN },
  { role: 'admin', leaf: '/admin-settings/site.general.stats', creds: ADMIN },
]

const SCHEMES: { name: string; overallTheme: string }[] = [
  { name: 'light', overallTheme: 'light-' },
  { name: 'dark', overallTheme: '' }, // '' = Dark (theme-toggle CHROME_OPTIONS)
]

// ---------- WCAG 2.x relative-luminance / contrast math ----------
function parseColor(s: string): [number, number, number] | null {
  const m = s.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/)
  if (!m) return null
  return [Number(m[1]), Number(m[2]), Number(m[3])]
}
function lum(c: [number, number, number]): number {
  const [r, g, b] = c.map((v) => {
    const x = v / 255
    return x <= 0.03928 ? x / 12.92 : Math.pow((x + 0.055) / 1.055, 2.4)
  }) as [number, number, number]
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
function contrast(
  c1: [number, number, number],
  c2: [number, number, number],
): number {
  const l1 = lum(c1)
  const l2 = lum(c2)
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05)
}

// ---------- the deterministic component-contrast check (fix C) ----------
// Walks up from each visible light-variant Badge label / Alert title to the
// first non-transparent background and asserts AA (4.5:1) text contrast.
async function assertComponentContrast(page: any, leaf: string, scheme: string) {
  const report: any[] = await page.evaluate(() => {
    const rows: any[] = []
    const sels = [
      ".mantine-Badge-root[data-variant='light'] .mantine-Badge-label",
      ".mantine-Alert-root[data-variant='light'] .mantine-Alert-title",
    ]
    for (const sel of sels) {
      for (const el of Array.from(document.querySelectorAll<HTMLElement>(sel))) {
        const r = (el as HTMLElement).getClientRects()
        if (!r.length) continue
        const cs = getComputedStyle(el)
        if (cs.visibility === 'hidden' || cs.display === 'none' || cs.opacity === '0') continue
        const fg = cs.color
        // effective bg: first non-transparent ancestor (incl. self) bg
        let cur: HTMLElement | null = el
        let bg: string | null = null
        while (cur) {
          const b = getComputedStyle(cur).backgroundColor
          if (b && b !== 'rgba(0, 0, 0, 0)' && b !== 'transparent') {
            bg = b
            break
          }
          cur = cur.parentElement
        }
        if (!bg) bg = getComputedStyle(document.body).backgroundColor
        rows.push({
          sel: sel.slice(0, 34),
          text: (el.textContent || '').trim().slice(0, 24),
          fg,
          bg,
        })
      }
    }
    return rows
  })
  const bad = report.filter((row: any) => {
    const fg = parseColor(row.fg || '')
    const bg = parseColor(row.bg || '')
    if (!fg || !bg) return false // unparsed → let axe handle / skip
    return contrast(fg, bg) < 4.5
  })
  expect(
    bad,
    `${scheme} ${leaf}: light-variant badge-label / alert-title contrast below AA 4.5:1 — ` +
      JSON.stringify(bad.slice(0, 5)),
  ).toEqual([])
}

test.describe('hub a11y (axe-core, both schemes)', () => {
  for (const c of CASES) {
    for (const sc of SCHEMES) {
      test(`no contrast violations — ${c.role} ${c.leaf} [${sc.name}]`, async ({ browser }) => {
        const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
        const page = await ctx.newPage()
        await loginRobust(page, c.creds.email, c.creds.password)
        // force the audit scheme for this user (per-session setting)
        await page.goto('/', { waitUntil: 'domcontentloaded' }).catch(() => {})
        await api(page, 'POST', '/user/settings', { overallTheme: sc.overallTheme })
        await page.goto(BASE + c.leaf, { waitUntil: 'domcontentloaded' })
        // let the leaf settle (data + rails). admin data sections fetch after
        // first paint — axe sampling mid-hydration produced intermittent
        // false color-contrast reads (badge-on-default-bg) → networkidle
        // before analyzing (kept from the 2026-09 original).
        await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {})
        await page.waitForTimeout(2000)

        // FIX C first (deterministic, scheme-specific, always runs)
        await assertComponentContrast(page, c.leaf, sc.name)

        const analyze = () =>
          new AxeBuilder({ page })
            .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
            .analyze()

        let res: any = await analyze()
        let contrastViol = res.violations.filter((v: any) => v.id === 'color-contrast')
        let serious = res.violations.filter(
          (v: any) => v.impact === 'critical' || v.impact === 'serious',
        )
        if (serious.length || contrastViol.length) {
          // transient-hydrate allowance (original): rescan once after a
          // settle window; only a PERSISTENT violation fails the run.
          await page.waitForTimeout(1500)
          res = await analyze()
          contrastViol = res.violations.filter((v: any) => v.id === 'color-contrast')
          serious = res.violations.filter(
            (v: any) => v.impact === 'critical' || v.impact === 'serious',
          )
          const moderate = res.violations.filter((v: any) => v.impact === 'moderate')
          if (moderate.length) {
            // eslint-disable-next-line no-console
            console.log(
              `[a11y backlog] ${sc.name} ${c.leaf}:`,
              moderate.map((v: any) => `${v.id} (${v.impact}) x${v.nodes.length}`).join('; '),
            )
          }
        }
        // FIX B: color-contrast fails at ANY impact (moderate included) —
        // axe's default impact for text contrast is `moderate`, which the
        // 2026-09 original silently reported as backlog. Include axe's own
        // computed pair/message for triage.
        expect(
          contrastViol.map((v: any) => ({
            impact: v.impact,
            nodes: v.nodes.slice(0, 3).map((n: any) => ({
              target: n.target.join(' '),
              why: ((n.any || []).flatMap((a: any) => a.message || []).join(' ')).slice(0, 160),
            })),
          })),
          `${sc.name} ${c.leaf}: axe color-contrast violation (persisted across rescan)`,
        ).toEqual([])
        expect(
          serious.map((v: any) => ({
            id: v.id,
            impact: v.impact,
            help: v.help,
            nodes: v.nodes.slice(0, 3).map((n: any) => n.target.join(' ')),
          })),
          `${sc.name} ${c.leaf}: axe critical/serious violations (persisted across rescan)`,
        ).toEqual([])

        await ctx.close()
      })
    }
  }

  test('restore dark default theme for the fixtures', async ({ browser }) => {
    // housekeeping: leave both fixtures on the hub default (Dark) so other
    // specs see the standing default after the dual-scheme sweep.
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    for (const creds of [USER, ADMIN]) {
      await loginRobust(page, creds.email, creds.password)
      await page.goto('/', { waitUntil: 'domcontentloaded' }).catch(() => {})
      await api(page, 'POST', '/user/settings', { overallTheme: '' }).catch(() => {})
    }
    await ctx.close()
  })
})
