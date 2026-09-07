import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  Button,
  Group,
  Modal,
  Radio,
  Stack,
  Table,
  Text,
} from '@mantine/core'
import Icon from '../../shared/icons'
import {
  UserSettingsProvider,
  useUserSettingsContext,
} from '@/shared/context/user-settings-context'
import { saveUserSettings } from '@/features/ide-settings/utils/api'
import {
  KEYBINDING_ACTIONS,
  isValidKeyString,
  keyStringFromKeyboardEvent,
} from '@/shared/keybinding-actions'
import type { Keybindings } from '../../../../../types/user-settings'

/**
 * Owner #5a/#5b (2026-09-07): My settings → Key bindings reworked with
 * Mantine (the legacy card relied on bootstrap classes the hub page does
 * not style — the "weird spacing"). All behavior is preserved 1:1 from
 * the legacy card + custom-keybindings-modal:
 *   - mode radios: default / vim / emacs / custom (saved immediately,
 *     `saveUserSettings('mode', …)` — the same store the editor reads)
 *   - customize: full action table with default + current key, rebind
 *     (press-keys capture), clear, preset reset (persisted, reported),
 *     JSON import (validated) / export, Cancel/Apply
 * Custom bindings apply account-wide on top of whichever keymap is used.
 */

const OPTIONS: { value: Keybindings; label: string; desc: string }[] = [
  {
    value: 'default',
    label: 'Default (Overleaf)',
    desc: 'The current standard editor keybindings (Ctrl/Cmd + …).',
  },
  {
    value: 'vim',
    label: 'Vim',
    desc: 'Vim emulation mode (i, Esc, h/j/k/l, dw, gg, G, / …).',
  },
  {
    value: 'emacs',
    label: 'Emacs',
    desc: 'Emacs emulation mode (C-a, C-e, C-f/C-b, C-k, C-w, M-f …).',
  },
  {
    value: 'custom',
    label: 'Custom',
    desc: 'The Overleaf keymap with your own custom bindings added on top (Customize key bindings).',
  },
]

type CustomMap = Record<string, string>

function sortObject(obj: Record<string, string>) {
  return Object.fromEntries(Object.entries(obj).sort(([a], [b]) => a.localeCompare(b)))
}

function Kbd({ v, custom = false }: { v: string; custom?: boolean }) {
  return (
    <span
      style={{
        fontFamily: 'var(--mantine-font-family-mono)',
        fontSize: 12,
        padding: '2px 7px',
        borderRadius: 6,
        border: '1px solid var(--mantine-color-default-border)',
        background: custom
          ? 'rgba(30,107,65,0.1)'
          : 'var(--mantine-color-body-light, rgba(0,0,0,0.03))',
        color: custom ? '#1e6b41' : 'var(--mantine-color-text)',
        whiteSpace: 'nowrap',
      }}
    >
      {v}
    </span>
  )
}

function KeybindingsInner() {
  const { userSettings, setUserSettings } = useUserSettingsContext()
  const mode = (userSettings?.mode as Keybindings) || 'default'
  const [customOpen, setCustomOpen] = useState(false)

  const change = (value: Keybindings) => {
    setUserSettings({ ...userSettings, mode: value })
    void saveUserSettings('mode', value).catch(() => undefined)
  }

  return (
    <Stack gap="md">
      <Group gap="xs" justify="space-between" wrap="wrap" align="flex-start">
        <div style={{ maxWidth: 560 }}>
          <Text size="sm" c="dimmed">
            Editor key bindings. Pick the base keymap for the source editor — it
            applies to every project you work in.
          </Text>
        </div>
        <Button
          size="sm"
          variant="light"
          color="ollitex"
          leftSection={<Icon name="tune" size={16} />}
          onClick={() => setCustomOpen(true)}
        >
          Customize key bindings…
        </Button>
      </Group>
      <Radio.Group
        value={mode}
        onChange={(value) => change((value as Keybindings) || 'default')}
        label="Key bindings"
        style={{ maxWidth: 640 }}
      >
        <Stack gap={8}>
          {OPTIONS.map(o => (
            <Radio
              key={o.value}
              value={o.value}
              label={
                <span style={{ display: 'block' }}>
                  <span style={{ fontWeight: 600 }}>{o.label} </span>
                  <span style={{ color: 'var(--mantine-color-text-dimmed)', fontWeight: 400 }}>({o.desc})</span>
                </span>
              }
            />
          ))}
        </Stack>
      </Radio.Group>
      <Text size="xs" c="dimmed">
        Changes save immediately and apply on the next project load (an open
        editor picks them up right away). Custom bindings layer on top of
        whichever keymap is selected.
      </Text>
      {customOpen ? <CustomBindingsModal onClose={() => setCustomOpen(false)} /> : null}
    </Stack>
  )
}

// ───────────────────────────── custom bindings modal ────────────────────
function CustomBindingsModal({ onClose }: { onClose: () => void }) {
  const { userSettings, setUserSettings } = useUserSettingsContext()

  const currentMode: Keybindings = (userSettings?.mode as Keybindings) || 'default'
  const storedCustom = useMemo(
    () =>
      Object.fromEntries(
        Object.entries(userSettings?.customKeybindings || {}).filter(
          ([, v]: [string, unknown]) => typeof v === 'string' && v.length > 0
        )
      ) as CustomMap,
    [userSettings]
  )

  const [draft, setDraft] = useState<CustomMap>({})
  const [resetPreset, setResetPreset] = useState<Keybindings>('default')
  const [capturing, setCapturing] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const [resetApplied, setResetApplied] = useState(false)
  const [savedFlash, setSavedFlash] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    setDraft({ ...storedCustom })
    setResetPreset(currentMode === 'custom' ? 'default' : currentMode)
    setCapturing(null)
    setSaved(false)
    setResetApplied(false)
    setSavedFlash(false)
    // init-on-open only
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const startCapturing = (id: string) => {
    setCapturing(id)
    const MODS = new Set(['Control', 'Shift', 'Alt', 'Meta', 'OS', 'Process', 'AltGraph'])
    const handler = (event: KeyboardEvent) => {
      if (MODS.has(event.key)) return
      window.removeEventListener('keydown', handler, true)
      event.preventDefault()
      event.stopPropagation()
      if (event.key === 'Escape') {
        setCapturing(null)
        return
      }
      const ks = keyStringFromKeyboardEvent(event)
      if (ks) setDraft(d => ({ ...d, [id]: ks }))
      setCapturing(null)
    }
    window.addEventListener('keydown', handler, true)
  }

  const baseOf = (m: Keybindings) => (m === 'custom' ? 'default' : m)
  const changed =
    JSON.stringify(sortObject(draft)) !== JSON.stringify(sortObject(storedCustom)) ||
    baseOf(draftMode()) !== baseOf(currentMode)

  function draftMode(): Keybindings {
    // the modal reset row is the authoritative "applied mode"
    return resetPreset === 'custom' ? 'default' : resetPreset
  }

  const doExport = () => {
    const payload = { version: 1, mode: baseOf(resetPreset), customKeybindings: sortObject(draft) }
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'ollitex-keybindings.json'
    a.click()
    URL.revokeObjectURL(url)
  }

  const doImportFile = async (file: File) => {
    try {
      const text = await file.text()
      const json = JSON.parse(text)
      const src: Record<string, unknown> =
        json && typeof json === 'object'
          ? typeof json.customKeybindings === 'object' && json.customKeybindings !== null
            ? (json.customKeybindings as Record<string, unknown>)
            : typeof json.custom === 'object' && json.custom !== null
              ? (json.custom as Record<string, unknown>)
              : json
          : {}
      const known = new Set(KEYBINDING_ACTIONS.map(a => a.id))
      const next: CustomMap = {}
      let invalid = 0
      for (const [k, v] of Object.entries(src || {})) {
        if (!known.has(k) || !isValidKeyString(v)) {
          invalid += 1
          continue
        }
        next[k] = v as string
      }
      setDraft(prev => ({ ...prev, ...next }))
      if (json && typeof json === 'object' && ['default', 'vim', 'emacs', 'custom'].includes(json.mode)) {
        const m = json.mode as Keybindings
        setResetPreset(m === 'custom' ? 'default' : m)
      }
      setSavedFlash(true)
      setTimeout(() => setSavedFlash(false), 2500)
      console.info(`custom keybindings import: ${Object.keys(next).length} bound, ${invalid} skipped`)
    } catch (err) {
      setSavedFlash(true)
      setTimeout(() => setSavedFlash(false), 2500)
      console.warn('custom keybindings import failed', err)
    }
  }

  const doApply = async () => {
    try {
      const preset = baseOf(resetPreset)
      if (preset !== baseOf(currentMode)) {
        await saveUserSettings('mode', preset)
        setUserSettings({ ...userSettings, mode: preset })
      }
      if (JSON.stringify(sortObject(draft)) !== JSON.stringify(sortObject(storedCustom))) {
        const payload: Record<string, string | null> = {}
        for (const a of KEYBINDING_ACTIONS) {
          payload[a.id] = draft[a.id] || null
        }
        await saveUserSettings('customKeybindings', payload)
        setUserSettings({ ...userSettings, customKeybindings: { ...draft } })
      }
      setSaved(true)
      setTimeout(onClose, 500)
    } catch (err) {
      console.warn('saving custom key bindings failed', err)
    }
  }

  const doResetNow = async () => {
    try {
      const preset = baseOf(resetPreset)
      await saveUserSettings('mode', preset)
      const payload: Record<string, string | null> = {}
      for (const a of KEYBINDING_ACTIONS) {
        payload[a.id] = null
      }
      await saveUserSettings('customKeybindings', payload)
      setUserSettings({ ...userSettings, mode: preset, customKeybindings: {} })
      setDraft({})
      setResetApplied(true)
      setSaved(false)
    } catch (err) {
      console.warn('reset to preset failed', err)
    }
  }

  return (
    <Modal opened onClose={onClose} size="lg" title={<Text fw={700}>Customize key bindings</Text>} withinPortal data-testid="custom-keybindings-modal">
      <Stack gap="md">
        <Text size="sm" c="dimmed">
          Rebind any action to your own keys. The defaults are the Overleaf
          bindings; “Current” is yours (empty = stock default). Custom bindings
          apply in the editor on top of whichever keymap you use (Overleaf /
          Vim / Emacs).
        </Text>
        <Table striped withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Action</Table.Th>
              <Table.Th style={{ width: 170 }}>Default</Table.Th>
              <Table.Th style={{ width: 170 }}>Current</Table.Th>
              <Table.Th style={{ width: 220, textAlign: 'right' }}>Change</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {KEYBINDING_ACTIONS.map(action => {
              const value = draft[action.id] || ''
              const isCapturing = capturing === action.id
              return (
                <Table.Tr key={action.id}>
                  <Table.Td>
                    <Text size="sm">{action.label}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Kbd v={action.defaultKey} />
                  </Table.Td>
                  <Table.Td>
                    {value ? (
                      <Kbd v={value} custom />
                    ) : (
                      <Text size="sm" c="dimmed">— default</Text>
                    )}
                  </Table.Td>
                  <Table.Td style={{ textAlign: 'right' }}>
                    <Group gap={6} justify="flex-end" wrap="nowrap">
                      {isCapturing ? (
                        <Text size="xs" c="teal" fw={600} tt="uppercase">
                          Press keys… (Esc cancels)
                        </Text>
                      ) : (
                        <Button size="xs" variant="default" onClick={() => startCapturing(action.id)}>
                          Rebind
                        </Button>
                      )}
                      {value ? (
                        <Button size="xs" variant="subtle" color="grey" onClick={() => setDraft(d => ({ ...d, [action.id]: '' }))}>
                          Clear
                        </Button>
                      ) : null}
                    </Group>
                  </Table.Td>
                </Table.Tr>
              )
            })}
          </Table.Tbody>
        </Table>

        <Group gap="xs" wrap="wrap" align="center">
          <Text size="sm" fw={700} pr="xs">Reset to default:</Text>
          <Radio.Group
            inline
            size="xs"
            value={resetPreset}
            onChange={v => setResetPreset((v as Keybindings) || 'default')}
          >
            <Radio value="default" label="Overleaf (default)" />
            <Radio value="vim" label="Vim" />
            <Radio value="emacs" label="Emacs" />
          </Radio.Group>
          <Button size="xs" variant="light" color="ollitex" onClick={() => void doResetNow()}>
            Reset now
          </Button>
          {resetApplied ? (
            <Text size="xs" c="teal">✓ Applied as your account default — the editor uses it on the next project load.</Text>
          ) : null}
        </Group>

        <Group gap="xs" wrap="wrap" align="center" justify="space-between">
          <Group gap={6} wrap="nowrap">
            <Button size="xs" variant="default" leftSection={<Icon name="upload_file" size={14} />} onClick={() => fileRef.current?.click()}>
              Import JSON
            </Button>
            <Button size="xs" variant="default" leftSection={<Icon name="download" size={14} />} onClick={doExport}>
              Export JSON
            </Button>
            <input
              ref={fileRef}
              type="file"
              accept="application/json,.json"
              style={{ display: 'none' }}
              onChange={e => {
                const f = e.target.files && e.target.files[0]
                if (f) void doImportFile(f)
                e.currentTarget.value = ''
              }}
            />
            {savedFlash ? <Text size="xs" c="teal">Imported (invalid rows skipped).</Text> : null}
          </Group>
          {!saved ? (
            <Text size="xs" c="dimmed">
              Saved account-wide; the editor applies it on the next project load.
            </Text>
          ) : (
            <Text size="xs" c="teal">Saved.</Text>
          )}
        </Group>

        <Group justify="flex-end" gap="xs">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button color="ollitex" disabled={!changed && !saved} onClick={() => void doApply()}>
            Apply
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}

export default function KeybindingsSection() {
  return (
    <UserSettingsProvider>
      <KeybindingsInner />
    </UserSettingsProvider>
  )
}
