import React, { useMemo } from 'react'
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

export default function OlliTProvider({
  children,
  themePatch,
}: {
  children: React.ReactNode
  themePatch?: Record<string, unknown>
}) {
  // M2.5 Appearance: optional override patch (custom hub theme) merged over
  // the default brand theme. Without a patch this is exactly the previous
  // behaviour, so other bundles are unaffected.
  const theme = useMemo(() => {
    if (!themePatch || typeof themePatch !== 'object') return ollitexTheme
    const colors = {
      ...(ollitexTheme as any).colors,
      ...(themePatch.colors as Record<string, unknown> | undefined),
    }
    const components = {
      ...
        ((ollitexTheme as any).components as Record<string, unknown> | undefined),
      ...((themePatch.components as Record<string, unknown> | undefined) || {}),
    }
    return {
      ...(ollitexTheme as any),
      ...(themePatch as any),
      colors,
      components,
    }
  }, [themePatch])

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
