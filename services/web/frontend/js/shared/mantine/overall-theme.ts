import { useSyncExternalStore } from 'react'
import { postJSON } from '@/infrastructure/fetch-json'

/**
 * OlliTeX overall-theme state: 'Dark' (''), 'Light' ('light-'), 'System'
 * ('system') — same value space as ace.overallTheme (see ExpressLocals
 * res.locals.overallThemes and UserController.updateUserSettings).
 *
 * Single source of truth for:
 *   - document.body.dataset.theme  ('default' = dark, 'light') — OL's theming
 *   - the Mantine colorScheme (provider.tsx reads useColorScheme)
 *
 * Reading the current preference uses the ol-userSettings meta tag that the
 * hub (and OL settings) pages already emit.
 */

export type OverallTheme = '' | 'light-' | 'system'
export type ColorScheme = 'light' | 'dark'

const listeners = new Set<() => void>()
let currentScheme: ColorScheme | null = null

export function prefersDark(): boolean {
  try {
    return !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches)
  } catch {
    return true
  }
}

export function storedOverallTheme(): OverallTheme {
  try {
    const el = document.querySelector('meta[name=ol-userSettings]')
    const raw = el ? el.getAttribute('content') : null
    if (!raw) return ''
    const parsed = JSON.parse(raw)
    const v = parsed ? parsed.overallTheme : ''
    if (v === 'light-' || v === 'system') return v
    return ''
  } catch {
    return ''
  }
}

export function schemeForTheme(value: OverallTheme = storedOverallTheme()): ColorScheme {
  if (value === 'light-') return 'light'
  if (value === 'system') return prefersDark() ? 'dark' : 'light'
  return 'dark' // '' (OL default) → dark, matches body[data-theme='default']
}

export function currentColorScheme(): ColorScheme {
  if (currentScheme === null) currentScheme = schemeForTheme()
  return currentScheme
}

function applyScheme(scheme: ColorScheme) {
  if (currentScheme === scheme) return // idempotent: prevents provider↔store loops
  currentScheme = scheme
  try {
    document.body.dataset.theme = scheme === 'light' ? 'light' : 'default'
  } catch {
    // body may not exist during SSR-ish paths; Mantine still gets the value
  }
  listeners.forEach(fn => {
    try {
      fn(scheme) // subscribers (e.g. Mantine's setColorScheme) expect the value
    } catch {
      // a broken listener must not break the theme switch
    }
  })
}

/** Keep the meta tag (the in-page source of truth) in sync with the UI. */
function rememberTheme(value: OverallTheme) {
  try {
    const el = document.querySelector('meta[name=ol-userSettings]')
    if (!el) return
    const json = JSON.parse(el.getAttribute('content') || '{}')
    json.overallTheme = value
    el.setAttribute('content', JSON.stringify(json))
  } catch {
    // non-fatal: the server is the durable store
  }
}

/**
 * Apply locally (immediate) and persist server-side (POST /user/settings).
 * Returns the fetch promise so callers can surface save failures.
 */
export function setTheme(value: OverallTheme): Promise<unknown> {
  applyScheme(schemeForTheme(value))
  rememberTheme(value)
  // fetch-json contract: the payload goes under `body` (overall-theme bug
  // fix 2026-09-07 — the payload used to be passed as a bare option and was
  // silently dropped, so the preference never persisted server-side).
  return postJSON('/user/settings', { body: { overallTheme: value } })
}

export function onColorSchemeChange(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

// System mode: follow the OS while the page is open.
if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
  const mq = window.matchMedia('(prefers-color-scheme: dark)')
  try {
    if (typeof mq.addEventListener === 'function') {
      mq.addEventListener('change', () => {
        if (storedOverallTheme() === 'system') applyScheme(schemeForTheme())
      })
    }
  } catch {
    // older engines: no live tracking; the value is still correct on load
  }
}

// Single writer: on load, mirror the store onto body[data-theme] so OL's
// legacy theming and Mantine agree (this is the authoritative initializer).
if (typeof document !== 'undefined') {
  try {
    if (document.body) {
      applyScheme(currentColorScheme())
    } else {
      document.addEventListener(
        'DOMContentLoaded',
        () => applyScheme(currentColorScheme()),
        { once: true },
      )
    }
  } catch {
    // body may not exist under SSR-ish paths; Mantine still reads the store
  }
}

/** Reactive React binding (stable snapshot — useSyncExternalStore-safe). */
export function useColorScheme(): ColorScheme {
  return useSyncExternalStore(onColorSchemeChange, currentColorScheme)
}
