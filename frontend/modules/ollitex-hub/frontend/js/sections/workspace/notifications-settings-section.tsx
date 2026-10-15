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
import Icon from '../../shared/icons'


// Owner #21 (2026-09-13 editor wave): per-type notification table.
// Each row toggles a pair of the backend preference keys (own project /
// invited project, or authored / participating threads). Defaults stay ON
// (the compiled server defaults) — the owner keeps the ability to switch
// individual types off without touching the global mute.
type PrefRow = {
  id: string
  label: string
  description: string
  keys: string[]
}

const NOTIFICATION_TYPE_ROWS: PrefRow[] = [
  {
    id: 'comments',
    label: 'Comments activity',
    description:
      'New comments in own projects and in projects you are invited to.',
    keys: ['commentOnOwnProject', 'commentOnInvitedProject'],
  },
  {
    id: 'replies',
    label: 'Replies & mentions in your threads',
    description:
      'Replies in comment threads you started or take part in, and @mentions.',
    keys: [
      'repliesOnAuthoredThread',
      'repliesOnParticipatingThread',
      'mentionsInThread',
    ],
  },
  {
    id: 'resolved',
    label: 'Comment threads resolved or reopened',
    description:
      'Threads you started or participate in are resolved or reopened.',
    keys: [
      'commentResolvedOnAuthoredThread',
      'commentResolvedOnParticipatingThread',
      'commentReopenedOnAuthoredThread',
      'commentReopenedOnParticipatingThread',
    ],
  },
  {
    id: 'tracked',
    label: 'Tracked changes',
    description:
      'New tracked changes in own projects and in projects you are invited to.',
    keys: ['trackedChangesOnOwnProject', 'trackedChangesOnInvitedProject'],
  },
  {
    id: 'trackdecisions',
    label: 'Your tracked changes accepted or rejected',
    description: 'A collaborator accepts or rejects changes you made.',
    keys: [
      'trackChangesAcceptedOnAuthoredChange',
      'trackChangesRejectedOnAuthoredChange',
    ],
  },
]


const KNOWN_PREF_KEYS = [
  'commentOnOwnProject',
  'commentOnInvitedProject',
  'repliesOnAuthoredThread',
  'repliesOnParticipatingThread',
  'mentionsInThread',
  'commentResolvedOnAuthoredThread',
  'commentResolvedOnParticipatingThread',
  'commentReopenedOnAuthoredThread',
  'commentReopenedOnParticipatingThread',
  'trackedChangesOnOwnProject',
  'trackedChangesOnInvitedProject',
  'trackChangesAcceptedOnAuthoredChange',
  'trackChangesRejectedOnAuthoredChange',
]

function allRowOn(pref: Record<string, unknown> | null, row: PrefRow): boolean {
  return row.keys.every(k => pref?.[k] === true)
}

export default function NotificationsSettingsSection() {
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [delay, setDelay] = useState<string>('')
  const [pref, setPref] = useState<Record<string, unknown> | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  // NTF-UI-1 fix (2026-10-15, owner-approved): the previous load() caught ANY
  // failure and rendered a SILENT checked-default state (setEnabled(true) + all
  // prefs on) — under load an intermittent first-GET failure made the mute
  // switch show the OPPOSITE of the saved state, and a user acting on it saved
  // the opposite direction. Now: bounded retries with backoff, and on final
  // failure a VISIBLE error state with Retry — the control is not rendered in
  // a misleading state (enabled stays null, so no save can go out the wrong way).
  const [loadFailed, setLoadFailed] = useState(false)

  const load = useCallback(async () => {
    setLoadFailed(false)
    let lastErr: unknown = null
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        const prefs = await getJSON<any>('/notifications/preferences')
        setEnabled(!(prefs?.muteAllNotifications ?? false))
        setDelay(prefs?.notificationDelayMinutes != null ? String(prefs.notificationDelayMinutes) : '')
        const next: Record<string, unknown> = {}
        for (const k of KNOWN_PREF_KEYS) {
          next[k] = prefs?.[k] === true
        }
        setPref(next)
        return
      } catch (e) {
        lastErr = e
        await new Promise((r) => setTimeout(r, 300 * (attempt + 1)))
      }
    }
    void lastErr
    setLoadFailed(true) // visible error state below — NO silent default render
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const buildBody = (nextEnabled: boolean, nextDelay: string, nextPref: Record<string, unknown>) => ({
    muteAllNotifications: !nextEnabled,
    notificationDelayMinutes: nextDelay === '' ? null : Number(nextDelay),
    ...nextPref,
  })

  const save = async (nextEnabled: boolean, nextDelay: string, nextPref: Record<string, unknown> | null = null) => {
    setBusy(true)
    setErr(null)
    try {
      await postJSON('/notifications/preferences', {
        body: buildBody(nextEnabled, nextDelay, nextPref ?? (pref ?? {})),
      })
      notify({ message: 'Notification preferences saved.', color: 'teal' })
      await load()
    } catch (e: any) {
      setErr((e?.data?.message as string) || 'Could not save preferences.')
    } finally {
      setBusy(false)
    }
  }

  if (loadFailed) {
    // NTF-UI-1: visible failure state — the control is NOT rendered with a
    // guessed/misleading value; the user can retry without risking a wrong save.
    return (
      <Alert
        icon={<Icon name="warn" size={18} />}
        variant="light"
        color="red"
        withBorder
        radius="md"
        title="Couldn't load notification preferences"
      >
        <Text size="sm">The preferences request failed. Your saved preferences were not changed.</Text>
        <Group mt={8}>
          <Button size="xs" onClick={() => void load()} loading={false}>
            Retry
          </Button>
        </Group>
      </Alert>
    )
  }

  if (enabled === null || pref === null) return <PageLoading label="Loading notification preferences…" />

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

      {/* Owner #21: the per-type table moved here from the editor's
          "Project notification" setting (which now only carries On/Off +
          this page's link). */}
      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="sm">
          <Text fw={600}>Notification types</Text>
          <Text size="sm" c="dimmed">
            Choose the kinds of project activity you want emails for. Defaults
            are all ON (the server defaults); switch individual types off here.
            Projects can still override these per project in the editor.
          </Text>
          <Stack gap="xs" mt="xs">
            {NOTIFICATION_TYPE_ROWS.map(row => {
              const on = allRowOn(pref, row)
              const patch = (value: boolean) => {
                const next = { ...pref }
                for (const k of row.keys) next[k] = value
                setPref(next)
                void save(enabled, delay, next)
              }
              return (
                <Group key={row.id} justify="space-between" wrap="nowrap"
                  style={{ padding: '10px 0', borderBottom: '1px solid rgba(128,128,128,0.2)' }}>
                  <div>
                    <Text size="sm" fw={500}>{row.label}</Text>
                    <Text size="xs" c="dimmed">{row.description}</Text>
                  </div>
                  <Switch
                    checked={on}
                    onChange={() => patch(!on)}
                    color="ollitex"
                    loading={busy}
                    aria-label={row.label}
                  />
                </Group>
              )
            })}
          </Stack>
        </Stack>
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
