/**
 * a11y BASELINE (editor renovation P0d — "record the before-numbers").
 *
 * Purpose: capture where the CURRENT (legacy) editor stands today, so later
 * phases (P8 gate: zero critical/serious on /editor) can be judged against a
 * known before-state rather than vibes. Informational in P0: the spec writes
 * tests/e2e/editor/a11y-baseline.json and reports the counts but does not
 * fail on them (the legacy editor predates our a11y bar).
 *
 * Scans (admin): the editor base surface + the share modal open.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../../helpers/auth'
import { ADMIN } from '../../fixtures/credentials'
import AxeBuilder from '@axe-core/playwright'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const BASE = 'http://127.0.0.1:7420'
const OUT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../editor/a11y-baseline.json')

test('a11y baseline: legacy editor before-numbers (P0 reference)', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page = await ctx.newPage()
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const projectId = await createBlankProject(page)
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
  await page.waitForTimeout(1500) // let the shell settle (presence, rail panels)

  async function scan(label: string, filter?: string) {
    const res = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      // the CodeMirror editor region is a giant text region — keep it in scope,
      // it is part of the product surface.
      .analyze()
    const pick = (imp: string) =>
      res.violations
        .filter(v => v.impact === imp)
        .map(v => ({ id: v.id, nodes: v.nodes.length }))
    const summary = {
      label,
      critical: pick('critical'),
      serious: pick('serious'),
      moderate: pick('moderate'),
    }
    if (filter) {
      // eslint-disable-next-line no-console
      console.log(`[a11y baseline] ${filter}: ` + JSON.stringify(summary).slice(0, 400))
    }
    return summary
  }

  const base = await scan('editor base surface', 'base')
  await page.screenshot({ path: '/tmp/oltest/a11y_baseline_editor.png' }).catch(() => {})

  // share modal open — a representative modal surface
  const share = page.locator('button[aria-label="Share"], button:has-text("Share")').first()
  let modal = null
  if (await share.isVisible().catch(() => false)) {
    await share.click().catch(() => {})
    await page.waitForTimeout(1200)
    modal = await scan('share modal open')
  }
  await page.keyboard.press('Escape').catch(() => {})
  await ctx.close()

  const baseline = {
    capturedAt: new Date().toISOString(),
    url: `/Project/${projectId}`,
    base,
    modal,
  }
  fs.mkdirSync(path.dirname(OUT), { recursive: true })
  fs.writeFileSync(OUT, JSON.stringify(baseline, null, 2))
  // eslint-disable-next-line no-console
  console.log(`[a11y baseline] wrote ${OUT}`)
  expect(base, 'scan must produce a summary').toBeTruthy()
})
