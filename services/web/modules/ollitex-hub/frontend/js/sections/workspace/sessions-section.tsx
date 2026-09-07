import React, { useCallback, useState } from 'react'
import { Button, Group, Stack, Text } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { postJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'

/**
 * Hub leaf: My settings → Sessions (/user/sessions parity).
 *
 * The backend exposes the sessions list as the server-rendered /user/sessions
 * page and a POST /user/sessions/clear action (drops every session except the
 * current one). This card exposes that action directly in /hub and links to
 * the full read-only list — no parameter is lost, nothing is dead-linked.
 */
export default function SessionsLeaf() {
  const [busy, setBusy] = useState(false)

  const clear = useCallback(async () => {
    setBusy(true)
    try {
      await postJSON('/user/sessions/clear', {})
      notifications.show({
        message: 'All other sessions were revoked.',
        color: 'green',
      })
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || 'Could not clear sessions.',
        color: 'red',
      })
    } finally {
      setBusy(false)
    }
  }, [])

  return (
    <CardWrap>
      <Stack gap="sm">
        <Group gap={10} wrap="nowrap">
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
        <Group gap="sm" wrap="wrap">
          <Button
            color="red"
            variant="light"
            leftSection={<Icon name="link_off" size={16} />}
            disabled={busy}
            loading={busy}
            onClick={() => void clear()}
          >
            Revoke all other sessions
          </Button>
          <a
            href="/user/sessions"
            target="_blank"
            rel="noreferrer"
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              color: 'var(--mantine-color-ollitex-7)',
              fontWeight: 600,
              fontSize: 13.5,
              textDecoration: 'none',
            }}
          >
            <Icon name="visibility" size={16} />
            View sessions list
          </a>
        </Group>
      </Stack>
    </CardWrap>
  )
}

/** Local wrapper so the leaf keeps the hub Card visual language. */
function CardWrap({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        border: '1px solid var(--mantine-color-default-border)',
        borderRadius: 12,
        padding: '18px 20px',
        background: 'var(--mantine-color-body)',
      }}
    >
      {children}
    </div>
  )
}
