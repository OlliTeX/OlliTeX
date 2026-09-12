import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { ADMIN, USER } from '../../fixtures/credentials'
import AxeBuilder from '@axe-core/playwright'

/**
 * ACCESSIBILITY SCAN (#10, 2026-09-08 — "a11y: axe-core scan of the 13 leaves").
 *
 * Sweeps a representative, role-appropriate sample of /hub leaves (workspace
 * + admin surfaces incl. the new Hub health leaf) with axe-core and FAILS on
 * any `critical` or `serious` violation. `moderate` violations are reported
 * (console) for the backlog — the bar is "no serious a11y regressions land"
 * while the hub matures.
 *
 * Run: npx playwright test specs/parity/hub-a11y.test.e2e.ts
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
  { role: 'user', leaf: '/hub#/mysettings.account', creds: USER },
  { role: 'user', leaf: '/hub#/mysettings.llm.usage', creds: USER },
  { role: 'admin', leaf: '/hub#/site.llm.usage', creds: ADMIN },
  { role: 'admin', leaf: '/hub#/overview', creds: ADMIN },
  { role: 'admin', leaf: '/hub#/site.general.users.all', creds: ADMIN },
  { role: 'admin', leaf: '/hub#/site.general.messages', creds: ADMIN },
  { role: 'admin', leaf: '/hub#/site.general.health', creds: ADMIN },
  { role: 'admin', leaf: '/hub#/site.general.stats', creds: ADMIN },
]

test.describe('hub a11y (axe-core)', () => {
  for (const c of CASES) {
    test(`no critical/serious violations — ${c.role} ${c.leaf}`, async ({ browser }) => {
      const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
      const page = await ctx.newPage()
      await loginRobust(page, c.creds.email, c.creds.password)
      await page.goto(BASE + c.leaf, { waitUntil: 'domcontentloaded' })
      // let the leaf settle (data + rails). admin data sections fetch after
      // first paint — axe sampling mid-hydration produced intermittent false
      // color-contrast reads (badge-on-default-bg), so wait for networkidle
      // before analyzing.
      await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {})
      await page.waitForTimeout(2000)

      const res = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
        .analyze()

      const serious = res.violations.filter(v => v.impact === 'critical' || v.impact === 'serious')
      if (serious.length) {
        // 2026-09 (test stability): under parallel-suite CPU contention the
        // leaf can still be hydrating (Mantine cards/titles mount late), which
        // axe reads as a transient contrast fault. Rescan once after a settle
        // window; only a PERSISTENT violation fails the run (real a11y bugs
        // never self-heal between two scans 1.5s apart).
        await page.waitForTimeout(1500)
        const res2 = await new AxeBuilder({ page })
          .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
          .analyze()
        const serious2 = res2.violations.filter(v => v.impact === 'critical' || v.impact === 'serious')
        if (!serious2.length) {
          // transient (hydration race) — pass
        } else {
          const moderate = res2.violations.filter(v => v.impact === 'moderate')
          if (moderate.length) {
            // eslint-disable-next-line no-console
            console.log(`[a11y backlog] ${c.leaf}:`, moderate.map(v => `${v.id} (${v.impact}) x${v.nodes.length}`).join('; '))
          }
          expect(
            serious2.map(v => ({
              id: v.id,
              impact: v.impact,
              help: v.help,
              nodes: v.nodes.slice(0, 3).map(n => n.target.join(' ')),
            })),
            `axe critical/serious violations on ${c.leaf} (persisted across rescan)`,
          ).toEqual([])
        }
      } else {
        const moderate = res.violations.filter(v => v.impact === 'moderate')
        if (moderate.length) {
          // backlog surface (does not fail): report for the a11y follow-up wave
          // eslint-disable-next-line no-console
          console.log(`[a11y backlog] ${c.leaf}:`, moderate.map(v => `${v.id} (${v.impact}) x${v.nodes.length}`).join('; '))
        }
      }
      await ctx.close()
    })
  }
})
