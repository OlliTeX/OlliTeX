/**
 * Theme picker (J, 2026-10-09):
 *
 * The old three-option ThemeSwitch (Dark / Light / System) now lives inside a
 * single `ThemeToggle` menu that ADDITIONALLY lists the vendored editor color
 * themes (J) from `js/features/source-editor/themes/cm6/index.json` — the
 * themes sourced from the react-codemirror reference repo (see CREDITS.md).
 *
 * Picking a code theme:
 *   1. saves it as `overallTheme` (same /user/settings endpoint as before),
 *      which resolves the chrome light/dark from the theme's own dark flag
 *      (js/shared/mantine/overall-theme.ts);
 *   2. is applied in the editor via shared/hooks/use-active-editor-theme,
 *      where an explicit code theme wins over the paired
 *      editorLightTheme/editorDarkTheme settings.
 *
 * Widget: a Mantine Menu — unlike a <select>, the value (the cm6 registry
 * name, e.g. `tokyo-night`) is independent of the display label
 * ("Tokyo Night"), and the current dark/light chip is possible.
 *
 * No React state: the selection is read from the ol-userSettings meta +
 * the overall-theme store (body data-theme), so re-renders stay driven by
 * the Mantine colorScheme subscription.
 */

import type { ReactNode } from 'react'
import { Button, Menu } from '@mantine/core'
import Icon from './icons'
import {
  type OverallTheme,
  isCodeTheme,
  setTheme,
} from '../../../../../js/shared/mantine/overall-theme'
import {
  themes as VENDORED_THEMES,
} from '../../../../../js/features/source-editor/themes/cm6/index.json'

const CHROME_OPTIONS: { value: OverallTheme; label: string; icon: ReactNode }[] = [
  { value: '', label: 'Dark', icon: <Icon name="bedtime" size={20} /> },
  { value: 'light-', label: 'Light', icon: <Icon name="light_mode" size={20} /> },
  { value: 'system', label: 'System', icon: <Icon name="brightness_4" size={20} /> },
]

interface Selection {
  value: OverallTheme
  label: string
}

function currentSelection(): Selection {
  try {
    const el = document.querySelector('meta[name=ol-userSettings]')
    const raw = el ? el.getAttribute('content') : null
    if (raw) {
      const v = (JSON.parse(raw) as { overallTheme?: string }).overallTheme
      const opt = CHROME_OPTIONS.find(o => o.value === (v === undefined ? '' : v))
      if (opt) return { value: opt.value, label: opt.label }
      if (typeof v === 'string') {
        const theme = VENDORED_THEMES.find(t => t.name === v)
        if (theme) return { value: v, label: theme.label }
      }
    }
  } catch {
    // fall through to the default
  }
  return { value: '', label: 'Dark' }
}

// Lazily populated with the selected theme's palette (cosmetic chip only).
const PALETTES: Record<string, string | null> = {}

function loadPalette(name: string) {
  if (name in PALETTES) return
  PALETTES[name] = null
  import(/* webpackChunkName: "cm6-theme" */ '../../../../../js/features/source-editor/themes/cm6/' + name + '.json')
    .then(m => {
      const mod = (m as { default?: { theme?: Record<string, Record<string, string>> } }).default ?? m
      const bg = mod.theme?.['&']?.backgroundColor
      PALETTES[name] = bg || null
    })
    .catch(() => {
      PALETTES[name] = null
    })
}

export function ThemeToggle() {
  const current = currentSelection()
  const isCode = isCodeTheme(current.value)
  if (isCode) loadPalette(current.value)
  const chip = isCode ? PALETTES[current.value] : null

  return (
    <Menu
      position="bottom-end"
      withinPortal
      width={250}
    >
      <Menu.Target>
        <Button variant="subtle" aria-label="Theme" leftSection={
          chip ? (
            <span
              style={{
                width: 12,
                height: 12,
                borderRadius: 999,
                display: 'inline-block',
                background: chip,
                border: '1px solid rgba(128,128,128,0.6)',
              }}
              aria-hidden
            />
          ) : (
            <Icon name="contrast" size={18} />
          )
        }>
          {current.label}
        </Button>
      </Menu.Target>
      <Menu.Dropdown>
        <Menu.Label>Theme</Menu.Label>
        {CHROME_OPTIONS.map(opt => (
          <Menu.Item
            key={opt.value === '' ? 'dark' : opt.value}
            leftSection={opt.icon}
            onClick={() => setTheme(opt.value).catch(() => {
              // save failures surface via existing toast layers upstream
            })}
            style={current.value === opt.value && !isCode ? { fontWeight: 700 } : undefined}
          >
            {opt.label}
          </Menu.Item>
        ))}
        <Menu.Divider />
        <Menu.Label>Editor themes</Menu.Label>
        {VENDORED_THEMES.map(t => (
          <Menu.Item
            key={t.name}
            onClick={() => setTheme(t.name).catch(() => {
              // cosmetic; logged upstream
            })}
            style={current.value === t.name ? { fontWeight: 700 } : undefined}
          >
            {t.label}
            <span
              style={{
                marginLeft: 10,
                width: 10,
                height: 10,
                borderRadius: 999,
                display: 'inline-block',
                verticalAlign: 'baseline',
                background: t.dark ? '#2a2e33' : '#f2f3f5',
                border: '1px solid rgba(128,128,128,0.6)',
              }}
              aria-label={t.dark ? 'dark theme' : 'light theme'}
            />
          </Menu.Item>
        ))}
      </Menu.Dropdown>
    </Menu>
  )
}

/**
 * Kept for surfaces that want the classic three-option switch only.
 */
export function ThemeSwitch() {
  const current = currentSelection()
  return (
    <Menu position="bottom-end" width={200}>
      <Menu.Target>
        <Button variant="subtle" leftSection={<Icon name="contrast" size={18} />}>
          {current.label}
        </Button>
      </Menu.Target>
      <Menu.Dropdown>
        {CHROME_OPTIONS.map(opt => (
          <Menu.Item
            key={opt.value === '' ? 'dark' : opt.value}
            leftSection={opt.icon}
            onClick={() => setTheme(opt.value).catch(() => {
              // ignore
            })}
            style={current.value === opt.value ? { fontWeight: 700 } : undefined}
          >
            {opt.label}
          </Menu.Item>
        ))}
      </Menu.Dropdown>
    </Menu>
  )
}

export default ThemeToggle
