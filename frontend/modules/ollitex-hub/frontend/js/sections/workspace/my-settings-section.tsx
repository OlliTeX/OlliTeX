import React, { useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Group,
  NativeSelect,
  Radio,
  RadioGroup,
  Stack,
  Switch,
  Tabs,
  Text,
  TextInput,
} from '@mantine/core'
import { notify } from '../../shared/notify'
import { postJSON } from '@/infrastructure/fetch-json'
import { setTheme } from '@/shared/mantine/overall-theme'
import Icon from '../../shared/icons'

function getMetaJson(name: string): any {
  try {
    const el = document.querySelector(`meta[name=${name}]`)
    const c = el?.getAttribute('content')
    return c ? JSON.parse(c) : null
  } catch {
    return null
  }
}

// Editor code-theme options. The editor page exposes these as the
// ol-editorThemes / ol-legacyEditorThemes meta; the /hub page does not,
// so read them when present and fall back to the canonical set (mirrors
// the theme registry; the values are the actual code-theme names stored
// in user settings editorLightTheme/editorDarkTheme).
const LIGHT_EDITOR_THEMES = [
  { value: 'eclipse', label: 'Eclipse' },
  { value: 'overleaf_light', label: 'Overleaf light' },
  { value: 'textmate', label: 'Textmate' },
]
const DARK_EDITOR_THEMES = [
  { value: 'cobalt', label: 'Cobalt' },
  { value: 'dracula', label: 'Dracula' },
  { value: 'monokai', label: 'Monokai' },
  { value: 'overleaf_dark', label: 'Overleaf dark' },
]
const LEGACY_EDITOR_THEMES = [
  'ambiance', 'chaos', 'chrome', 'clouds', 'clouds_midnight',
  'crimson_editor', 'dawn', 'dreamweaver', 'github', 'gob',
  'gruvbox', 'idle_fingers', 'iplastic', 'katzenmilch', 'kr_theme',
  'kuroir', 'merbivore', 'merbivore_soft', 'mono_industrial',
  'nord_dark', 'pastel_on_dark', 'solarized_dark', 'solarized_light',
  'sqlserver', 'terminal', 'tomorrow', 'tomorrow_night',
  'tomorrow_night_blue', 'tomorrow_night_bright',
  'tomorrow_night_eighties', 'twilight', 'vibrant_ink', 'xcode',
].map(v => ({ value: v, label: v.replace(/_/g, ' ') + ' (Legacy)' }))

function themeGroups(metaName: string, fallback: { value: string; label: string }[]) {
  const meta = getMetaJson(metaName)
  if (Array.isArray(meta) && meta.length > 0) {
    return meta.map((m: any) => ({
      value: m.name,
      label: String(m.name).replace(/_/g, ' '),
    }))
  }
  return fallback
}

function useUserSettings() {
  const [settings, setSettings] = useState<any>(() => getMetaJson('ol-userSettings') || {})
  const [user] = useState<any>(() => {
    try {
      return JSON.parse(document.querySelector('meta[name=ol-user]')?.getAttribute('content') || '{}')
    } catch {
      return {}
    }
  })
  const refresh = () => setSettings(getMetaJson('ol-userSettings') || {})
  return { settings, user, refresh }
}

async function saveUserSettings(patch: Record<string, unknown>) {
  try {
    await postJSON('/user/settings', { body: patch })
  } catch (err: any) {
    throw new Error((err?.data?.message as string) || 'Could not save settings.')
  }
}

function AccountTab() {
  const { settings, user, refresh } = useUserSettings()
  const [firstName, setFirstName] = useState(user?.first_name || settings?.first_name || '')
  const [lastName, setLastName] = useState(user?.last_name || settings?.last_name || '')
  const [busy, setBusy] = useState(false)

  const save = async () => {
    setBusy(true)
    try {
      await saveUserSettings({ first_name: firstName, last_name: lastName })
      notify({ message: 'Profile saved.', color: 'teal' })
      refresh()
    } catch (e: any) {
      notify({ message: e.message, color: 'red' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card withBorder paddings="lg" radius="lg">
      <Stack gap="md">
        <Text size="sm" c="dimmed">
          These details appear to collaborators and on shared projects.
        </Text>
        <div>
          <Text size="sm" fw={600} mb={6}>
            Primary email
          </Text>
          <Text size="sm">{user?.email || '—'}</Text>
          <Text size="xs" c="dimmed" mt={4}>
            The primary email was set at sign-up; changing it requires a
            confirmation email flow and is not offered in this build.
          </Text>
        </div>
        <Group gap="md" wrap="wrap">
          <div style={{ minWidth: 200, flex: 1 }}>
            <Text size="sm" fw={600} mb={6}>
              First name
            </Text>
            <TextInput value={firstName} onChange={e => setFirstName(e.currentTarget.value)} aria-label="First name" />
          </div>
          <div style={{ minWidth: 200, flex: 1 }}>
            <Text size="sm" fw={600} mb={6}>
              Last name
            </Text>
            <TextInput value={lastName} onChange={e => setLastName(e.currentTarget.value)} aria-label="Last name" />
          </div>
        </Group>
        <Group>
          <Button color="ollitex" loading={busy} onClick={() => void save()}>
            Save profile
          </Button>
        </Group>
      </Stack>
    </Card>
  )
}

function PasswordTab() {
  const [current, setCurrent] = useState('')
  const [next1, setNext1] = useState('')
  const [next2, setNext2] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const save = async () => {
    if (!current || !next1) {
      setErr('Fill in all three password fields.')
      return
    }
    if (next1 !== next2) {
      setErr('New passwords do not match.')
      return
    }
    setBusy(true)
    setErr(null)
    try {
      await postJSON('/user/password/update', {
        body: { currentPassword: current, newPassword1: next1, newPassword2: next2 },
      })
      setCurrent('')
      setNext1('')
      setNext2('')
      notify({ message: 'Password changed.', color: 'teal' })
    } catch (e: any) {
      setErr((e?.data?.message as string) || 'Password change failed.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card withBorder paddings="lg" radius="lg">
      <Stack gap="md">
        <Text size="sm" c="dimmed">
          Use a strong, unique password. You will stay signed in on this device.
        </Text>
        <div>
          <Text size="sm" fw={600} mb={6}>
            Current password
          </Text>
          <TextInput type="password"
            value={current}
            onChange={e => setCurrent(e.currentTarget.value)}
            placeholder="Current password"
            aria-label="Current password"
          />
        </div>
        <Group gap="md" wrap="wrap">
          <div style={{ minWidth: 200, flex: 1 }}>
            <Text size="sm" fw={600} mb={6}>
              New password
            </Text>
            <TextInput type="password" value={next1} onChange={e => setNext1(e.currentTarget.value)} placeholder="New password" aria-label="New password" />
          </div>
          <div style={{ minWidth: 200, flex: 1 }}>
            <Text size="sm" fw={600} mb={6}>
              Repeat new password
            </Text>
            <TextInput type="password" value={next2} onChange={e => setNext2(e.currentTarget.value)} placeholder="Repeat new password" aria-label="Repeat new password" />
          </div>
        </Group>
        {err ? <Alert color="red" icon={null} variant="light">{err}</Alert> : null}
        <Group>
          <Button color="ollitex" loading={busy} onClick={() => void save()}>
            Change password
          </Button>
        </Group>
      </Stack>
    </Card>
  )
}

function AppearanceTab() {
  const { settings, refresh } = useUserSettings()
  // The store contract (shared/mantine/overall-theme.ts) is the LEGACY value
  // space '' (dark) | 'light-' | 'system'; the Radio values are the friendly
  // 'dark' | 'light' | 'system'. Mapping both ways (owner issue #10: the old
  // code passed the radio value straight to the store, so "Light" resolved
  // to dark and the live theme never matched the saved preference).
  const radioForStored = (v: unknown) =>
    v === 'light-' ? 'light' : v === '' || v == null ? 'dark' : 'system'
  const storedForRadio = (v: string) =>
    v === 'light' ? 'light-' : v === 'dark' ? '' : 'system'
  const [radio, setRadio] = useState<string>(() => radioForStored(settings?.overallTheme))
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setRadio(radioForStored(settings?.overallTheme))
  }, [settings?.overallTheme])

  const save = async (patch: Record<string, unknown>, note: string) => {
    setBusy(true)
    try {
      if ('overallTheme' in patch) {
        // setTheme (shared store) applies locally + writes the meta tag + POSTs
        // the valid stored value ('' | 'light-' | 'system') to /user/settings.
        await setTheme(storedForRadio(String(patch.overallTheme)))
      } else {
        await saveUserSettings(patch)
      }
      notify({ message: note, color: 'teal' })
      refresh()
    } catch (e: any) {
      notify({ message: e.message, color: 'red' })
    } finally {
      setBusy(false)
    }
  }

  // Owner #16 (2026-09-13 editor wave): the Appearance page is now the
  // CENTRAL point for appearance — overall theme, the code-editor theme
  // pair (light/dark, which the editor follows per active overall theme),
  // and the editor font (size / family / line height). The editor
  // settings modal keeps its in-context copies (same store, so both
  // surfaces stay in sync automatically).
  const lightThemes = themeGroups('ol-editorThemes', LIGHT_EDITOR_THEMES)
  const darkThemes = DARK_EDITOR_THEMES
  const legacyThemes = themeGroups('ol-legacyEditorThemes', LEGACY_EDITOR_THEMES)
  const lightGroups = [
    { label: 'Current', items: lightThemes },
    ...(legacyThemes.length ? [{ label: 'Legacy', items: legacyThemes }] : []),
  ]
  const darkGroups = [
    { label: 'Current', items: darkThemes },
    ...(legacyThemes.length ? [{ label: 'Legacy', items: legacyThemes }] : []),
  ]
  const FONT_SIZES = ['10', '11', '12', '13', '14', '16', '18', '20', '22', '24']

  return (
    <Stack gap="md">
      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <Text size="sm" c="dimmed">
            Applies to the whole workspace (this area, the project list, the editor chrome and its modals).
          </Text>
          <RadioGroup
            value={radio}
            onChange={v => void save({ overallTheme: String(v) }, 'Theme saved.')}
            label={<Text size="sm" fw={600}>Appearance</Text>}
          >
            <Stack gap="sm">
              <Radio value="system" label="Match system appearance" description="Follows your OS light/dark setting." />
              <Radio value="light" label="Light" description="Always light." />
              <Radio value="dark" label="Dark" description="Always dark." />
            </Stack>
          </RadioGroup>
          {busy ? <Text size="xs" c="dimmed">Saving…</Text> : null}
        </Stack>
      </Card>

      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <Text size="sm" c="dimmed">
            The code editor follows the active appearance: light UI uses the
            light editor theme, dark UI uses the dark one.
          </Text>
          <Group gap="md" wrap="wrap">
            <div style={{ width: 260 }}>
              <Text size="sm" fw={600} mb={6}>
                Editor theme — light interface
              </Text>
              <NativeSelect
                itemGroups={lightGroups}
                value={settings?.editorLightTheme || 'textmate'}
                onChange={e => void save({ editorLightTheme: e.currentTarget.value }, 'Editor theme saved.')}
                aria-label="Editor theme for the light interface"
              />
            </div>
            <div style={{ width: 260 }}>
              <Text size="sm" fw={600} mb={6}>
                Editor theme — dark interface
              </Text>
              <NativeSelect
                itemGroups={darkGroups}
                value={settings?.editorDarkTheme || 'overleaf_dark'}
                onChange={e => void save({ editorDarkTheme: e.currentTarget.value }, 'Editor theme saved.')}
                aria-label="Editor theme for the dark interface"
              />
            </div>
          </Group>
        </Stack>
      </Card>

      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <Text size="sm" c="dimmed">
            Text formatting inside the code editor.
          </Text>
          <Group gap="md" wrap="wrap">
            <div style={{ width: 180 }}>
              <Text size="sm" fw={600} mb={6}>
                Font size
              </Text>
              <NativeSelect
                value={settings?.fontSize ? String(settings.fontSize) : ''}
                onChange={e => void save({ fontSize: e.currentTarget.value ? Number(e.currentTarget.value) : null }, 'Editor font saved.')}
                data={FONT_SIZES}
                placeholder="Default (12px)"
                aria-label="Editor font size"
              />
            </div>
            <div style={{ width: 240 }}>
              <Text size="sm" fw={600} mb={6}>
                Font family
              </Text>
              <NativeSelect
                value={settings?.fontFamily || 'lucida'}
                onChange={e => void save({ fontFamily: e.currentTarget.value }, 'Editor font saved.')}
                data={[
                  { value: 'monaco', label: 'Monaco / Menlo / Consolas' },
                  { value: 'lucida', label: 'Lucida / Source Code Pro' },
                  { value: 'opendyslexicmono', label: 'OpenDyslexic Mono' },
                ]}
                aria-label="Editor font family"
              />
            </div>
            <div style={{ width: 180 }}>
              <Text size="sm" fw={600} mb={6}>
                Line height
              </Text>
              <NativeSelect
                value={settings?.lineHeight || 'normal'}
                onChange={e => void save({ lineHeight: e.currentTarget.value }, 'Editor font saved.')}
                data={[
                  { value: 'compact', label: 'Compact' },
                  { value: 'normal', label: 'Normal' },
                  { value: 'wide', label: 'Wide' },
                ]}
                aria-label="Editor line height"
              />
            </div>
          </Group>
          <Text size="xs" c="dimmed">
            “Dark mode PDF preview” is a per-project option and stays in the
            project's Settings → Appearance (in the editor).
          </Text>
        </Stack>
      </Card>
    </Stack>
  )
}

function EditorTab() {
  const { settings, refresh } = useUserSettings()
  const [autoPair, setAutoPair] = useState<boolean>(settings?.autoPairDelimiters ?? true)
  const [syntaxValidation, setSyntaxValidation] = useState<boolean>(settings?.syntaxValidation ?? true)
  const [mathPreview, setMathPreview] = useState<boolean>(settings?.mathPreview ?? true)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setAutoPair(settings?.autoPairDelimiters ?? true)
    setSyntaxValidation(settings?.syntaxValidation ?? true)
    setMathPreview(settings?.mathPreview ?? true)
  }, [settings])

  const save = async (patch: Record<string, unknown>) => {
    setBusy(true)
    try {
      await saveUserSettings(patch)
      notify({ message: 'Editor defaults saved.', color: 'teal' })
      refresh()
    } catch (e: any) {
      notify({ message: e.message, color: 'red' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card withBorder paddings="lg" radius="lg">
      <Stack gap="lg">
        <Text size="sm" c="dimmed">
          Editing behavior. (Font size, family and line height live in the
          Appearance page — the central appearance point, owner #16.)
        </Text>
        <Stack gap="sm">
          {[
            {
              label: 'Auto-pair delimiters',
              description: 'Complete brackets, quotes, and LaTeX command pairs as you type.',
              value: autoPair,
              setValue: (v: boolean) => {
                setAutoPair(v)
                void save({ autoPairDelimiters: v })
              },
            },
            {
              label: 'Syntax validation',
              description: 'Wave squiggles under likely LaTeX errors while you type.',
              value: syntaxValidation,
              setValue: (v: boolean) => {
                setSyntaxValidation(v)
                void save({ syntaxValidation: v })
              },
            },
            {
              label: 'Live math preview',
              description: 'Render inline math in the editor as you type.',
              value: mathPreview,
              setValue: (v: boolean) => {
                setMathPreview(v)
                void save({ mathPreview: v })
              },
            },
          ].map(row => (
            <Group key={row.label} justify="space-between" wrap="nowrap">
              <div>
                <Text size="sm" fw={500}>
                  {row.label}
                </Text>
                <Text size="xs" c="dimmed">
                  {row.description}
                </Text>
              </div>
              <Switch
                checked={row.value}
                onChange={checked => row.setValue(checked)}
                loading={busy}
                color="ollitex"
              />
            </Group>
          ))}
        </Stack>
      </Stack>
    </Card>
  )
}

export default function MySettingsSection({
  initialTab,
}: {
  /** Focused view (owner review #12-#15): render only this tab, no tab list,
   * no "full settings page" link — the hub keeps everything in /hub. */
  initialTab?: string
}) {
  const focused = Boolean(initialTab)
  const tab = initialTab || 'account'
  return (
    <Stack gap="md">
      <Tabs defaultValue={tab} style={{ borderWidth: 0 }}>
        {focused ? null : (
          <Tabs.List mb="md">
            <Tabs.Tab value="account" leftSection={<Icon name="person" size={16} />}>
              Account
            </Tabs.Tab>
            <Tabs.Tab value="password" leftSection={<Icon name="key" size={16} />}>
              Password
            </Tabs.Tab>
            <Tabs.Tab value="appearance" leftSection={<Icon name="dark_mode" size={16} />}>
              Appearance
            </Tabs.Tab>
            <Tabs.Tab value="editor" leftSection={<Icon name="code" size={16} />}>
              Editor defaults
            </Tabs.Tab>
          </Tabs.List>
        )}
        <Tabs.Panel value="account">
          <AccountTab />
        </Tabs.Panel>
        <Tabs.Panel value="password">
          <PasswordTab />
        </Tabs.Panel>
        <Tabs.Panel value="appearance">
          <AppearanceTab />
        </Tabs.Panel>
        <Tabs.Panel value="editor">
          <EditorTab />
        </Tabs.Panel>
      </Tabs>
    </Stack>
  )
}
