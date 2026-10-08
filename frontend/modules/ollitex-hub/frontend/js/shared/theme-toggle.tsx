import React, { useState } from 'react'
import { Group, Tooltip } from '@mantine/core'
import { notify } from './notify'
import Icon from './icons'
import { OverallTheme, setTheme, storedOverallTheme } from '../../../../../js/shared/mantine/overall-theme'

/**
 * Dark / Light / System selector for the hub headers.
 * Mirrors the OL account-menu theme toggle: same value space
 * ('' | 'light-' | 'system'), same write path (POST /user/settings).
 * Local apply is immediate; the server call is best-effort with a toast
 * on failure.
 *
 * AJ-6 (owner 2026-10-08): the three icon RADIO BUTTONS are RETIRED — ONE
 * dropdown is THE theme picker, saved per user, applied on all pages (hub
 * header + the /user-settings / /admin-settings shells render this same
 * component). The per-editor code-theme dropdowns (Appearance tab) are a
 * different setting and stay.
 */
const OPTIONS: { value: OverallTheme; label: string }[] = [
  { value: '', label: 'Dark' },
  { value: 'light-', label: 'Light' },
  { value: 'system', label: 'Use system theme' },
]

export default function ThemeToggle() {
  const [active, setActive] = useState<OverallTheme>(() => storedOverallTheme())
  const [busy, setBusy] = useState(false)

  async function choose(value: OverallTheme) {
    if (busy || value === active) return
    setBusy(true)
    setActive(value)
    try {
      await setTheme(value)
    } catch (err) {
      setActive(storedOverallTheme())
      try {
        notify({
          color: 'red',
          title: 'Theme not saved',
          message: 'Could not save the theme preference. The change is applied for this page only.',
        })
      } catch {
        // notifications are cosmetic; the local switch already happened
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <Group gap={6} wrap="nowrap" aria-label="Theme picker and account">
      {/* AJ-6: the radio buttons live on — the owner wanted the appearance
          RADIO retired in favour of the editor-style dropdown. */}
      <label className="ol-theme-dropdown-wrap" style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
        <span className="sr-only" style={{ position: 'absolute', width: 1, height: 1, overflow: 'hidden', clip: 'rect(0 0 0 0)' }}>
          Theme
        </span>
        <select
          className="ol-theme-dropdown"
          aria-label="Theme"
          value={active}
          disabled={busy}
          style={{
            height: 32,
            padding: '0 8px',
            borderRadius: 8,
            border: '1px solid rgba(120,120,140,0.35)',
            background: 'var(--mantine-color-ollitex-light, transparent)',
            color: 'inherit',
            font: 'inherit',
            fontSize: 13.5,
            cursor: busy ? 'default' : 'pointer',
          }}
          onChange={e => {
            void choose(e.target.value as OverallTheme)
          }}>
          {OPTIONS.map(o => (
            <option key={o.value || 'dark'} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </label>
      {/* 2026-09-08 (owner request): log-out lives in the same group.
          GET /logout is the CE logout route (router.mjs) — a plain
          navigation ends the session and lands on the login page. */}
      <Tooltip label="Log out" withArrow position="bottom">
        <a
          href="/logout"
          aria-label="Log out"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 32,
            height: 32,
            borderRadius: 8,
            textDecoration: 'none',
            color: 'inherit',
          }}>
          <Icon name="logout" size={17} />
        </a>
      </Tooltip>
    </Group>
  )
}
