import React from 'react'
import { Button, Group, Text } from '@mantine/core'
import Icon from './icons'

/**
 * Accessible pager for the hub tables (2026-09-08, item #10 a11y baseline).
 *
 * Mantine's <Pagination> in 9.6 renders icon-only prev/next buttons with NO
 * accessible names (axe `button-name` critical) — so the hub ships its own
 * minimal pager: real text buttons with explicit aria-labels, plus a
 * "Page X of Y" text for screen-reader and visual context.
 *
 * Visual parity with the Mantine pager: same size/variant language, right
 * aligned in the page footer.
 */
export default function HubPagination({
  page,
  total,
  onChange,
  disabled,
}: {
  page: number
  total: number
  onChange: (p: number) => void
  disabled?: boolean
}) {
  if (total <= 1) return null

  return (
    <Group gap="xs" justify="flex-end" align="center">
      <Text size="sm" c="dimmed">
        Page {page} of {total}
      </Text>
      <Button
        size="xs"
        variant="default"
        disabled={disabled || page <= 1}
        onClick={() => onChange(page - 1)}
        leftSection={<Icon name="chevron_left" size={16} />}
        aria-label="Previous page"
      >
        Prev
      </Button>
      <Button
        size="xs"
        variant="default"
        disabled={disabled || page >= total}
        onClick={() => onChange(page + 1)}
        rightSection={<Icon name="chevron_right" size={16} />}
        aria-label="Next page"
      >
        Next
      </Button>
    </Group>
  )
}
