import React, { useState } from 'react'
import { Button, Group, Stack, Text, TextInput, ActionIcon, Tooltip } from '@mantine/core'
import { notify, errorMessage } from '../../shared/notify'
import { postJSON, getJSON, deleteJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'
import { invalidate, useSectionData } from '../../shared/use-section-data'

type Msg = { _id?: string; content: string }

const LABEL = 'GET /system/messages'

/**
 * Hub leaf: Site settings → General → System messages (legacy /admin
 * "System messages" pane, Mantine-native).
 *
 * overleaf-lab #6 (2026-09-08): the list now refreshes LIVE (30 s heartbeat
 * + tab-activation refetch via useSectionData) — an admin changing the
 * banner in another window/tab, or on the classic page, sees it appear here
 * without a manual reload.
 *
 * API (same endpoints the classic admin uses):
 *   GET  /system/messages          → Msg[]
 *   POST /admin/messages           → { content } (strict body)
 *   POST /admin/messages/clear     → drops all
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

  const refresh = async () => {
    // mutations must never be answered by the shared TTL copy
    invalidate(LABEL)
    await refetch(true)
  }

  const add = async () => {
    if (!content.trim()) return
    setBusy(true)
    try {
      await postJSON('/admin/messages', { body: { content: content.trim() } })
      setContent('')
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
            Banner messages shown to every user on this instance (login page,
            project list, editor). One line each. · overleaf-lab #6: live —
            {lastUpdated
              ? ` last updated ${new Date(lastUpdated).toLocaleTimeString()}`
              : ' auto-refreshes every 30 s while visible'}
          </Text>
        </div>
      </Group>

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

      {messages && messages.length > 0 ? (
        <Stack gap={6}>
          {messages.map((m, i) => (
            <div
              key={m._id || i}
              style={{
                border: '1px solid var(--mantine-color-default-border)',
                borderRadius: 8,
                padding: '10px 14px',
                background: 'var(--mantine-color-body)',
                display: 'flex',
                alignItems: 'center',
                gap: 10,
              }}
            >
              <Icon name="campaign" size={16} style={{ color: 'var(--mantine-color-dimmed)' }} />
              <Text size="sm" style={{ flex: 1 }}>
                {m.content}
              </Text>
              {/* Owner #17c (2026-09-13): delete a single message, not only all. */}
              <Tooltip label="Delete this message">
                <ActionIcon
                  variant="subtle"
                  color="red"
                  aria-label={
                    `Delete message: ${String(m.content || 'message').slice(0, 40)}`
                  }
                  style={{ cursor: 'pointer' }}
                  onClick={() => {
                    void (async () => {
                      try {
                        await deleteJSON(`/admin/messages/${encodeURIComponent(m._id)}`)
                        notify({ message: 'Message deleted.', color: 'teal' })
                        await refresh()
                      } catch (err) {
                        notify({ message: errorMessage(err, 'Could not delete the message.'), color: 'red' })
                      }
                    })()
                  }}
                >
                  <Icon name="delete" size={16} />
                </ActionIcon>
              </Tooltip>
            </div>
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
