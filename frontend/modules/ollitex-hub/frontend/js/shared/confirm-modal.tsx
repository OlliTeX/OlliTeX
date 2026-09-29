import React from 'react'
import { Button, Group, Modal, Text } from '@mantine/core'

interface ConfirmModalProps {
  open: boolean
  title: string
  body?: React.ReactNode
  confirmLabel?: string
  danger?: boolean
  loading?: boolean
  /** gate the confirm button (e.g. typed confirmation for destructive actions) */
  disabled?: boolean
  onCancel: () => void
  onConfirm: () => void
}

export default function ConfirmModal(props: ConfirmModalProps) {
  const { open, title, body, confirmLabel = 'Confirm', danger, loading, onCancel, onConfirm, disabled } = props

  const note = body === undefined ? null : <Text size="sm" c="dimmed" mb="md">{body}</Text>

  const actions = (
    <Group justify="flex-end" gap="xs" mt="md">
      <Button variant="default" onClick={onCancel} disabled={loading}>
        Cancel
      </Button>
      <Button
        color={danger ? 'red' : 'ollitex'}
        variant="filled"
        loading={loading}
        disabled={disabled}
        onClick={onConfirm}
      >
        {confirmLabel}
      </Button>
    </Group>
  )

  return (
    <Modal opened={open} onClose={onCancel} size="sm" title={title}>
      {note}
      {actions}
    </Modal>
  )
}
