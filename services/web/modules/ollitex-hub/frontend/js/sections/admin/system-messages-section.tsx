import React, { useCallback, useEffect, useState } from 'react'
import { Button, Group, Stack, Text, TextInput } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'

type Msg = { _id?: string; content: string }

/**
 * Hub leaf: Site settings → General → System messages (legacy /admin
 * "System messages" pane, Mantine-native).
 *
 * API (same endpoints the classic admin uses):
 *   GET  /system/messages          → Msg[]
 *   POST /admin/messages           → { content } (strict body)
 *   POST /admin/messages/clear     → drops all
 */
export default function SystemMessagesSection() {
  const [messages, setMessages] = useState<Msg[] | null>(null)
  const [content, setContent] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)

  const load = useCallback(async () => {
    try {
      const data = await getJSON('/system/messages')
      setMessages(Array.isArray(data) ? data : [])
    } catch {
      setMessages([])
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const add = async () => {
    if (!content.trim()) return
    setBusy(true)
    try {
      await postJSON('/admin/messages', { body: { content: content.trim() } })
      setContent('')
      notifications.show({ message: 'System message added.', color: 'green' })
      void load()
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || 'Could not add the message.',
        color: 'red',
      })
    } finally {
      setBusy(false)
    }
  }

  const clearAll = async () => {
    setBusy(true)
    setConfirmClear(false)
    try {
      await postJSON('/admin/messages/clear', {})
      notifications.show({ message: 'All system messages cleared.', color: 'green' })
      void load()
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || 'Could not clear the messages.',
        color: 'red',
      })
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
            project list, editor). One line each.
          </Text>
        </div>
      </Group>

      <Group gap="sm" wrap="nowrap">
        <TextInput
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
