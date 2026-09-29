/**
 * 2026-09-11 (owner batch 2 item 6): Git integration (git-bridge personal
 * access tokens) — restored to the hub mysettings. The legacy
 * /user/mysettings rendered this via the git-bridge widget; the hub leaf
 * `mysettings.gitsync` renders the same data natively:
 *
 *   GET    /git-bridge/personal-access-tokens      → token list
 *   POST   /git-bridge/personal-access-tokens      → create (raw token once)
 *   DELETE /git-bridge/personal-access-tokens/:id  → revoke
 *
 * Limits are enforced server-side (MAX_PAT_COUNT = 10).
 */
import React, { useCallback, useEffect, useState } from 'react'
import {
  ActionIcon,
  Anchor,
  Button,
  Card,
  Code,
  CopyButton,
  Group,
  Modal,
  Stack,
  Table,
  Text,
  Tooltip,
} from '@mantine/core'
import { notify } from '../../shared/notify'
import { getJSON, postJSON, deleteJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'

type PatToken = {
  _id: string
  accessTokenPartial?: string
  createdAt?: string
  lastUsedAt?: string | null
  expiresAt?: string
}

function fmtDate(d?: string | null): string {
  if (!d) return '—'
  try {
    return new Date(d).toLocaleDateString('en-GB', {
      day: 'numeric',
      month: 'short',
      year: 'numeric',
    })
  } catch {
    return '—'
  }
}

export default function GitIntegrationSection() {
  const [tokens, setTokens] = useState<PatToken[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [newTokenRaw, setNewTokenRaw] = useState<string | null>(null)
  const [revoking, setRevoking] = useState<PatToken | null>(null)
  const [deleting, setDeleting] = useState(false)

  const refresh = useCallback(() => {
    getJSON<PatToken[]>('/git-bridge/personal-access-tokens')
      .then(d => {
        setLoadError(null)
        setTokens(Array.isArray(d) ? d : (d && (d as any).tokens) || [])
      })
      .catch(err => {
        setLoadError((err?.data?.message as string) || String(err?.message || err))
        setTokens(null)
      })
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  const addToken = async () => {
    setAdding(true)
    try {
      const res = await postJSON<any>('/git-bridge/personal-access-tokens', { body: {} })
      const raw = res?.accessToken || res?.token
      setNewTokenRaw(raw || null)
      notify({ message: 'Git token created.', color: 'teal' })
      refresh()
    } catch (err: any) {
      notify({
        message: (err?.data?.message as string) || 'Could not create a token.',
        color: 'red',
      })
    } finally {
      setAdding(false)
    }
  }

  const doRevoke = async () => {
    if (!revoking) return
    setDeleting(true)
    try {
      await deleteJSON(`/git-bridge/personal-access-tokens/${revoking._id}`)
      notify({ message: 'Token removed.', color: 'gray' })
      setRevoking(null)
      refresh()
    } catch (err: any) {
      notify({
        message: (err?.data?.message as string) || 'Could not remove the token.',
        color: 'red',
      })
      setRevoking(null)
    } finally {
      setDeleting(false)
    }
  }

  const atLimit = (tokens?.length || 0) >= 10

  return (
    <Stack key="git-integration" gap="md">
      <Text size="sm" c="dimmed">
        With Git integration, you can clone your projects with Git. For full instructions on how to
        do this,{' '}
        <Anchor href="/learn/how-to/Git_Integration" target="_blank" rel="noreferrer">
          read the Git Integration help page
        </Anchor>
        .
      </Text>

      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <div>
            <Text fw={700}>Your Git authentication tokens</Text>
            <Text size="sm" c="dimmed" mt={4}>
              Your Git authentication tokens should be entered whenever you’re prompted for a
              password.
            </Text>
          </div>

          <Group gap="xs" wrap="wrap">
            <Text size="sm" c="dimmed">
              • You can have up to 10 tokens.
            </Text>
            <Text size="sm" c="dimmed">
              • If you reach the maximum limit, you’ll need to delete a token before you can
              generate a new one.
            </Text>
          </Group>

          {loadError ? (
            <Text size="sm" c="red">
              {loadError}
            </Text>
          ) : tokens === null ? (
            <Text size="sm" c="dimmed">
              Loading tokens…
            </Text>
          ) : tokens.length === 0 ? (
            <Text size="sm" c="dimmed">
              No tokens yet — generate one below and it will appear here.
            </Text>
          ) : (
            <Table withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Token</Table.Th>
                  <Table.Th style={{ width: 140 }}>Created at</Table.Th>
                  <Table.Th style={{ width: 140 }}>Last used</Table.Th>
                  <Table.Th style={{ width: 140 }}>Expires</Table.Th>
                  <Table.Th style={{ width: 70, textAlign: 'right' }} aria-label="Remove token" />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {tokens.map(t => (
                  <Table.Tr key={t._id}>
                    <Table.Td style={{ fontFamily: 'var(--mantine-font-family-mono)' }}>
                      <Text size="sm" fw={600}>
                        {t.accessTokenPartial || 'olp_'}
                        {'\u2022'.repeat(12)}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{fmtDate(t.createdAt)}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{fmtDate(t.lastUsedAt)}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{fmtDate(t.expiresAt)}</Text>
                    </Table.Td>
                    <Table.Td style={{ textAlign: 'right' }}>
                      <Tooltip label="Remove token" withArrow>
                        <ActionIcon
                          variant="subtle"
                          color="red"
                          aria-label="Remove token"
                          onClick={() => setRevoking(t)}
                        >
                          <Icon name="delete" size={17} />
                        </ActionIcon>
                      </Tooltip>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}

          <Group gap="sm">
            <Button
              size="sm"
              color="ollitex"
              loading={adding}
              disabled={atLimit}
              onClick={() => void addToken()}
              leftSection={<Icon name="add" size={15} />}
            >
              Add another token
            </Button>
            {atLimit ? (
              <Text size="xs" c="dimmed">
                Token limit reached (10) — remove one to add a new token.
              </Text>
            ) : null}
          </Group>
        </Stack>
      </Card>

      {/* one-time raw token reveal */}
      <Modal opened={!!newTokenRaw} onClose={() => setNewTokenRaw(null)} title="New Git token" size="md">
        <Text size="sm" c="dimmed" mb="md">
          Copy this token now — for security it is shown one time only and cannot be revealed again.
        </Text>
        <Group gap="xs" align="center" wrap="nowrap">
          <Code block style={{ flex: 1, overflowX: 'auto', wordBreak: 'break-all' }}>
            {newTokenRaw || ''}
          </Code>
          <CopyButton value={newTokenRaw || ''}>
            {({ copied, copy }) => (
              <Button
                size="sm"
                variant="default"
                onClick={copy}
                label={copied ? 'Copied' : 'Copy'}
                leftSection={<Icon name={copied ? 'check' : 'copy_all'} size={15} />}
              />
            )}
          </CopyButton>
        </Group>
        <Group justify="flex-end" mt="md">
          <Button color="ollitex" size="sm" onClick={() => setNewTokenRaw(null)}>
            Done
          </Button>
        </Group>
      </Modal>

      <ConfirmModal
        open={!!revoking}
        title="Remove Git token?"
        body="Git clones that use this token will stop working."
        confirmLabel="Remove"
        danger
        loading={deleting}
        onCancel={() => setRevoking(null)}
        onConfirm={() => void doRevoke()}
      />
    </Stack>
  )
}
