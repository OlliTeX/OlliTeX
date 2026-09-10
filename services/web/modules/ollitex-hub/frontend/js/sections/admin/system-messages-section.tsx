import React, { useState } from 'react'
import {
  Button,
  Checkbox,
  Group,
  Stack,
  Text,
  TextInput,
  ActionIcon,
  Tooltip,
} from '@mantine/core'
import { notify, errorMessage } from '../../shared/notify'
import { postJSON, getJSON, patchJSON, deleteJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'
import { invalidate, useSectionData } from '../../shared/use-section-data'
import {
  PLACEMENT_OPTIONS,
  normalizePlacementsChecked,
} from '../../../../../../frontend/js/shared/components/system-message-surface'

type Msg = {
  _id?: string
  content: string
  placements?: string[]
}

type CheckedMap = Record<string, boolean>

const LABEL = 'GET /system/messages'

function placementsToChecked(placements?: string[]): CheckedMap {
  const all = !placements || placements.length === 0
  return {
    all,
    editor: !all && placements!.includes('editor'),
    hub: !all && placements!.includes('hub'),
    auth: !all && placements!.includes('auth'),
  }
}

function applyToggle(prev: CheckedMap, value: string): CheckedMap {
  if (value === 'all') {
    // already 'All pages' → checkbox is a no-op (empty selection means 'all')
    if (prev.all) return prev
    return { all: true, editor: false, hub: false, auth: false }
  }
  if (prev.all) {
    // switch out of 'all' into concrete surfaces
    return {
      all: false,
      editor: value === 'editor',
      hub: value === 'hub',
      auth: value === 'auth',
    }
  }
  const next: CheckedMap = { ...prev, [value]: !prev[value] }
  if (!next.editor && !next.hub && !next.auth) {
    // at least one surface must stay on; "nothing" maps back to 'all'
    next.all = true
  }
  return next
}

/**
 * Checkbox chips: All pages (exclusive) / Editor / Hub / Login & register.
 */
function PlacementChips({
  checked,
  onToggle,
  disabled,
}: {
  checked: CheckedMap
  onToggle: (value: string) => void
  disabled?: boolean
}) {
  return (
    <Group gap={14} wrap="nowrap" align="center">
      {PLACEMENT_OPTIONS.map(opt => (
        <Checkbox
          key={opt.value}
          aria-label={`Placement: ${opt.label}`}
          label={<Text size="sm" fw={600}>{opt.label}</Text>}
          size="xs"
          checked={!!checked[opt.value]}
          disabled={disabled}
          onChange={() => onToggle(opt.value)}
        />
      ))}
    </Group>
  )
}

/**
 * Hub leaf: Site settings → General → System messages (legacy /admin
 * "System messages" pane, Mantine-native).
 *
 * overleaf-lab #6 (2026-09-08): the list refreshes LIVE (30 s heartbeat
 * + tab-activation refetch via useSectionData).
 *
 * #17 (owner 2026-09-13): a – messages render on /hub + auth pages;
 *    b – per-message PLACEMENT selector (where a message is shown);
 *    c – per-message delete.
 *
 * API:
 *   GET    /system/messages          → Msg[]
 *   POST   /admin/messages           → { content, placements? } (strict)
 *   PATCH  /admin/messages/:id       → { placements } (#17b)
 *   DELETE /admin/messages/:id       → (#17c)
 *   POST   /admin/messages/clear     → drops all
 */
export default function SystemMessagesSection() {
  const {
    data: messages,
    lastUpdated,
    refetch,
  } = useSectionData<Msg[]>(
    async () => {
      const d: any = await getJSON('/system/messages')
      return Array.isArray(d) ? (d as Msg[]) : []
    },
    { label: LABEL, live: true },
  )
  const [content, setContent] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)
  const [newChecked, setNewChecked] = useState<CheckedMap>(() =>
    placementsToChecked(undefined)
  )

  const refresh = async () => {
    // mutations must never be answered by the shared TTL copy
    invalidate(LABEL)
    await refetch(true)
  }

  const add = async () => {
    if (!content.trim()) return
    setBusy(true)
    try {
      const { placements } = normalizePlacementsChecked(newChecked)
      await postJSON('/admin/messages', {
        body: { content: content.trim(), placements },
      })
      setContent('')
      setNewChecked(placementsToChecked(undefined))
      notify({ message: 'System message added.', color: 'green' })
      await refresh()
    } catch (err) {
      notify({ message: errorMessage(err, 'Could not add the message.'), color: 'red' })
    } finally {
      setBusy(false)
    }
  }

  const clearAll = async () => {
    setBusy(true)
    setConfirmClear(false)
    try {
      await postJSON('/admin/messages/clear', {})
      notify({ message: 'All system messages cleared.', color: 'green' })
      await refresh()
    } catch (err) {
      notify({ message: errorMessage(err, 'Could not clear the messages.'), color: 'red' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Stack gap="md">
      <Group gap={10} wrap="nowrap">
        <Icon name="campaign" size={22} style={{ color: 'var(--mantine-color-ollitex-6)' }} />
        <div style={{ flex: 1 }}>
          <Text fw={700} size="md">
            System messages
          </Text>
          <Text size="sm" c="dimmed" mt={4}>
            Banner messages shown to everyone on this instance. <b>#17b:</b>{' '}
            choose where each message is shown — all pages, or a specific
            surface (Editor, Hub, Login &amp; register). Legacy messages
            without a placement stay visible everywhere. · live: auto-refreshes
            every 30 s while visible (last updated{' '}
            {lastUpdated
              ? new Date(lastUpdated).toLocaleTimeString()
              : '…'}
            )
          </Text>
        </div>
      </Group>

      <Stack gap={8}>
        <Group gap="sm" wrap="nowrap">
          <TextInput
            aria-label="Message text"
            value={content}
            onChange={e => setContent(e.currentTarget.value)}
            placeholder="e.g. Maintenance window on Sunday 02:00–04:00 UTC"
            style={{ flex: 1, maxWidth: 640 }}
            leftSection={<Icon name="content_copy" size={16} />}
            onKeyDown={e => {
              if (e.key === 'Enter') void add()
            }}
          />
          <Button color="ollitex" leftSection={<Icon name="add" size={16} />} loading={busy} onClick={() => void add()}>
            Add message
          </Button>
          <Button
            color="red"
            variant="light"
            leftSection={<Icon name="delete" size={16} />}
            disabled={!messages?.length || busy}
            onClick={() => setConfirmClear(true)}
          >
            Clear all
          </Button>
        </Group>
        <Group gap={8} align="center">
          <Text size="sm" c="dimmed" fw={600}>
            Where new messages are shown:
          </Text>
          <PlacementChips
            checked={newChecked}
            disabled={busy}
            onToggle={value => setNewChecked(prev => applyToggle(prev, value))}
          />
        </Group>
      </Stack>

      {messages && messages.length > 0 ? (
        <Stack gap={6}>
          {messages.map((m, i) => (
            <MessageRow
              key={m._id || i}
              message={m}
              onDeleted={() => void refresh()}
              onPlacementsSaved={() => void refresh()}
            />
          ))}
        </Stack>
      ) : (
        <Text size="sm" c="dimmed">
          No system messages are currently set.
        </Text>
      )}

      <ConfirmModal
        open={confirmClear}
        title="Clear all system messages?"
        body="Every system message will be removed and users will no longer see the banners."
        confirmLabel="Clear all"
        danger
        loading={busy}
        onConfirm={() => void clearAll()}
        onCancel={() => setConfirmClear(false)}
      />
    </Stack>
  )
}

function MessageRow({
  message: m,
  onDeleted,
  onPlacementsSaved,
}: {
  message: Msg
  onDeleted: () => void
  onPlacementsSaved: () => void
}) {
  const [checked, setChecked] = useState<CheckedMap>(() =>
    placementsToChecked(m.placements)
  )
  const [busy, setBusy] = useState(false)

  const toggle = async (value: string) => {
    const next = applyToggle(checked, value)
    if (next[value] === checked[value]) return
    setChecked(next)
    const { placements } = normalizePlacementsChecked(next)
    if (!m._id) return
    setBusy(true)
    try {
      await patchJSON(`/admin/messages/${encodeURIComponent(m._id)}`, {
        body: { placements },
      })
      notify({ message: 'Message placement updated.', color: 'teal' })
      onPlacementsSaved()
    } catch (err) {
      // roll back local state on failure
      setChecked(checked)
      notify({
        message: errorMessage(err, 'Could not update the placement.'),
        color: 'red',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Stack gap={8} data-testid={m._id ? `msg-row-${m._id}` : undefined} style={{
      border: '1px solid var(--mantine-color-default-border)',
      borderRadius: 8,
      padding: '10px 14px',
      background: 'var(--mantine-color-body)',
    }}>
      <Group gap={10} wrap="nowrap" align="center">
        <Icon name="campaign" size={16} style={{ color: 'var(--mantine-color-dimmed)' }} />
        <Text size="sm" style={{ flex: 1 }}>
          {m.content}
        </Text>
        {/* Owner #17c (2026-09-13): delete a single message, not only all. */}
        <Tooltip label="Delete this message">
          <ActionIcon
            variant="subtle"
            color="red"
            aria-label={`Delete message: ${String(m.content || 'message').slice(0, 40)}`}
            style={{ cursor: 'pointer' }}
            disabled={busy}
            onClick={() => {
              void (async () => {
                try {
                  await deleteJSON(`/admin/messages/${encodeURIComponent(m._id)}`)
                  notify({ message: 'Message deleted.', color: 'teal' })
                  onDeleted()
                } catch (err) {
                  notify({ message: errorMessage(err, 'Could not delete the message.'), color: 'red' })
                }
              })()
            }}
          >
            <Icon name="delete" size={16} />
          </ActionIcon>
        </Tooltip>
      </Group>
      {/* Owner #17b (2026-09-13): WHERE this message is shown. */}
      <Group gap={8} align="center" wrap="nowrap">
        <Text size="xs" c="dimmed" fw={600}>
          Shown on:
        </Text>
        <PlacementChips
          checked={checked}
          disabled={busy}
          onToggle={value => void toggle(value)}
        />
      </Group>
    </Stack>
  )
}
