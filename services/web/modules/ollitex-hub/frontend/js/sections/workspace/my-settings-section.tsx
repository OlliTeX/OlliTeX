import React, { useEffect, useState } from 'react'
import {
  Alert,
  Anchor,
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
import { notifications } from '@mantine/notifications'
import { postJSON } from '@/infrastructure/fetch-json'
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
      notifications.show({ message: 'Profile saved.', color: 'teal' })
      refresh()
    } catch (e: any) {
      notifications.show({ message: e.message, color: 'red' })
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
            Changing the primary email lives in the full settings page (it involves confirmation emails).
            <Anchor href="/user/mysettings" target="_blank" size="xs" ml={6}>
              Open
            </Anchor>
          </Text>
        </div>
        <Group gap="md" wrap="wrap">
          <div style={{ minWidth: 200, flex: 1 }}>
            <Text size="sm" fw={600} mb={6}>
              First name
            </Text>
            <TextInput value={firstName} onChange={e => setFirstName(e.currentTarget.value)} />
          </div>
          <div style={{ minWidth: 200, flex: 1 }}>
            <Text size="sm" fw={600} mb={6}>
              Last name
            </Text>
            <TextInput value={lastName} onChange={e => setLastName(e.currentTarget.value)} />
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
      notifications.show({ message: 'Password changed.', color: 'teal' })
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
          />
        </div>
        <Group gap="md" wrap="wrap">
          <div style={{ minWidth: 200, flex: 1 }}>
            <Text size="sm" fw={600} mb={6}>
              New password
            </Text>
            <TextInput type="password" value={next1} onChange={e => setNext1(e.currentTarget.value)} placeholder="New password" />
          </div>
          <div style={{ minWidth: 200, flex: 1 }}>
            <Text size="sm" fw={600} mb={6}>
              Repeat new password
            </Text>
            <TextInput type="password" value={next2} onChange={e => setNext2(e.currentTarget.value)} placeholder="Repeat new password" />
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
  const [theme, setTheme] = useState<string>(settings?.overallTheme || 'system')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setTheme(settings?.overallTheme || 'system')
  }, [settings?.overallTheme])

  const save = async (value: string) => {
    setTheme(value)
    setBusy(true)
    try {
      await saveUserSettings({ overallTheme: value })
      notifications.show({ message: 'Theme saved.', color: 'teal' })
      refresh()
    } catch (e: any) {
      notifications.show({ message: e.message, color: 'red' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card withBorder paddings="lg" radius="lg">
      <Stack gap="md">
        <Text size="sm" c="dimmed">
          Applies to the whole workspace (this area, the project list, and the editor chrome).
        </Text>
        <RadioGroup
          value={theme}
          onChange={v => void save(String(v))}
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
  )
}

function EditorTab() {
  const { settings, refresh } = useUserSettings()
  const [fontSize, setFontSize] = useState<number | null>(settings?.fontSize ?? null)
  const [autoPair, setAutoPair] = useState<boolean>(settings?.autoPairDelimiters ?? true)
  const [syntaxValidation, setSyntaxValidation] = useState<boolean>(settings?.syntaxValidation ?? true)
  const [, setPdfViewer] = useState<boolean>(settings?.pdfViewer ?? 'latexWork')
  const [mathPreview, setMathPreview] = useState<boolean>(settings?.mathPreview ?? true)
  const [family, setFamily] = useState<string>(settings?.fontFamily || 'lucida')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setFontSize(settings?.fontSize ?? null)
    setAutoPair(settings?.autoPairDelimiters ?? true)
    setSyntaxValidation(settings?.syntaxValidation ?? true)
    setPdfViewer(settings?.pdfViewer !== false)
    setMathPreview(settings?.mathPreview ?? true)
    setFamily(settings?.fontFamily || 'lucida')
  }, [settings])

  const save = async (patch: Record<string, unknown>) => {
    setBusy(true)
    try {
      await saveUserSettings(patch)
      notifications.show({ message: 'Editor defaults saved.', color: 'teal' })
      refresh()
    } catch (e: any) {
      notifications.show({ message: e.message, color: 'red' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card withBorder paddings="lg" radius="lg">
      <Stack gap="lg">
        <Group gap="md" wrap="wrap">
          <div style={{ width: 180 }}>
            <Text size="sm" fw={600} mb={6}>
              Default font size
            </Text>
            <NativeSelect
              value={fontSize ? String(fontSize) : ''}
              onChange={e => {
                const v = e.currentTarget.value
                setFontSize(v ? Number(v) : null)
                void save({ fontSize: v ? Number(v) : null })
              }}
              data={['9', '10', '11', '12', '13', '14', '16', '18']}
              placeholder="Instance default"
            />
          </div>
          <div style={{ width: 240 }}>
            <Text size="sm" fw={600} mb={6}>
              Editor font
            </Text>
            <NativeSelect
              value={family}
              onChange={e => {
                setFamily(e.currentTarget.value)
                void save({ fontFamily: e.currentTarget.value })
              }}
              data={[
                { value: 'lucida', label: 'Lucida (default)' },
                { value: 'dejavu', label: 'DejaVu Sans Mono' },
              ]}
            />
          </div>
        </Group>
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
