// overleaf-lab (M2.5): Appearance section (admin-only).
//
// Site settings → GENERAL → Appearance (nav_structure.md §8.8): the admin
// can re-brand the /hub visual theme — all colors for light and dark mode,
// buttons, text, backgrounds, font family, font size and radius — with
// Apply / Reset / Export (JSON) / Import (JSON). The theme is stored as a
// JSON document on the server (PUT /api/hub-theme) and applied live to the
// Mantine theme + hub CSS variables (hub/hub-theme.ts).
import React, { useRef, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Divider,
  Group,
  Slider,
  Stack,
  Text,
  TextInput,
} from '@mantine/core'
import { notify } from '../shared/notify'
import { putJSON, deleteJSON } from '@/infrastructure/fetch-json'
import Icon from '../shared/icons'
import ConfirmModal from '../shared/confirm-modal'
import {
  DEFAULT_HUB_THEME,
  getAppliedHubTheme,
  setAppliedHubTheme,
  validateHubThemeDoc,
} from '../hub/hub-theme'

const COLOR_ROWS: { key: 'primary' | 'background' | 'surface' | 'text' | 'dimmed' | 'border' | 'button' | 'buttonText'; label: string }[] = [
  { key: 'primary', label: 'Primary / accent' },
  { key: 'background', label: 'Page background' },
  { key: 'surface', label: 'Panels & cards' },
  { key: 'text', label: 'Text' },
  { key: 'dimmed', label: 'Dimmed text' },
  { key: 'border', label: 'Borders' },
  { key: 'button', label: 'Button' },
  { key: 'buttonText', label: 'Button text' },
]

function Swatch({ value, onChange, label }: { value: string; onChange: (v: string) => void; label: string }) {
  return (
    <Group gap={10} wrap="nowrap" style={{ width: '100%' }}>
      <span
        aria-label={label}
        style={{
          width: 30,
          height: 30,
          borderRadius: 6,
          border: '1px solid var(--mantine-color-border)',
          background: value,
          display: 'inline-block',
          flexShrink: 0,
          position: 'relative',
          overflow: 'hidden',
        }}
      >
        <input
          type="color"
          value={/^#[0-9a-fA-F]{6}$/.test(value) ? value : '#ffffff'}
          onChange={e => onChange(e.target.value)}
          style={{
            position: 'absolute',
            inset: -8,
            width: 'calc(100% + 16px)',
            height: 'calc(100% + 16px)',
            opacity: 0,
            cursor: 'pointer',
            border: 'none',
            padding: 0,
          }}
        />
      </span>
      <TextInput
        aria-label={label}
        value={value}
        onChange={e => onChange(e.currentTarget.value.trim())}
        style={{ flex: 1, minWidth: 110, maxWidth: 150 }}
        description={label}
        size="xs"
        fw={500}
        fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace"
      />
    </Group>
  )
}

export default function AppearanceSection() {
  const [theme, setTheme] = useState(() => {
    const cur = getAppliedHubTheme()
    return cur
      ? {
          version: 1,
          light: { ...cur.light },
          dark: { ...cur.dark },
        }
      : {
          version: 1,
          light: { ...DEFAULT_HUB_THEME.light },
          dark: { ...DEFAULT_HUB_THEME.dark },
        }
  })
  const [saving, setSaving] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [importText, setImportText] = useState('')
  const [, setImportErr] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const setMode = (mode: 'light' | 'dark', key: string, value: unknown) =>
    setTheme(t => ({ ...t, [mode]: { ...t[mode], [key]: value } }))

  const setTypography = (key: 'fontFamily' | 'fontSize' | 'radius', value: unknown) =>
    setTheme(t => ({ ...t, light: { ...t.light, [key]: value }, dark: { ...t.dark, [key]: value } }))

  const isCustom = getAppliedHubTheme() !== null

  const apply = async () => {
    setSaving(true)
    try {
      const saved = await putJSON('/api/hub-theme', { body: theme })
      setAppliedHubTheme(saved && saved.light ? (saved as any) : theme)
      notify({ message: 'Hub appearance applied to everyone on this instance.', color: 'green' })
    } catch (e: any) {
      notify({ message: 'Could not save the hub theme: ' + ((e && e.message) || 'unknown error'), color: 'red' })
    } finally {
      setSaving(false)
    }
  }

  const reset = async () => {
    setSaving(true)
    try {
      await deleteJSON('/api/hub-theme')
      setTheme({ version: 1, light: { ...DEFAULT_HUB_THEME.light }, dark: { ...DEFAULT_HUB_THEME.dark } })
      setAppliedHubTheme(null)
      notify({ message: 'Hub appearance reset to the OlliTeX defaults.', color: 'green' })
    } catch (e: any) {
      notify({ message: 'Could not reset the hub theme: ' + ((e && e.message) || 'unknown error'), color: 'red' })
    } finally {
      setSaving(false)
    }
  }

  const exportTheme = () => {
    try {
      const blob = new Blob([JSON.stringify(theme, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'ollitex-hub-theme.json'
      document.body.appendChild(a)
      a.click()
      a.remove()
      setTimeout(() => URL.revokeObjectURL(url), 2000)
      notify({ message: 'Theme JSON exported.', color: 'green' })
    } catch (e: any) {
      notify({ message: 'Export failed: ' + ((e && e.message) || 'unknown error'), color: 'red' })
    }
  }

  const commitImport = () => {
    try {
      const doc = validateHubThemeDoc(JSON.parse(importText))
      setTheme({ version: 1, light: { ...doc.light }, dark: { ...doc.dark } })
      setImportOpen(false)
      setImportText('')
      setImportErr(null)
      notify({ message: 'Imported — review, then press Apply.', color: 'teal' })
    } catch (e: any) {
      setImportErr((e && e.message) || 'Invalid theme JSON')
    }
  }

  const pickFile = (f: File | undefined) => {
    if (!f) return
    const reader = new FileReader()
    reader.onload = () => {
      setImportText(String(reader.result || ''))
      setImportErr(null)
      setImportOpen(true)
    }
    reader.onerror = () => setImportErr('Could not read that file.')
    reader.readAsText(f)
  }

  return (
    <div>
      <Stack gap={14}>
        <Alert
          variant="light"
          color="teal"
          icon={<Icon name="palette" size={18} />}
          title="Instance-wide hub theme"
          style={{ fontSize: 13.5 }}
        >
          Changes apply to <b>everyone's</b> /hub (not just you), in both light and dark mode.
          Nothing here affects the editor or any other page. Press <b>Apply</b> to publish, or
          <b> Import/Export</b> to keep a JSON copy on your machine.
        </Alert>

        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: 14 }}>
          {(['light', 'dark'] as const).map(mode => (
            <Card key={mode} padding="md" radius="md" withBorder>
              <Group justify="space-between" mb={6}>
                <Group gap={8}>
                  <Icon name={mode === 'light' ? 'light_mode' : 'dark_mode'} size={18} />
                  <Text size="md" fw={650}>{mode === 'light' ? 'Light mode colors' : 'Dark mode colors'}</Text>
                </Group>
              </Group>
              <Stack gap={9}>
                {COLOR_ROWS.map(row => (
                  <Swatch
                    key={row.key}
                    label={row.label}
                    value={String((theme[mode] as any)[row.key] || '#ffffff')}
                    onChange={v => setMode(mode, row.key, v)}
                  />
                ))}
              </Stack>
            </Card>
          ))}
        </div>

        <Card padding="md" radius="md" withBorder>
          <Group gap={8} mb={8}>
            <Icon name="format_size" size={18} />
            <Text size="md" fw={650}>Typography & shape (both modes)</Text>
          </Group>
          <Stack gap={14}>
            <TextInput
              label="Font family (CSS stack)"
              placeholder="'Noto Sans', -apple-system, sans-serif"
              value={String(theme.light.fontFamily || '')}
              onChange={e => setTypography('fontFamily', e.currentTarget.value)}
              description="e.g. 'Inter', 'Segoe UI', sans-serif — inherited by the whole hub"
            />
            <Group gap={16} wrap="wrap">
              <div style={{ flex: 1, minWidth: 220, maxWidth: 380 }}>
                <Text size="xs" c="dimmed" mb={4}>
                  Base font size — <b>{theme.light.fontSize} px</b>
                </Text>
                <Slider
                  min={12}
                  max={22}
                  step={1}
                  value={Number(theme.light.fontSize) || 16}
                  onChange={v => setTypography('fontSize', v)}
                  marks={[{ value: 12 }, { value: 16 }, { value: 22 }]}
                />
              </div>
              <div style={{ flex: 1, minWidth: 220, maxWidth: 380 }}>
                <Text size="xs" c="dimmed" mb={4}>
                  Corner radius — <b>{theme.light.radius} px</b>
                </Text>
                <Slider
                  min={2}
                  max={24}
                  step={1}
                  value={Number(theme.light.radius) || 8}
                  onChange={v => setTypography('radius', v)}
                  marks={[{ value: 2 }, { value: 8 }, { value: 24 }]}
                />
              </div>
            </Group>
          </Stack>
        </Card>

        <Group gap={10} wrap="wrap">
          <Button leftSection={<Icon name="check" size={16} />} loading={saving} onClick={() => void apply()}>
            Apply to all users
          </Button>
          <Button
            variant="default"
            leftSection={<Icon name="restart_alt" size={16} />}
            loading={saving}
            onClick={() => void reset()}
          >
            Reset to defaults
          </Button>
          <Divider orientation="vertical" style={{ alignSelf: 'stretch' }} />
          <Button variant="subtle" leftSection={<Icon name="upload" size={16} />} onClick={exportTheme}>
            Export JSON
          </Button>
          <Button
            variant="subtle"
            leftSection={<Icon name="download" size={16} />}
            onClick={() => fileRef.current && fileRef.current.click()}
          >
            Import JSON
          </Button>
          <input
            ref={fileRef}
            type="file"
            accept="application/json,.json"
            style={{ display: 'none' }}
            onChange={e => {
              const f = e.currentTarget.files && e.currentTarget.files[0]
              pickFile(f)
              e.currentTarget.value = ''
            }}
          />
        </Group>

        <ConfirmModal
          open={importOpen}
          title="Import hub theme JSON"
          body="Paste the exported theme JSON on the left, or pick a file. The import only fills this editor — nothing is published until you press Apply."
          confirmLabel="Use this theme"
          onCancel={() => {
            setImportOpen(false)
            setImportErr(null)
          }}
          onConfirm={commitImport}
        />
      </Stack>

      {isCustom ? (
        <Text size="xs" c="dimmed" mt={10}>
          The hub is currently showing a custom theme. Use <b>Reset to defaults</b> to return to the standard
          OlliTeX look.
        </Text>
      ) : (
        <Text size="xs" c="dimmed" mt={10}>
          The hub is currently showing the standard OlliTeX theme.
        </Text>
      )}
    </div>
  )
}
