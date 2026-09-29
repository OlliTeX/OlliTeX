import React, { useCallback, useEffect, useState } from 'react'
import { Button, Group, Paper, Stack, Table, Text } from '@mantine/core'
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import { notify } from '../../shared/notify'
import Icon from '../../shared/icons'

type SessionRow = {
  ip_address?: string
  session_created?: string | number | Date
}

function fmtWhen(v: SessionRow['session_created']) {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString(undefined, {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/**
 * Hub leaf: My settings → Sessions (parity with the classic /user/sessions).
 *
 * 2026-09-08 (owner): the classic page is slated for removal, so the leaf
 * carries the full list itself — current session (from the /user/sessions/list
 * JSON endpoint) plus every other active session — and the "revoke all
 * others" action. No external link to the soon-deleted page.
 */
export default function SessionsLeaf() {
  const [busy, setBusy] = useState(false)
  const [current, setCurrent] = useState<SessionRow | null>(null)
  const [others, setOthers] = useState<SessionRow[] | null>(null)
  const [listError, setListError] = useState<string | null>(null)

  const loadList = useCallback(() => {
    getJSON<{ currentSession: SessionRow; sessions: SessionRow[] }>('/user/sessions/list')
      .then(data => {
        setCurrent(data?.currentSession || null)
        setOthers(Array.isArray(data?.sessions) ? data.sessions : [])
        setListError(null)
      })
      .catch(err => {
        setOthers(null)
        setListError(typeof err === 'string' ? err : (err?.message as string) || 'Could not load the session list.')
      })
  }, [])

  useEffect(() => {
    loadList()
  }, [loadList])

  const clear = useCallback(async () => {
    setBusy(true)
    try {
      await postJSON('/user/sessions/clear', {})
      notify({ message: 'All other sessions were revoked.', color: 'green' })
      loadList()
    } catch (err: any) {
      notify({
        message: (err?.data?.message as string) || 'Could not clear sessions.',
        color: 'red',
      })
    } finally {
      setBusy(false)
    }
  }, [loadList])

  return (
    <div>
      <Stack gap="sm">
        <Group gap={10} wrap="nowrap" align="flex-start">
          <Icon name="public" size={22} style={{ color: 'var(--mantine-color-ollitex-6)' }} />
          <div style={{ flex: 1 }}>
            <Text fw={700} size="md">
              Sessions
            </Text>
            <Text size="sm" c="dimmed" mt={4}>
              Active login sessions for this account. Revoking all other
              sessions signs out every other browser or device you are signed
              in on — the current session stays signed in.
            </Text>
          </div>
        </Group>

        {/* current session (the classic page showed this first) */}
        <Paper withBorder radius="lg" p="sm" w="100%">
          <Group justify="space-between" wrap="nowrap" align="center">
            <Group gap={8} wrap="nowrap">
              <Icon name="check_circle" size={17} style={{ color: 'var(--mantine-color-ollitex-6)' }} />
              <Text size="sm" fw={700}>This session</Text>
            </Group>
            <Text size="sm" c="dimmed" style={{ whiteSpace: 'nowrap' }}>
              {current?.ip_address || '—'} · {fmtWhen(current?.session_created)}
            </Text>
          </Group>
        </Paper>

        {/* other sessions — the list the classic page linked to */}
        {listError ? (
          <Text size="sm" c="dimmed">
            {listError}
          </Text>
        ) : others == null ? (
          <Text size="sm" c="dimmed">Loading sessions…</Text>
        ) : others.length === 0 ? (
          <Text size="sm" c="dimmed">
            No other active sessions — this device is the only one signed in.
          </Text>
        ) : (
          <Table strip withBorder highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Session</Table.Th>
                <Table.Th>IP address</Table.Th>
                <Table.Th>Created</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {others.map((s, i) => (
                <Table.Tr key={i}>
                  <Table.Td>
                    <Text size="sm">
                      {i === 0 ? 'Most recent other session' : `Session ${i + 1}`}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{s.ip_address || '—'}</Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{fmtWhen(s.session_created)}</Text>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}

        <Group gap="sm" wrap="wrap" mt="xs">
          <Button
            color="red"
            variant="light"
            leftSection={<Icon name="link_off" size={16} />}
            disabled={busy || others?.length === 0}
            loading={busy}
            onClick={() => void clear()}
          >
            Revoke all other sessions
          </Button>
        </Group>
      </Stack>
    </div>
  )
}
