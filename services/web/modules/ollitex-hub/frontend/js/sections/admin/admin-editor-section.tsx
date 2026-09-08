import React, { useCallback, useEffect, useState } from 'react'
import { Alert, Badge, Button, Group, Stack, Text, TextInput, Title } from '@mantine/core'
import { notify, errorMessage } from '../../shared/notify'
import getMeta from '@/utils/meta'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'

/**
 * Editor controls — parity of the legacy /admin/panel "Open/Close Editor"
 * pane (PG-PN-1: the pane had no hub surface at all).
 *
 * overleaf-lab #4 (2026-09-08):
 *   * live editor-gate chip (GET /admin/editor-state — new read-only endpoint)
 *     refreshed on mount and after every action, so the panel always shows
 *     whether opening is currently allowed;
 *   * every destructive one-click action now goes through a confirmation
 *     modal; "Disconnect all users" requires TYPING DISCONNECT
 *     (legacy admin-panel had zero confirmation for any of the three).
 *
 * Legacy contract (admin-panel.pug + AdminController):
 *   POST /admin/closeEditor            → Settings.editorIsOpen = false (blocks new editor opens)
 *   POST /admin/openEditor             → Settings.editorIsOpen = true
 *   POST /admin/disconnectAllUsers     → force-disconnect every connected editor
 * The mutating endpoints answer with a 302 to /admin#open-close-editor —
 * treat a completed response (non-network-error) as success, matching the
 * legacy form POSTs.
 */
type Action = 'close' | 'open' | 'disconnect'

export default function AdminEditorSection() {
  const [busy, setBusy] = useState<Action | null>(null)
  const [last, setLast] = useState<string | null>(null)
  const [state, setState] = useState<{ editorIsOpen: boolean; siteIsOpen: boolean } | null>(null)
  const [stateErr, setStateErr] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<Action | null>(null)
  const [typed, setTyped] = useState('')

  const loadState = useCallback(async () => {
    try {
      const res = await fetch('/admin/editor-state', {
        headers: { Accept: 'application/json' },
        credentials: 'include',
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setState((await res.json()) as { editorIsOpen: boolean; siteIsOpen: boolean })
      setStateErr(null)
    } catch (e: any) {
      setStateErr(errorMessage(e, 'State unavailable'))
    }
  }, [])

  useEffect(() => {
    void loadState()
  }, [loadState])

  const run = async (kind: Action) => {
    setBusy(kind)
    setConfirming(null)
    setTyped('')
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
      notify({ message: msg, color: kind === 'open' ? 'teal' : 'yellow' })
      setLast(msg)
      // refresh the live chip from the authoritative server state
      await loadState()
    } catch (e: any) {
      notify({ message: e?.message || 'Editor action failed.', color: 'red' })
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

      {/* overleaf-lab #4: live editor-gate state (was a static text line) */}
      <Group gap="xs" align="center">
        {state ? (
          <Badge
            variant="light"
            color={state.editorIsOpen ? 'teal' : 'red'}
            radius="sm"
            size="sm"
            icon={<Icon name={state.editorIsOpen ? 'check_circle' : 'block'} size={14} />}
          >
            Editor gate: {state.editorIsOpen ? 'OPEN — users can open the editor' : 'CLOSED — new opens are blocked'}
          </Badge>
        ) : (
          <Badge variant="light" color="gray" radius="sm" size="sm">
            {stateErr ? `state: ${stateErr}` : 'state: loading…'}
          </Badge>
        )}
        <Button variant="subtle" size="xs" onClick={() => void loadState()}>
          refresh state
        </Button>
      </Group>

      <Stack gap="sm">
        <Group gap="xs">
          <Button
            variant="light"
            color="red"
            leftSection={<Icon name="block" size={16} />}
            loading={busy === 'close'}
            disabled={busy !== null}
            onClick={() => setConfirming('close')}
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
            onClick={() => setConfirming('open')}
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
              onClick={() => {
                setTyped('')
                setConfirming('disconnect')
              }}
            >
              Disconnect all users
            </Button>
            <Text size="xs" c="dimmed">
              Force disconnect all users with the editor open. Make sure to close the editor first to
              avoid them reconnecting. Requires typed confirmation.
            </Text>
          </Group>
        </Alert>

        {last ? (
          <Text size="xs" c="dimmed">
            Last action: {last}
          </Text>
        ) : null}
      </Stack>

      <ConfirmModal
        open={confirming === 'close'}
        title="Close the editor for the whole instance?"
        body="Users will no longer be able to open the source editor. Currently connected sessions keep running until they disconnect."
        confirmLabel="Close Editor"
        danger
        loading={busy === 'close'}
        onCancel={() => setConfirming(null)}
        onConfirm={() => void run('close')}
      />
      <ConfirmModal
        open={confirming === 'open'}
        title="Reopen the editor?"
        body="Users are allowed to open the source editor again."
        confirmLabel="Open Editor"
        loading={busy === 'open'}
        onCancel={() => setConfirming(null)}
        onConfirm={() => void run('open')}
      />
      <ConfirmModal
        open={confirming === 'disconnect'}
        title="Disconnect ALL users"
        body={
          <Stack gap="xs">
            <Text size="sm" c="dimmed">
              Every connected editor session on this instance is force-disconnected. Users with
              unsaved in-editor state lose it after the disconnect. Close the editor first to
              prevent reconnects.
            </Text>
            <Text size="sm" fw={600}>
              Type <code>DISCONNECT</code> to enable the button.
            </Text>
            <TextInput
              value={typed}
              onChange={e => setTyped(e.currentTarget.value)}
              placeholder="DISCONNECT"
              size="sm"
              aria-label="Type DISCONNECT to confirm"
            />
          </Stack>
        }
        confirmLabel="Disconnect everyone"
        danger
        loading={busy === 'disconnect'}
        onCancel={() => {
          setConfirming(null)
          setTyped('')
        }}
        onConfirm={() => void run('disconnect')}
      />
    </Stack>
  )
}
