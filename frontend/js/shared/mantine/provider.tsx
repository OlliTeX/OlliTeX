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

  // R (owner 2026-10-10, WCAG contrast gate; dark half completed 2026-10-11
  // in the B wave): Mantine resolves the anchor from theme.primaryColor
  // (default-css-variables-resolver: light → primaryColor[primaryShade],
  // dark → primaryColor-4); with the instance Appearance patch that lands on
  // blue-6 #228be6 = 3.56:1 on white (< 4.5 AA text). The dimmed var
  // (default #828282-#868e96) fails AA for normal text on the dark hub
  // tables too (≈ 4.0:1 on #242424 — the hub-a11y gate caught it): pin per
  // scheme to AA-passing steps (light: blue-8 #1971c2 5.02:1, dimmed
  // gray-7 #495057; dark: blue-4, dimmed gray-5 #a0a8b0 ≈ 6.4:1 on
  // dark-6/7 surfaces): <Anchor>, <Text component="a">, every Mantine link
  // + <Text c="dimmed"> flip at once.
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
          // dimmed (subdued text) per scheme: light wants a DARKER gray
          // (gray-7), dark wants a LIGHTER one (gray-5) — the R dark
          // half originally left dimmed at its default, which is ≈ 4.0:1 on
          // the #242424 hub table surface (below AA 4.5): the hub-a11y
          // gate (specs/parity/hub-a11y) caught it in the B wave.
          '--mantine-color-dimmed':
            scheme === 'dark' ? 'var(--mantine-color-gray-5)' : 'var(--mantine-color-gray-7)',
          // 2026-10-11 (hub-a11y dual-scheme gate): settings-shell active nav
          // entry = blue-8 on blue-light tint → 4.07:1 light / ≈2.5:1 dark
          // (both < AA 4.5). Per-scheme pin: light → blue-9 (4.93:1 on
          // blue-0/1), dark → blue-1 (#d0ebff, 10.6:1 on the dark tint).
          '--ollitex-settings-nav-active-color':
            scheme === 'dark' ? 'var(--mantine-color-blue-1)' : 'var(--mantine-color-blue-9)',
          // 2026-10-11 (dual-scheme gate): success/status TEXT (c="teal") —
          // teal-4/5 is below AA on the light cards (2.5:1, health leaf).
          '--ollitex-ok-text':
            scheme === 'dark' ? 'var(--mantine-color-teal-3)' : 'var(--mantine-color-teal-9)',
          // default primary FILL: light blue-6 + white = 3.55:1 (stats "Save")
          // → light uses blue-8 (white 5.02:1). The instance Appearance patch
          // maps primaryColor → 'blue', so the button actually reads
          // --mantine-color-blue-filled (probed 2026-10-11) — pin the whole
          // blue filled family, not just the primary alias.
          ...(scheme === 'dark'
            ? {}
            : {
                '--mantine-color-primary-filled': 'var(--mantine-color-blue-8)',
                '--mantine-color-blue-filled': 'var(--mantine-color-blue-8)',
                '--mantine-color-blue-filled-hover': 'var(--mantine-color-blue-9)',
                '--mantine-color-blue-filled-pressed': 'var(--mantine-color-blue-10)',
              }),
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
      // 2026-10-11: key off the SAME attribute the CSS cascade uses
      // (data-mantine-color-scheme on <html>) rather than the JS state, which
      // can lag one render behind the token scheme (body[data-theme] flip was
      // observed stale in the hub-a11y diagnostics); fall back to state.
      const attr = el.getAttribute('data-mantine-color-scheme')
      const dark = attr ? attr === 'dark' : currentColorScheme() === 'dark'
      el.style.setProperty('--mantine-color-anchor', dark ? 'var(--mantine-color-blue-4)' : 'var(--mantine-color-blue-8)')
      // dimmed (subdued text) per scheme — B wave 2026-10-11: dark now gets
      // the LIGHTER accessible step (gray-5 ≈ 6.4:1 on the dark table
      // surfaces) instead of removing the override (default ≈ 4.0:1 fails
      // the hub-a11y gate)
      el.style.setProperty('--mantine-color-dimmed', dark ? 'var(--mantine-color-gray-5)' : 'var(--mantine-color-gray-7)')
      // settings-shell active nav text (2026-10-11, dual-scheme AA pin).
      // NO scheme-neutral value exists: the active tint is LIGHT in the light
      // scheme (needs dark text, blue-9 4.93:1) and DARK in the dark scheme
      // (needs light text, blue-1 10.6:1 on #0c3256; blue-9 measured 2.14:1
      // there — gate: dual-scheme hub-a11y). The inline var wins the cascade
      // over the injected :root style, so it must be scheme-accurate here.
      el.style.setProperty('--ollitex-settings-nav-active-color', dark ? 'var(--mantine-color-blue-1)' : 'var(--mantine-color-blue-9)')
      // success/status TEXT (c="teal" usage class): teal-4/5 on white ≈ 2.5:1
      // (hub-a11y light). light → THIS scale's teal-9 (#087f5b, 4.98:1 on white — teal-6 is a MID
      // step here ≈ 2.5:1, measured 2026-10-11); dark →
      // teal-3 (#63e6be, bright step holds on the dark cards).
      el.style.setProperty('--ollitex-ok-text', dark ? 'var(--mantine-color-teal-3)' : 'var(--mantine-color-teal-9)')
      // default primary FILL (e.g. stats "Save" / blue-6 + white = 3.55:1,
      // dual-scheme gate light): light → blue-8 fill (white 5.02:1). Dark
      // keeps Mantine's light-blue fill (re-checked by the gate per round).
      if (!dark) {
        el.style.setProperty('--mantine-color-primary-filled', 'var(--mantine-color-blue-8)')
        el.style.setProperty('--mantine-color-blue-filled', 'var(--mantine-color-blue-8)')
        el.style.setProperty('--mantine-color-blue-filled-hover', 'var(--mantine-color-blue-9)')
        el.style.setProperty('--mantine-color-blue-filled-pressed', 'var(--mantine-color-blue-10)')
      }
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
