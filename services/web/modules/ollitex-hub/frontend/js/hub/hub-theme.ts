// overleaf-lab (M2.5): Appearance — client-side theme engine for /hub.
//
// The admin's custom hub theme (JSON doc, nav_structure.md §8.8) drives:
//   1. a Mantine theme patch (primary palette, fonts, radius, button text)
//      merged on top of the default OlliT theme (provider prop), and
//   2. CSS variables on the hub wrapper (--mantine-color-body/text/dimmed/
//      border/surface) so every hub section that reads those vars re-skins
//      live, in both light and dark modes.
//
// The applied theme is page-level state (not user settings): every visitor
// sees the instance's hub branding; the admin edits it under
// Site settings → GENERAL → Appearance.
import getMeta from '@/utils/meta'
import { OLL_GREEN, OLLITEX_RADIUS } from '../../../../../frontend/js/shared/mantine/palette'

export interface HubMode {
  primary: string
  background: string
  surface: string
  text: string
  dimmed: string
  border: string
  button: string
  buttonText: string
  fontFamily: string
  fontSize: number
  radius: number
}

export interface HubThemeDoc {
  version: number
  light: HubMode
  dark: HubMode
}

const DEFAULT_LIGHT: HubMode = {
  primary: OLL_GREEN[6],
  background: '#ffffff',
  surface: '#f4f5f6',
  text: '#1c2128',
  dimmed: '#5c6b7a',
  border: '#d0d5dd',
  button: OLL_GREEN[6],
  buttonText: '#ffffff',
  fontFamily: "'Noto Sans', -apple-system, 'Segoe UI', sans-serif",
  fontSize: 16,
  radius: OLLITEX_RADIUS,
}

const DEFAULT_DARK: HubMode = {
  primary: OLL_GREEN[6],
  background: '#1b222c',
  surface: '#232b37',
  text: '#e7e9ee',
  dimmed: '#a3adb8',
  border: '#3a4657',
  button: OLL_GREEN[6],
  buttonText: '#ffffff',
  fontFamily: "'Noto Sans', -apple-system, 'Segoe UI', sans-serif",
  fontSize: 16,
  radius: OLLITEX_RADIUS,
}

export const DEFAULT_HUB_THEME: HubThemeDoc = {
  version: 1,
  light: { ...DEFAULT_LIGHT },
  dark: { ...DEFAULT_DARK },
}

/** Read the instance theme from the page meta (null = defaults). */
export function loadInitialHubTheme(): HubThemeDoc | null {
  try {
    const m = (getMeta as any)('ol-hub-theme')
    if (!m || typeof m !== 'object' || !m.light || !m.dark) return null
    return m as HubThemeDoc
  } catch {
    return null
  }
}

/**
 * Page-level applied state. hub-root subscribes and rebuilds its provider
 * patch + CSS variables; the Appearance section writes through on Apply /
 * Reset. (Simple module store — no global registry needed in this bundle.)
 */
let applied: HubThemeDoc | null = loadInitialHubTheme()
const listeners = new Set<(t: HubThemeDoc | null) => void>()

export function getAppliedHubTheme(): HubThemeDoc | null {
  return applied
}

export function setAppliedHubTheme(t: HubThemeDoc | null): void {
  applied = t
  listeners.forEach(fn => {
    try {
      fn(applied)
    } catch {
      // a broken subscriber must not break the store
    }
  })
}

export function onAppliedHubThemeChange(fn: (t: HubThemeDoc | null) => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, Math.round(Number.isFinite(n) ? n : lo)))
}

/** Build a 10-step palette from a base hex (0..5 toward black, 5..9 white). */
export function makePalette(base: string): string[] {
  const raw = (base || '').replace('#', '')
  const hex =
    raw.length === 3
      ? raw
          .split('')
          .map(c => c + c)
          .join('')
      : raw.slice(0, 6)
  const num = parseInt(hex || '098842', 16)
  const r0 = (num >> 16) & 255
  const g0 = (num >> 8) & 255
  const b0 = num & 255
  const out: string[] = []
  for (let i = 0; i < 10; i += 1) {
    const t = Math.abs(5 - i) / 5
    const mix = (c: number) =>
      Math.round(i < 5 ? c * t : c + (255 - c) * t)
    const r = mix(r0)
    const g = mix(g0)
    const b = mix(b0)
    out.push(
      '#' +
        [r, g, b]
          .map(x => x.toString(16).padStart(2, '0'))
          .join('')
    )
  }
  return out
}

/**
 * Mantine theme patch for a custom theme (mode-independent parts).
 * Button text color is per-scheme and is resolved through the styles
 * function (theme.colorScheme is authoritative there).
 */
export function buildThemePatch(t: HubThemeDoc | null): Record<string, unknown> | null {
  if (!t || !t.light || !t.dark) return null
  const font = (t.light.fontFamily || DEFAULT_LIGHT.fontFamily).slice(0, 300)
  const fs = clamp(t.light.fontSize, 12, 22)
  const r = clamp(t.light.radius, 2, 24)
  const primary = makePalette(t.light.primary)
  const buttonColor = (theme: { colorScheme?: string }) =>
    (theme && theme.colorScheme === 'dark' ? t.dark.buttonText : t.light.buttonText) ||
    '#ffffff'
  return {
    colors: { ollitex: primary, success: primary },
    fontFamily: font,
    fontFamilyMonospace: "'DM Mono', 'SFMono-Regular', monospace",
    defaultFontSize: fs,
    borderRadius: r,
    defaultRadius: r,
    headings: { fontFamily: font, fontWeight: 600 },
    components: {
      Button: { styles: { root: (th: any) => ({ color: buttonColor(th) }) } },
    },
  }
}

/**
 * CSS variables for the hub wrapper, per current scheme. These feed every
 * hub surface that reads var(--mantine-color-body/text/dimmed/border).
 */
export function cssVarsFor(
  t: HubThemeDoc | null,
  dark: boolean
): Record<string, string> {
  if (!t || !t.light || !t.dark) return {}
  const m = dark ? t.dark : t.light
  return {
    '--mantine-color-body': m.background,
    '--mantine-color-surface': m.surface,
    '--mantine-color-text': m.text,
    '--mantine-color-dimmed': m.dimmed,
    '--mantine-color-border': m.border,
    '--mantine-color-default-border': m.border,
    fontSize: `${m.fontSize}px`,
  }
}

/** Validate a parsed JSON doc before it may be applied/saved. Throws. */
export function validateHubThemeDoc(input: any): HubThemeDoc {
  if (!input || typeof input !== 'object' || !input.light || !input.dark) {
    throw new Error('theme must be an object with light and dark modes')
  }
  const COLOR = /^#([0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/
  const keys = [
    'primary',
    'background',
    'surface',
    'text',
    'dimmed',
    'border',
    'button',
    'buttonText',
  ]
  const fix = (mode: any, where: string): HubMode => {
    const out: any = { ...DEFAULT_LIGHT }
    for (const k of keys) {
      if (typeof mode[k] !== 'string' || !COLOR.test(mode[k])) {
        throw new Error(`${where}.${k} must be a hex color`)
      }
      out[k] = mode[k]
    }
    if (typeof mode.fontFamily !== 'string' || !mode.fontFamily.trim()) {
      throw new Error(`${where}.fontFamily must be a non-empty string`)
    }
    out.fontFamily = mode.fontFamily.slice(0, 300)
    out.fontSize = clamp(mode.fontSize === undefined ? 16 : Number(mode.fontSize), 12, 22)
    out.radius = clamp(mode.radius === undefined ? 8 : Number(mode.radius), 2, 24)
    return out
  }
  return { version: 1, light: fix(input.light, 'light'), dark: fix(input.dark, 'dark') }
}
