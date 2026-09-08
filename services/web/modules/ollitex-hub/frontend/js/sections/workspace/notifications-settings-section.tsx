import React, { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Group,

  Stack,
  Switch,
  Text,
  TextInput,
} from '@mantine/core'
import { notify } from '../../shared/notify'
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import { PageLoading } from '../../shared/page-state'

export default function NotificationsSettingsSection() {
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [delay, setDelay] = useState<string>('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const prefs = await getJSON('/notifications/preferences')
      setEnabled(!(prefs?.muteAllNotifications ?? false))
      setDelay(prefs?.notificationDelayMinutes != null ? String(prefs.notificationDelayMinutes) : '')
    } catch {
      setEnabled(true)
      setDelay('')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const save = async (nextEnabled: boolean, nextDelay: string) => {
    setBusy(true)
    setErr(null)
    try {
      await postJSON('/notifications/preferences', {
        body: {
          muteAllNotifications: !nextEnabled,
          notificationDelayMinutes: nextDelay === '' ? null : Number(nextDelay),
        },
      })
      notify({ message: 'Notification preferences saved.', color: 'teal' })
    } catch (e: any) {
      setErr((e?.data?.message as string) || 'Could not save preferences.')
    } finally {
      setBusy(false)
    }
  }

  if (enabled === null) return <PageLoading label="Loading notification preferences…" />

  return (
    <Stack gap="md">
      <Card withBorder paddings="lg" radius="lg">
        <Group justify="space-between" wrap="nowrap">
          <div>
            <Text fw={600}>Project activity notifications</Text>
            <Text size="sm" c="dimmed" mt={4}>
              Email me when collaborators update a project I care about.
            </Text>
          </div>
          <Switch
            checked={enabled}
            onChange={() => {
              // PG-NM-1 (parity): toggle from the current UI state. Mantine
              // hands us a value the controlled re-render had already fought
              // (observed: click never turned the switch off and the POST was
              // always muteAllNotifications:false). Deriving the target from
              // `enabled` makes the control deterministic regardless of the
              // onChange payload shape.
              const next = !(enabled === true)
              setEnabled(next)
              void save(next, delay)
            }}
            color="ollitex"
            loading={busy}
          />
        </Group>
      </Card>

      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="sm">
          <Text fw={600}>Delivery delay</Text>
          <Text size="sm" c="dimmed">
            Batch activity emails for at least this many minutes. Leave empty to use the server
            default (2 minutes). Max 10080 (one week).
          </Text>
          <div style={{ maxWidth: 220 }}>
            {/* PG-ND-1 (parity): Mantine NumberInput's typeahead left the React
                state stuck at '' — typed values never reached save(), so the
                delay silently did not persist (legacy form works). A plain
                sanitizing TextInput has no typeahead state and behaves like
                the legacy number input. */}
            <TextInput
              aria-label="Notification delay (minutes)"
              value={delay}
              onChange={e => setDelay(e.currentTarget.value.replace(/[^0-9]/g, '').slice(0, 5))}
              placeholder="Server default (minutes)"
              rightSection="min"
            />
          </div>
          <Group>
            <Button
              color="ollitex"
              loading={busy}
              onClick={() => void save(enabled, delay)}
            >
              Save preferences
            </Button>
          </Group>
          {err ? <Text size="sm" c="red">{err}</Text> : null}
        </Stack>
      </Card>

      <Alert icon={null} variant="light" color="gray" withBorder radius="md">
        <Text size="sm">
          You will always receive important emails (invites, password resets, ownership transfers)
          regardless of these settings.
        </Text>
      </Alert>
    </Stack>
  )
}
