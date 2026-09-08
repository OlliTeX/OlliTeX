import React, { useState } from 'react'
import { Alert, Button, Group, Stack, Text, Title } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import getMeta from '@/utils/meta'
import Icon from '../../shared/icons'

/**
 * Editor controls — parity of the legacy /admin/panel "Open/Close Editor"
 * pane (PG-PN-1: the pane had no hub surface at all).
 *
 * Legacy contract (admin-panel.pug + AdminController):
 *   POST /admin/closeEditor            → Settings.editorIsOpen = false (blocks new editor opens)
 *   POST /admin/openEditor             → Settings.editorIsOpen = true
 *   POST /admin/disconnectAllUsers     → force-disconnect every connected editor
 * The server answers with a 302 to /admin#open-close-editor — treat a
 * completed response (non-network-error) as success, matching the form POSTs.
 */
export default function AdminEditorSection() {
  const [busy, setBusy] = useState<'close' | 'open' | 'disconnect' | null>(null)
  const [last, setLast] = useState<string | null>(null)

  const run = async (kind: 'close' | 'open' | 'disconnect') => {
    setBusy(kind)
    try {
      const path =
        kind === 'close' ? '/admin/closeEditor' : kind === 'open' ? '/admin/openEditor' : '/admin/disconnectAllUsers'
      const res = await fetch(path, {
        method: 'POST',
        credentials: 'include',
        headers: {
          'Content-Type': 'application/json',
          'X-Csrf-Token': getMeta('ol-csrfToken') || '',
        },
        body: kind === 'close' ? JSON.stringify({ isOpen: false }) : kind === 'disconnect' ? JSON.stringify({}) : undefined,
        redirect: 'follow',
      })
      // the legacy endpoints answer with a 302 redirect to /admin#open-close-editor;
      // a completed response that lands on the admin page = accepted.
      const landedAdmin = /\/admin/.test(res.url)
      if (!landedAdmin) throw new Error(`POST ${path} did not land on the admin panel (status ${res.status}, url ${res.url})`)
      const msg =
        kind === 'close'
          ? 'Editor closed — new editor opens are now blocked.'
          : kind === 'open'
            ? 'Editor reopened.'
            : 'Disconnect message sent to all connected editors.'
      notifications.show({ message: msg, color: kind === 'open' ? 'teal' : 'yellow' })
      setLast(msg)
    } catch (e: any) {
      notifications.show({ message: e?.message || 'Editor action failed.', color: 'red' })
    } finally {
      setBusy(null)
    }
  }

  return (
    <Stack gap="md" w="100%">
      <div>
        <Title order={2} fw={800}>Editor controls</Title>
        <Text size="sm" c="dimmed" mt={4}>
          Force-close or reopen the instance editor, or disconnect everyone. Mirrors the legacy
          Admin Panel → “Open/Close Editor” pane.
        </Text>
      </div>

      <Stack gap="sm">
        <Group gap="xs">
          <Button
            variant="light"
            color="red"
            leftSection={<Icon name="block" size={16} />}
            loading={busy === 'close'}
            disabled={busy !== null}
            onClick={() => void run('close')}
          >
            Close Editor
          </Button>
          <Text size="xs" c="dimmed">
            Will stop anyone opening the editor. Will NOT disconnect already connected users.
          </Text>
        </Group>

        <Group gap="xs">
          <Button
            variant="default"
            leftSection={<Icon name="play_circle" size={16} />}
            loading={busy === 'open'}
            disabled={busy !== null}
            onClick={() => void run('open')}
          >
            Open Editor
          </Button>
          <Text size="xs" c="dimmed">Re-allow users to open the editor.</Text>
        </Group>

        <Alert icon={<Icon name="warning" size={18} />} color="orange" variant="light">
          <Group gap="xs" wrap="nowrap">
            <Button
              size="xs"
              variant="light"
              color="red"
              leftSection={<Icon name="link_off" size={14} />}
              loading={busy === 'disconnect'}
              disabled={busy !== null}
              onClick={() => void run('disconnect')}
            >
              Disconnect all users
            </Button>
            <Text size="xs" c="dimmed">
              Force disconnect all users with the editor open. Make sure to close the editor first to
              avoid them reconnecting.
            </Text>
          </Group>
        </Alert>

        {last ? (
          <Text size="xs" c="dimmed">
            Last action: {last}
          </Text>
        ) : null}
      </Stack>
    </Stack>
  )
}
