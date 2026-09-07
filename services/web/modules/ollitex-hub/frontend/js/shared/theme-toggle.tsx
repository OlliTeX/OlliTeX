import React, { useState } from 'react'
import { ActionIcon, Group, Tooltip } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import Icon from './icons'
import { OverallTheme, setTheme, storedOverallTheme } from '../../../../../frontend/js/shared/mantine/overall-theme'

/**
 * Dark / Light / System selector for the hub headers.
 * Mirrors the OL account-menu theme toggle: same value space
 * ('' | 'light-' | 'system'), same write path (POST /user/settings).
 * Local apply is immediate; the server call is best-effort with a toast
 * on failure.
 */
const OPTIONS: { value: OverallTheme; label: string; icon: string }[] = [
  { value: '', label: 'Dark', icon: 'dark_mode' },
  { value: 'light-', label: 'Light', icon: 'light_mode' },
  { value: 'system', label: 'Use system theme', icon: 'contrast' },
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
        notifications.show({
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
    <Group gap={2} wrap="nowrap" aria-label="Theme selector">
      {OPTIONS.map(o => (
        <Tooltip key={o.value || 'dark'} label={o.label} withArrow position="bottom">
          <ActionIcon
            variant={active === o.value ? 'filled' : 'subtle'}
            color="ollitex"
            size="md"
            disabled={busy}
            onClick={() => {
              void choose(o.value)
            }}
            aria-pressed={active === o.value}
          >
            <Icon name={o.icon} size={17} />
          </ActionIcon>
        </Tooltip>
      ))}
    </Group>
  )
}
