import { describe, it, expect } from 'vitest'
import {
  DEFAULT_HUB_THEME,
  makePalette,
  buildThemePatch,
  cssVarsFor,
  validateHubThemeDoc,
} from '../../frontend/js/hub/hub-theme'
import type { HubThemeDoc } from '../../frontend/js/hub/hub-theme'

describe('hub theme logic (Appearance section)', () => {
  it('makePalette produces exactly 10 steps (Mantine requirement)', () => {
    const p = makePalette('#0f8b4c')
    expect(p).toHaveLength(10)
    for (const c of p) expect(c).toMatch(/^#[0-9a-f]{6}$/)
    // documented contract: steps 0..4 base→black, 5..9 black→white
    const lum = (hex: string) => {
      const n = parseInt(hex.slice(1), 16)
      return ((n >> 16) & 255) + ((n >> 8) & 255) + (n & 255)
    }
    expect(lum(p[5])).toBeLessThanOrEqual(lum(p[0]))
    expect(lum(p[5])).toBeLessThanOrEqual(lum(p[9]))
  })

  it('makePalette accepts 3- and 6-char hex', () => {
    expect(makePalette('#f00')).toHaveLength(10)
    expect(makePalette('#ff0000')).toHaveLength(10)
  })

  it('buildThemePatch is null without a theme, and valid otherwise', () => {
    expect(buildThemePatch(null)).toBeNull()
    const patch = buildThemePatch(DEFAULT_HUB_THEME) as any
    expect(patch).toBeTruthy()
    expect(patch.colors.ollitex).toHaveLength(10)
    expect(patch.fontFamily).toBeTruthy()
    expect(patch.borderRadius).toBeGreaterThanOrEqual(2)
    expect(patch.borderRadius).toBeLessThanOrEqual(24)
    // button text color follows the runtime scheme (per-scheme resolution)
    expect(patch.components.Button.styles.root({ colorScheme: 'light' }).color).toBeTruthy()
  })

  it('buildThemePatch clamps font size and radius to sane bounds', () => {
    const t: HubThemeDoc = {
      version: 1,
      light: { ...DEFAULT_HUB_THEME.light, fontSize: 99, radius: 99 },
      dark: { ...DEFAULT_HUB_THEME.dark },
    }
    const patch = buildThemePatch(t) as any
    expect(patch.defaultFontSize).toBeLessThanOrEqual(22)
    expect(patch.borderRadius).toBeLessThanOrEqual(24)
  })

  it('cssVarsFor resolves per-scheme values and font size', () => {
    const light = cssVarsFor(DEFAULT_HUB_THEME, false)
    const dark = cssVarsFor(DEFAULT_HUB_THEME, true)
    expect(light['--mantine-color-body']).toBeTruthy()
    expect(dark['--mantine-color-body']).toBeTruthy()
    expect(light.fontSize).toMatch(/px$/)
    expect(cssVarsFor(null, false)).toEqual({})
  })

  it('validateHubThemeDoc accepts a well-formed doc', () => {
    const doc = validateHubThemeDoc(DEFAULT_HUB_THEME)
    expect(doc.light.primary).toBeTruthy()
    expect(doc.dark.text).toBeTruthy()
  })

  it('validateHubThemeDoc rejects invalid colors with a precise error', () => {
    expect(() => validateHubThemeDoc(null)).toThrow(/object with light and dark/)
    const bad: any = { ...DEFAULT_HUB_THEME }
    bad.light = { ...DEFAULT_HUB_THEME.light, primary: 'not-a-color' }
    expect(() => validateHubThemeDoc(bad)).toThrow(/light\.primary/)
    bad.light = { ...DEFAULT_HUB_THEME.light, fontFamily: '' }
    expect(() => validateHubThemeDoc(bad)).toThrow(/fontFamily/)
  })

  it('validateHubThemeDoc clamps numeric fields', () => {
    const bad: any = { ...DEFAULT_HUB_THEME }
    bad.light = { ...DEFAULT_HUB_THEME.light, fontSize: -10, radius: 1000 }
    const doc = validateHubThemeDoc(bad)
    expect(doc.light.fontSize).toBeGreaterThanOrEqual(12)
    expect(doc.light.radius).toBeLessThanOrEqual(24)
  })
})
