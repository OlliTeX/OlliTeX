/**
 * command-palette — source-matching contract baseline (editor renovation P0e,
 * test-debt #1: this surface had ZERO live specs).
 *
 * Freezes the palette's query→source rules (EDITOR_RENOVATION_PLAN.md: the
 * renovated P6 surface must keep exactly this behavior):
 *   - empty query      → EVERY source contributes (defaults path)
 *   - prefix match     → ONLY the sources with that prefix; query minus prefix
 *   - no prefix match  → sources that do not require a prefix
 *   (results sorting score-descending is the hook's job, asserted by e2e)
 */
import { describe, it, expect } from 'vitest'
import { getSourcesMatchingQuery } from '@/features/command-palette/hooks/use-command-palette-results'
import type { CommandPaletteSource } from '@/features/command-palette/types'

function makeSource(overrides: Partial<CommandPaletteSource> = {}): CommandPaletteSource {
  return {
    id: overrides.id || 'source',
    search: q => [{ title: `search:${q}`, onSelect: () => undefined, score: 1 }],
    defaults: () => [{ title: 'default', onSelect: () => undefined, score: 0.9 }],
    ...overrides,
  }
}

describe('command palette — getSourcesMatchingQuery (baseline contract)', () => {
  it('empty query → all sources contribute (defaults path)', () => {
    const registry = makeSource({ id: 'registry', prefix: 'com' })
    const file = makeSource({ id: 'file' })
    const jump = makeSource({ id: 'jump', prefix: 'ln' })
    const matching = getSourcesMatchingQuery('', [registry, file, jump])
    expect(matching).toHaveLength(3)
    for (const m of matching) {
      expect(m.query).toBe('')
    }
    expect(matching.map(m => m.source.id).sort()).toEqual(['file', 'jump', 'registry'])
  })

  it('prefix match → only matching sources, query trimmed of the prefix', () => {
    const registry = makeSource({ id: 'registry', prefix: 'com' })
    const file = makeSource({ id: 'file' })
    const jump = makeSource({ id: 'jump', prefix: 'ln' })

    const com = getSourcesMatchingQuery('com new file', [registry, file, jump])
    expect(com).toHaveLength(1)
    expect(com[0].source.id).toBe('registry')
    expect(com[0].query).toBe('new file')

    const ln = getSourcesMatchingQuery('ln 42', [registry, file, jump])
    expect(ln).toHaveLength(1)
    expect(ln[0].source.id).toBe('jump')
    expect(ln[0].query).toBe('42')
  })

  it('no prefix match → only sources that do not require a prefix', () => {
    const registry = makeSource({ id: 'registry', prefix: 'com' })
    const file = makeSource({ id: 'file' })
    const required = makeSource({ id: 'required', prefix: 'ex', prefixRequired: true })
    const plainRequired = makeSource({ id: 'plain-required', prefixRequired: true })

    const matching = getSourcesMatchingQuery('anything', [
      registry,
      file,
      required,
      plainRequired,
    ])
    expect(matching.map(m => m.source.id).sort()).toEqual(['file', 'registry'])
    for (const m of matching) {
      expect(m.query).toBe('anything')
    }
  })

  it('multiple sources sharing a prefix all contribute', () => {
    const a = makeSource({ id: 'a', prefix: 'com' })
    const b = makeSource({ id: 'b', prefix: 'com' })
    const matching = getSourcesMatchingQuery('com x', [a, b])
    expect(matching).toHaveLength(2)
    expect(matching.every(m => m.query === 'x')).toBe(true)
  })
})
