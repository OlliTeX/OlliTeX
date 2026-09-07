import React, { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Group,
  NumberInput,
  Stack,
  Switch,
  Text,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
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
      notifications.show({ message: 'Notification preferences saved.', color: 'teal' })
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
            onChange={v => {
              setEnabled(v)
              void save(v, delay)
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
            <NumberInput
              value={delay === '' ? null : Number(delay)}
              onChange={v => setDelay(v == null ? '' : String(v))}
              min={1}
              max={10080}
              placeholder="Server default"
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
