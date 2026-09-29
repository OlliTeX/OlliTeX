import React from 'react'
import { Alert, Button, Group, Loader, Stack, Text } from '@mantine/core'

export function PageLoading({ label = 'Loading…' }: { label?: string }) {
  return (
    <Stack align="center" gap="sm" py="xl">
      <Loader size="sm" color="ollitex" />
      <Text size="sm" c="dimmed">
        {label}
      </Text>
    </Stack>
  )
}

export function PageError({
  label,
  detail,
  onRetry,
}: {
  label?: string
  detail?: string
  onRetry?: () => void
}) {
  return (
    <Alert icon={null} color="red" withBorder radius="md" style={{ maxWidth: 560 }}>
      <Group wrap="wrap" justify="space-between" align="flex-start" gap="md">
        <div>
          <Text fw={600}>{label || 'Something went wrong'}</Text>
          {detail ? (
            <Text size="sm" c="dark" mt={4} style={{ wordBreak: 'break-word' }}>
              {detail}
            </Text>
          ) : null}
        </div>
        {onRetry ? (
          <Button size="xs" variant="default" onClick={onRetry}>
            Retry
          </Button>
        ) : null}
      </Group>
    </Alert>
  )
}

export function EmptyState({
  icon = 'folder_off',
  title,
  hint,
  action,
}: {
  icon?: string
  title: string
  hint?: string
  action?: React.ReactNode
}) {
  return (
    <Stack align="center" gap="xs" py="xl" ta="center" style={{ maxWidth: 460, margin: '0 auto' }}>
      <span
        className="material-symbols"
        style={{ fontSize: 40, color: 'var(--mantine-color-gray-4)' }}
      >
        {icon}
      </span>
      <Text fw={600}>{title}</Text>
      {hint ? (
        <Text size="sm" c="dimmed">
          {hint}
        </Text>
      ) : null}
      {action ? <Group mt="xs">{action}</Group> : null}
    </Stack>
  )
}
