/**
 * OlliTeX visual tokens for Mantine 9 — the OL design-system palette
 * (source of truth: the page CSS variables --green-*, --neutral-*).
 *
 * - brand primary: OL green (--green-50 #098842, .../--green-70)
 * - surfaces: the neutral scale (--neutral-10 … --neutral-90) in both
 *   schemes, so the hubs read like the rest of the app (white on light,
 *   #1b222c on dark) instead of generic Mantine grays.
 *
 * Mantine shades: index 0 lightest … 9 darkest.
 */

export const OLL_GREEN: string[] = [
  '#f2faf6', // 0 (light tint)
  '#eaf6ef', // 1 = --green-10
  '#b8dbc8', // 2 = --green-20
  '#86caa5', // 3 = --green-30
  '#53b57f', // 4 = --green-40
  '#2f9e63', // 5
  '#098842', // 6 = --green-50 — brand
  '#1e6b41', // 7 = --green-60
  '#195936', // 8 = --green-70
  '#12402a', // 9
]

export const OL_NEUTRAL: string[] = [
  '#f4f5f6', // 0 = --neutral-10
  '#e7e9ee', // 1 = --neutral-20
  '#d0d5dd', // 2 = --neutral-30
  '#afb5c0', // 3 = --neutral-40
  '#8d96a5', // 4 = --neutral-50
  '#677283', // 5 = --neutral-60
  '#495365', // 6 = --neutral-70
  '#2f3a4c', // 7 = --neutral-80
  '#252e3c', // 8 = --neutral-85
  '#1b222c', // 9 = --neutral-90 (dark-mode surface)
]

export const OLLITEX_RADIUS = 8 // px — the app kit radius (0.5rem)
