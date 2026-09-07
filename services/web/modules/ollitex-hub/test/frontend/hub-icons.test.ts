import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'
import { describe, it, expect } from 'vitest'
import { HUB_VALID_ICONS } from '../../frontend/js/hub/icon-names'

/**
 * Icon-font guard (owner review #18 class of bug).
 *
 * The app ships a SLICED Material Symbols font (frontend/fonts/material-symbols/);
 * icon names that are not in the font's ligature table render as .notdef
 * puzzle pieces at runtime. This test walks every hub source file, extracts
 * the icon names we ask for (<Icon name="..."/> and nav `icon: "..."`), and
 * asserts each one exists in the bundled font (committed allowlist
 * frontend/js/hub/icon-names.ts, generated from the woff2 GSUB table).
 */
describe('hub icon names vs bundled Material Symbols font slice', () => {
  const here = path.dirname(fileURLToPath(import.meta.url))
  const root = path.join(here, '..', '..', 'frontend', 'js')

  function walk(dir: string): string[] {
    const out: string[] = []
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, entry.name)
      if (entry.isDirectory()) out.push(...walk(p))
      else if (/\.(tsx?|jsx?)$/.test(entry.name)) out.push(p)
    }
    return out
  }

  function iconNamesIn(file: string): string[] {
    const text = fs.readFileSync(file, 'utf8')
    const found = new Set<string>()
    for (const m of text.matchAll(/Icon name="([a-z0-9_]+)"/g)) found.add(m[1])
    for (const m of text.matchAll(/icon: "([a-z0-9_]+)"/g)) found.add(m[1])
    for (const m of text.matchAll(/icon: '([a-z0-9_]+)'/g)) found.add(m[1])
    return [...found]
  }

  it('the allowlist is non-trivial', () => {
    expect(HUB_VALID_ICONS.size).toBeGreaterThan(1000)
    for (const name of ['layers', 'menu_book', 'keyboard', 'campaign', 'monitoring']) {
      expect(HUB_VALID_ICONS.has(name), `allowlist should contain ${name}`).toBe(true)
    }
  })

  it('every icon used anywhere in the hub exists in the bundled font', () => {
    const missing = new Map<string, string>()
    for (const file of walk(root)) {
      for (const name of iconNamesIn(file)) {
        if (!HUB_VALID_ICONS.has(name)) {
          missing.set(name, path.relative(root, file))
        }
      }
    }
    if (missing.size > 0) {
      const details = [...missing.entries()].map(([n, f]) => `  ${n}  (in ${f})`).join('\n')
      throw new Error(
        `Icons not in the bundled font slice (they render as .notdef puzzle pieces):\n${details}`
      )
    }
  })
})
