import { createTheme } from '@mantine/core'
import { OLL_GREEN, OL_NEUTRAL, OLLITEX_RADIUS } from './palette'

/**
 * OlliTeX Mantine 9 theme — one brand, both light and dark.
 *
 * - primaryColor 'ollitex': OL green (--green-50), the app's existing brand
 *   accent, in both schemes (light: shade 6; dark: white text on green-60)
 * - surfaces: OL's neutral scale (white light / #1b222c dark) so the hubs
 *   continue the app's existing color scheme
 * - typography: the app's existing Noto Sans / DM Mono stack
 * - radius 8px to match the existing kit (--kit-radius 0.5rem)
 *
 * Light/dark is driven by OlliTProvider → useColorScheme() (shared/mantine/
 * overall-theme.ts), which follows the user's dark/light/system
 * preference (ace.overallTheme) and body[data-theme].
 */
export const ollitexTheme = createTheme({
  primaryColor: 'ollitex',
  primaryShade: 6,
  colors: {
    ollitex: OLL_GREEN,
    success: OLL_GREEN,
    dark: OL_NEUTRAL,
    gray: OL_NEUTRAL,
    white: Array(10).fill('#ffffff'),
  },
  fontFamily: "'Noto Sans', -apple-system, 'Segoe UI', sans-serif",
  fontFamilyMonospace: "'DM Mono', 'SFMono-Regular', monospace",
  borderRadius: OLLITEX_RADIUS,
  defaultRadius: OLLITEX_RADIUS,
  focusRing: 'auto',
  headings: {
    fontFamily: "'Noto Sans', sans-serif",
    fontWeight: 600,
  },
  other: {
    cursorModifier: 'pointer',
  },
})

export type OllitexTheme = typeof ollitexTheme
