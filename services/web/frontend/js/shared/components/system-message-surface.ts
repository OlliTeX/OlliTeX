import type { SystemMessage } from '../../../../types/system-message'

/**
 * #17b (owner 2026-09-13): per-message placement.
 *
 * Selectable surfaces:
 *  - 'editor' → the IDE (/editor/:id, /project/:id)
 *  - 'hub'    → /hub (workspace + admin areas)
 *  - 'auth'   → login / register / password reset
 *
 * 'app' is a detection-only surface: any other app page (classic project
 * list, legacy user settings, library pages, …) receives ONLY messages
 * whose placement list is empty ("All pages").
 *
 * Pure functions so they can be unit-tested without a DOM.
 */
export type MessageSurface = 'editor' | 'hub' | 'auth' | 'app'

const AUTH_PATH_RE = /^\/(login|register|user\/(reset|forgot)-password)/
const EDITOR_PATH_RE = /^\/(editor|project)\/[^/]+/

export function detectMessageSurface(pathname: string): MessageSurface {
  if (AUTH_PATH_RE.test(pathname)) return 'auth'
  if (EDITOR_PATH_RE.test(pathname)) return 'editor'
  if (pathname === '/hub' || pathname.startsWith('/hub/')) return 'hub'
  return 'app'
}

/**
 * Legacy messages carry no `placements` (or an empty list) → visible on
 * every surface, preserving pre-#17b behavior.
 */
export function messageVisibleForSurface(
  message: Pick<SystemMessage, 'placements'>,
  surface: MessageSurface
): boolean {
  const placements = message.placements
  if (!placements || placements.length === 0) return true
  if (surface === 'app') return false
  return placements.includes(surface)
}

/** UI convenience: 'all' is the exclusive "all pages" sentinel. */
export const PLACEMENT_OPTIONS: ReadonlyArray<{ value: string; label: string }> = [
  { value: 'all', label: 'All pages' },
  { value: 'editor', label: 'Editor (IDE)' },
  { value: 'hub', label: 'Hub' },
  { value: 'auth', label: 'Login & register' },
]

/**
 * Reconcile checkbox state where 'all' is exclusive:
 *  - checking 'all' clears the others
 *  - checking any other clears 'all'
 *  - nothing checked → [] ("all pages")
 */
export function normalizePlacementsChecked(
  checked: Record<string, boolean>
): { all: boolean; placements: string[] } {
  const all = !!checked.all
  const placements = all
    ? []
    : (['editor', 'hub', 'auth'] as const).filter(k => checked[k])
  return { all, placements }
}
