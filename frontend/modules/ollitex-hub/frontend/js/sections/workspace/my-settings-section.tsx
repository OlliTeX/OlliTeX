import React, { useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Group,
  NativeSelect,
  Stack,
  Switch,
  Tabs,
  Text,
  TextInput,
} from '@mantine/core'
import { notify } from '../../shared/notify'
import { getJSON, postJSON, deleteJSON } from '@/infrastructure/fetch-json'
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
  const [busy, setBusy] = useState(false)

  const save = async (patch: Record<string, unknown>, note: string) => {
    setBusy(true)
    try {
      await saveUserSettings(patch)
      notify({ message: note, color: 'teal' })
      refresh()
    } catch (e: any) {
      notify({ message: e.message, color: 'red' })
    } finally {
      setBusy(false)
    }
  }

  // 2026-10-09 (owner): the Appearance (overall theme) + editor-theme cards
  // were removed from this page — the header theme toggle stays as the theme
  // surface; the editor font card remains.
  const FONT_SIZES = ['10', '11', '12', '13', '14', '16', '18', '20', '22', '24']

  return (
    <Stack gap="md">
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

function WakaTimeTab() {
  // G (owner 2026-10-09): per-user opt-in for the self-hosted WakaTime
  // (wakapi) relay. ON → PUT /user/wakatime {auto:true} (the server
  // provisions the wakapi account from the user's email and stores the
  // key); OFF → DELETE /user/wakatime. Status via GET /user/wakatime/status.
  //
  // owner 2026-10-10 (Q2): the user's OWN WakaTime VIEW lives here too —
  // cross-project time (GET /user/wakatime/summary) + dashboard access
  // (url/login/api-key come back on the status endpoint, user-scoped only).
  type WakaStatus = {
    connected?: boolean
    apiUrl?: string
    dashboardUrl?: string
    apiKey?: string
    username?: string
    password?: string
  }
  const [checking, setChecking] = useState(true)
  const [st, setSt] = useState<WakaStatus>({ connected: false })
  const [totalSeconds, setTotalSeconds] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  const [copied, setCopied] = useState(false)
  const on = Boolean(st.connected)

  const refresh = () => {
    getJSON<WakaStatus>('/user/wakatime/status')
      .then(r => setSt({ connected: false, ...(r || {}) }))
      .catch(() => setSt({ connected: false }))
      .finally(() => setChecking(false))
  }

  useEffect(() => {
    void refresh()
  }, [])

  // the cross-project time figure (best-effort — the settings page never
  // breaks on a WakaTime API wobble)
  useEffect(() => {
    if (!on) {
      setTotalSeconds(null)
      return
    }
    let live = true
    getJSON<{ connected?: boolean; totalSeconds?: number }>('/user/wakatime/summary')
      .then(r => { if (live && r && r.connected) setTotalSeconds(Number(r.totalSeconds || 0)) })
      .catch(() => undefined)
    return () => { live = false }
  }, [on])

  const flip = async (next: boolean) => {
    setBusy(true)
    setNote('')
    try {
      if (next) {
        const r: any = await postJSON('/user/wakatime', { body: { auto: true } })
        if (r && (r.ok === false || r.message)) {
          setNote(String(r.message || 'Could not enable WakaTime.'))
          setSt({ connected: false })
          return
        }
        await refresh()
        setNote('Enabled — your editor activity now relays to your wakapi account (username = your email local part).')
      } else {
        await deleteJSON('/user/wakatime')
        setSt({ connected: false })
        setTotalSeconds(null)
        setNote('Disabled — heartbeats are no longer sent.')
      }
    } catch (e: any) {
      setNote(e?.data?.message || e?.message || 'Could not change WakaTime state.')
    } finally {
      setBusy(false)
    }
  }

  const copyKey = async () => {
    try {
      await navigator.clipboard.writeText(String(st.apiKey || ''))
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard unavailable — the key stays visible in the line above */
    }
  }

  const fmtTime = (secs: number): string => {
    const h = Math.floor(secs / 3600)
    const m = Math.floor((secs % 3600) / 60)
    if (h > 0) return `${h} h ${m} min`
    if (m > 0) return `${m} min`
    return `${Math.floor(secs)} s`
  }

  return (
    <Card withBorder paddings="lg" radius="lg">
      <Stack gap="md">
        <Text size="sm" c="dimmed">
          Editor activity tracking (WakaTime / wakapi, self-hosted). When on,
          OlliTeX relays your editing statistics to your wakapi account
          (username = your institutional email). The browser never talks to
          wakapi directly; the API key stays on the server.
        </Text>
        <Group justify="space-between" align="center">
          <Text size="sm" fw={600}>
            {checking ? 'Checking account…' : on ? 'WakaTime tracking: on' : 'WakaTime tracking: off'}
          </Text>
          <Switch
            checked={on}
            onChange={() => undefined}
            onClick={() => void flip(!on)}
            loading={busy}
            color="ollitex"
          />
        </Group>
        {note ? <Text size="sm" c={on ? 'teal' : 'dimmed'}>{note}</Text> : null}
        {on ? (
          <Stack gap="xs" pt="xs">
            <Text size="sm" fw={600}>Your WakaTime data</Text>
            {totalSeconds !== null ? (
              <Text size="sm">
                <Text size="sm" inline fw={600}>{fmtTime(totalSeconds)}</Text>
                {' '}tracked in the last 14 days (all projects)
              </Text>
            ) : null}
            <Group gap="xs" wrap="wrap" align="center">
              {st.dashboardUrl ? (
                <Button
                  component="a"
                  href={st.dashboardUrl}
                  target="_blank"
                  rel="noreferrer"
                  size="xs"
                  variant="light"
                  color="ollitex"
                >
                  Open my WakaTime dashboard ↗
                </Button>
              ) : null}
              {st.username ? (
                <Text size="xs" c="dimmed">
                  dashboard login: <Text size="xs" inline component="code">{st.username}</Text>
                  {st.password ? <> / <Text size="xs" inline component="code">{st.password}</Text></> : null}
                </Text>
              ) : null}
            </Group>
            {st.apiKey ? (
              <Group gap="xs" wrap="wrap" align="center">
                <Text size="xs" c="dimmed">API key (URL + key for local WakaTime plugins):</Text>
                <Text size="xs" component="code" lineClamp={1} style={{ maxWidth: 340 }}>{st.apiKey}</Text>
                <Button size="xs" variant="subtle" loading={copied} onClick={() => void copyKey()}>
                  {copied ? 'Copied' : 'Copy key'}
                </Button>
              </Group>
            ) : null}
          </Stack>
        ) : null}
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
            <Tabs.Tab value="waka" leftSection={<Icon name="query_stats" size={16} />}>
              WakaTime
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
        <Tabs.Panel value="waka">
          <WakaTimeTab />
        </Tabs.Panel>
      </Tabs>
    </Stack>
  )
}
