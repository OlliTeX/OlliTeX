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
} from '../../../modules/ollitex-hub/frontend/js/hub/hub-theme'

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

  // R (owner 2026-10-10, WCAG contrast gate): Mantine resolves the anchor
  // from theme.primaryColor (default-css-variables-resolver: light →
  // primaryColor[primaryShade], dark → primaryColor-4); with the instance
  // Appearance patch that lands on blue-6 #228be6 = 3.56:1 on white (< 4.5
  // AA text). The dimmed var (gray-6 #868e96 = 3.32:1) fails AA for normal
  // text too. Pin per scheme to AA-passing steps (blue-8 #1971c2 5.02:1;
  // blue-4 light on dark-7 ≈ 7.9:1; dimmed → gray-7 #495057 ≈ 7.6:1):
  // <Anchor>, <Text component="a">, every Mantine link + <Text c="dimmed">
  // flip at once.
  const anchorSafeTheme = useMemo(() => {
    const t: any = theme
    const base = typeof t.getCssVariables === 'function' ? t.getCssVariables : undefined
    return {
      ...t,
      getCssVariables: (scheme: string) => {
        const vars = base ? (base as any).call(t, scheme) : {}
        return {
          ...vars,
          '--mantine-color-anchor':
            scheme === 'dark' ? 'var(--mantine-color-blue-4)' : 'var(--mantine-color-blue-8)',
          ...(scheme === 'light'
            ? { '--mantine-color-dimmed': 'var(--mantine-color-gray-7)' }
            : {}),
        }
      },
    }
  }, [theme])

  // Runtime enforcement (R): Mantine injects the resolved variables into a
  // <style> tag on :root, which can out-cascade stylesheet overrides. The
  // inline custom properties below are set on documentElement itself, so
  // they win the cascade in ALL surfaces of this provider regardless of
  // injection order or theme identity. Re-applied on every theme or
  // colour-scheme change.
  useEffect(() => {
    const apply = () => {
      const el = typeof document !== 'undefined' ? document.documentElement : null
      if (!el) return
      const dark = currentColorScheme() === 'dark'
      el.style.setProperty('--mantine-color-anchor', dark ? 'var(--mantine-color-blue-4)' : 'var(--mantine-color-blue-8)')
      if (dark) el.style.removeProperty('--mantine-color-dimmed')
      else el.style.setProperty('--mantine-color-dimmed', 'var(--mantine-color-gray-7)')
    }
    apply()
    const offScheme = onColorSchemeChange(apply)
    const offTheme = onAppliedHubThemeChange(() => apply())
    return () => {
      try { offScheme && offScheme() } catch (e) { /* noop */ }
      try { offTheme && offTheme() } catch (e) { /* noop */ }
    }
  }, [theme])

  return (
    <MantineProvider
      theme={anchorSafeTheme as any}
      defaultColorScheme={currentColorScheme()}
      colorSchemeManager={hubColorSchemeManager}
    >
      <Notifications value={{ position: 'bottom-right', autoClose: true, duration: 4000, zIndex: 1200 }} />
      {children}
    </MantineProvider>
  )
}
