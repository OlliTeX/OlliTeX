// /hub shared selection machinery — owner items #18–21.
// Checkbox column + select-all header + bulk action toolbar for the
// admin users/projects lists (legacy admin-tools parity).

import React, { useCallback, useMemo, useState } from 'react'
import { ActionIcon, Badge, Button, Checkbox, Group, Paper, Text } from '@mantine/core'
import Icon from './icons'

export function useSelection(ids: string[]) {
  const [selected, setSelected] = useState<Set<string>>(() => new Set())
  const key = ids.join('\u0000')
  const visible = useMemo(() => new Set(ids), [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const cleanSelected = useMemo(
    () => new Set([...selected].filter(id => visible.has(id))),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [selected, key]
  )
  const isAllSelected = ids.length > 0 && ids.every(id => cleanSelected.has(id))
  const toggle = useCallback((id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])
  const toggleAll = useCallback(() => {
    setSelected(prev => {
      const next = new Set(prev)
      const all = ids.length > 0 && ids.every(id => next.has(id))
      if (all) {
        for (const id of ids) next.delete(id)
      } else {
        for (const id of ids) next.add(id)
      }
      return next
    })
  }, [key]) // eslint-disable-line react-hooks/exhaustive-deps
  const clear = useCallback(() => setSelected(new Set()), [])
  return {
    selected: cleanSelected,
    count: cleanSelected.size,
    isAllSelected,
    toggle,
    toggleAll,
    clear,
    isSelected: (id: string) => cleanSelected.has(id),
  }
}

export function HeaderCheckbox({
  checked,
  indeterminate,
  onChange,
  label,
}: {
  checked: boolean
  indeterminate?: boolean
  onChange: () => void
  label: string
}) {
  return (
    <div style={{ display: 'flex', justifyContent: 'center' }}>
      <Checkbox
        checked={checked}
        indeterminate={indeterminate}
        onChange={e => {
          void e // controlled
          onChange()
        }}
        aria-label={label}
        style={{ cursor: 'pointer' }}
        onClick={e => e.stopPropagation()}
      />
    </div>
  )
}

export function RowCheckbox({
  id,
  label,
  selected,
  onToggle,
}: {
  id: string
  label: string
  selected: boolean
  onToggle: (id: string) => void
}) {
  return (
    <Checkbox
      checked={selected}
      onChange={() => onToggle(id)}
      aria-label={`Select ${label}`}
      onClick={e => e.stopPropagation()}
      style={{ cursor: 'pointer' }}
    />
  )
}

export type BulkAction = {
  key: string
  label: string
  icon: string
  tone?: 'default' | 'danger'
  onClick: () => void
  disabled?: boolean
  loading?: boolean
}

// Legacy `OLButtonToolbar` equivalent: "N selected" + actions + clear.
export function BulkToolbar({
  count,
  actions,
  onClear,
}: {
  count: number
  actions: BulkAction[]
  onClear: () => void
}) {
  if (count === 0) return null
  return (
    <Paper withBorder shadow={0} radius={10} pos="relative">
      <Group gap="xs" wrap="wrap" pt={4} pr={8} pl={8} pb={4}>
        <Badge size="sm" variant="light" color="teal">
          {count} selected
        </Badge>
        <Group gap={6} wrap="wrap">
          {actions.map(a => (
            <Button
              key={a.key}
              size="xs"
              variant="light"
              color={a.tone === 'danger' ? 'red' : 'teal'}
              leftSection={<Icon name={a.icon} size={15} />}
              onClick={a.onClick}
              disabled={a.disabled}
              loading={a.loading}
              aria-label={`Bulk ${a.label}`}
            >
              {a.label}
            </Button>
          ))}
        </Group>
        <Text size="xs" c="dimmed">
          Bulk action applies to the checked rows in this view only.
        </Text>
        <ActionIcon
          variant="subtle"
          color="gray"
          onClick={onClear}
          aria-label="Clear selection"
          title="Clear selection"
        >
          <Icon name="close" size={15} />
        </ActionIcon>
      </Group>
    </Paper>
  )
}
