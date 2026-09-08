/**
 * overleaf-lab editor renovation P0 — route contract test.
 *
 * The /editor/:Project_id dual-run route (EDITOR_RENOVATION_PLAN.md §2) must
 * be registered with EXACTLY the same middleware chain as the legacy
 * /Project/:Project_id route (rate limit → capabilities → read guard →
 * loadEditor). We assert the contract against the router source (importing
 * router.mjs in unit scope is not viable — it drags in queues/redis/mongo),
 * and the live e2e suite (specs/editor/dual-run) proves behavior on both URLs.
 */
import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const routerSrc = fs.readFileSync(
  path.join(here, '../../../../app/src/router.mjs'),
  'utf8'
)

function chainFor(routeLiteral) {
  // the router registers the editor page routes in for-loops that end at
  // the loadEditor handler; grab the block containing our literal.
  const blocks =
    routerSrc.match(/for \(const route of \[[\s\S]*?ProjectController\.loadEditor\s*\)\s*\}/g) ||
    []
  return blocks.find(b => b.includes(routeLiteral))
}

describe('editor renovation P0 — /editor dual-run route contract', () => {
  it('registers /editor/:Project_id and the detached variant', () => {
    expect(routerSrc).toContain("'/editor/:Project_id'")
    expect(routerSrc).toContain("'/editor/:Project_id/:detachRole(detacher|detached)'")
  })

  it('legacy /Project/:Project_id routes still registered (fallback route)', () => {
    expect(routerSrc).toContain("'/Project/:Project_id'")
    expect(routerSrc).toContain("'/Project/:Project_id/:detachRole(detacher|detached)'")
  })

  it('editor chain = same middleware as the legacy chain (order matters)', () => {
    const legacy = chainFor("'/Project/:Project_id'")
    const editor = chainFor("'/editor/:Project_id'")
    expect(legacy, 'legacy route loop not found').toBeTruthy()
    expect(editor, 'editor route loop not found').toBeTruthy()

    const chainTokens = [
      'RateLimiterMiddleware.rateLimit(openProjectRateLimiter',
      'AsyncLocalStorage.middleware',
      'PermissionsController.useCapabilities()',
      'AuthorizationMiddleware.ensureUserCanReadProject',
      'ProjectController.loadEditor',
    ]
    for (const t of chainTokens) {
      expect(legacy, `legacy chain missing "${t}"`).toContain(t)
      expect(editor, `editor chain missing "${t}"`).toContain(t)
    }
    // same relative order in both chains (exact positions differ — block sizes differ)
    for (const src of [legacy, editor]) {
      const pos = chainTokens.map(t => src.indexOf(t))
      expect(pos.every(p => p >= 0), 'a middleware token appears more than once or not at all').toBe(true)
      expect(
        JSON.stringify(pos),
        'middleware order in chain must be rate-limit → capabilities → read-guard → loadEditor'
      ).toBe(JSON.stringify([...pos].sort((a, b) => a - b)))
    }
  })
})
