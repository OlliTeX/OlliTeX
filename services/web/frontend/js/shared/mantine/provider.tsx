import React, { useEffect, useMemo, useState } from 'react'
import { MantineProvider } from '@mantine/core'
import { Notifications } from '@mantine/notifications'
import '@mantine/core/styles.css'
import '@mantine/notifications/styles.css'
import { ollitexTheme } from './ollitex-theme'
import {
  currentColorScheme,
  onColorSchemeChange,
  setTheme,
} from './overall-theme'
import {
  buildThemePatch,
  getAppliedHubTheme,
  onAppliedHubThemeChange,
} from '../../../../modules/ollitex-hub/frontend/js/hub/hub-theme'

/**
 * Shared Mantine 9.6.0 shell for OlliTeX pages (hubs, settings, admin).
 * - brand tokens (OL green primary, neutral surfaces, Noto Sans, 8px radius)
 * - light/dark follows the user's overall theme preference (Dark/Light/
 *   System, ace.overallTheme) live: the colorSchemeManager below is bound
 *   to the shared overall-theme store, so toggling anywhere re-renders the
 *   provider (Mantine subscribes to manager.subscribe)
 * - Mantine notifications baked in for all pages using this provider
 */

// Mantine 9 provider contract: get/set/clear/subscribe/unsubscribe.
// We bridge it to OlliTeX's theme store (source of truth: ace.overallTheme,
// persisted via POST /user/settings).
const hubColorSchemeManager = {
  get: (_default?: unknown) => currentColorScheme(),
  set: (scheme?: unknown) => {
    if (scheme !== 'light' && scheme !== 'dark') return
    if (currentColorScheme() === scheme) return // already applied — no-op
    void setTheme(scheme === 'light' ? 'light-' : '')
  },
  clear: () => {},
  subscribe: (fn: (scheme?: unknown) => void) =>
    onColorSchemeChange(value => fn((value as string) || currentColorScheme())),
  unsubscribe: () => {},
}

// Owner #14/#15 (2026-09-13 editor wave): the instance Appearance theme
// (Site settings → GENERAL → Appearance, /api/hub-theme) now governs
// EVERY Mantine surface, not just /hub — editor chrome, module modals
// (Zotero, equation, settings…), auth pages. One design language per
// instance, admin-controlled. Applied in the shared provider so new
// surfaces inherit it automatically; live-updates when an admin applies
// a theme (onAppliedHubThemeChange), no page reload needed.
function useAppliedThemePatch(): Record<string, unknown> | null {
  const [autoPatch, setAutoPatch] = useState<Record<string, unknown> | null>(
    () => buildThemePatch(getAppliedHubTheme()),
  )
  useEffect(() => {
    setAutoPatch(buildThemePatch(getAppliedHubTheme()))
    return onAppliedHubThemeChange(t =>
      setAutoPatch(buildThemePatch(t))
    )
  }, [])
  return autoPatch
}

export default function OlliTProvider({
  children,
  themePatch,
}: {
  children: React.ReactNode
  themePatch?: Record<string, unknown>
}) {
  const autoPatch = useAppliedThemePatch()
  // an explicit prop (e.g. the hub's own live patch) wins over the
  // provider-wide auto patch; without a custom theme both are null
  const effectivePatch = themePatch || autoPatch || null
  // Portals (modals, dropdowns) mount on <body>, OUTSIDE the MantineProvider
  // wrapper element. Mantine 9 scopes most default/dark variable swaps with
  // [data-mantine-color-scheme] selectors, so portaled surfaces kept the
  // light defaults on a dark page (unreadable headers/inputs — owner #11).
  // Mirror the resolved scheme onto <html> so every descendant portal picks
  // up the same scheme. (Idempotent; the provider attribute stays canonical.)
  React.useEffect(() => {
    const apply = () => {
      const s = hubColorSchemeManager.get('light')
      if (s === 'light' || s === 'dark') {
        document.documentElement.dataset.mantineColorScheme = s
      }
    }
    apply()
    return onColorSchemeChange(apply)
  }, [])

  // M2.5 Appearance: optional override patch (custom hub theme) merged over
  // the default brand theme. Without a patch this is exactly the previous
  // behaviour, so other bundles are unaffected.
  const theme = useMemo(() => {
    if (!effectivePatch || typeof effectivePatch !== 'object')
      return ollitexTheme
    const colors = {
      ...(ollitexTheme as any).colors,
      ...(effectivePatch.colors as Record<string, unknown> | undefined),
    }
    const components = {
      ...
        ((ollitexTheme as any).components as Record<string, unknown> | undefined),
      ...((effectivePatch.components as Record<string, unknown> | undefined) ||
        {}),
    }
    return {
      ...(ollitexTheme as any),
      ...(effectivePatch as any),
      colors,
      components,
    }
  }, [effectivePatch])

  return (
    <MantineProvider
      theme={theme as any}
      defaultColorScheme={currentColorScheme()}
      colorSchemeManager={hubColorSchemeManager}
    >
      <Notifications value={{ position: 'bottom-right', autoClose: true, duration: 4000, zIndex: 1200 }} />
      {children}
    </MantineProvider>
  )
}
