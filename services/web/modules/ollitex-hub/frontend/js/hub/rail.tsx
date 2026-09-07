import React from 'react'
import { Accordion, Group, Text } from '@mantine/core'
import Icon from '../shared/icons'
import { useOpenSet, accordionState } from './accordion-state'
import type { HubNode } from './nav-tree'

interface RailProps {
  nav: HubNode[]
  active: string | null
  onSelect: (id: string) => void
}

/**
 * Recursive hub rail (nav_structure.md §3):
 * - every folder folds independently → each folder renders its OWN
 *   Mantine Accordion instance (separate id space), so no shared
 *   Accordion context can collapse unrelated branches
 * - open state is centralized (accordion-state) with persistence
 * - leaf = Mantine-styled row; active leaf highlighted
 */
export default function Rail({ nav, active, onSelect }: RailProps) {
  const open = useOpenSet()
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      {nav.map(n => {
        if (n.children && n.children.length > 0) return <Folder key={n.id} node={n} active={active} onSelect={onSelect} open={open} />
        return <Leaf key={n.id} node={n} active={active} onSelect={onSelect} />
      })}
    </div>
  )
}

function Leaf({
  node,
  active,
  onSelect,
}: {
  node: HubNode
  active: string | null
  onSelect: (id: string) => void
}) {
  const isActive = active === node.id
  return (
    <button
      type="button"
      onClick={() => onSelect(node.id)}
      aria-current={isActive ? 'page' : undefined}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        width: '100%',
        padding: '8px 10px',
        borderRadius: 8,
        border: 'none',
        cursor: 'pointer',
        fontSize: 13.5,
        fontWeight: isActive ? 600 : 500,
        textAlign: 'left',
        background: isActive ? 'var(--mantine-color-ollitex-6)' : 'transparent',
        color: isActive ? 'var(--mantine-color-white)' : 'var(--mantine-color-text)',
        transition: 'background 120ms ease',
      }}
      onMouseEnter={e => {
        if (!isActive) (e.currentTarget as HTMLButtonElement).style.background = 'var(--mantine-color-default-hover)'
      }}
      onMouseLeave={e => {
        if (!isActive) (e.currentTarget as HTMLButtonElement).style.background = 'transparent'
      }}
    >
      <Icon
        name={node.icon}
        size={18}
        style={{ color: isActive ? 'var(--mantine-color-white)' : 'var(--mantine-color-dimmed)' }}
      />
      <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{node.label}</span>
    </button>
  )
}

/** One accordion instance per folder (independent fold state). */
function Folder({
  node,
  active,
  onSelect,
  open,
}: {
  node: HubNode
  active: string | null
  onSelect: (id: string) => void
  open: Set<string>
}) {
  const isOpen = open.has(node.id)
  const containsActive = active !== null && (active === node.id || active.startsWith(node.id + '.'))
  const controlBg = containsActive ? 'var(--mantine-color-ollitex-0)' : 'transparent'
  return (
    <Accordion
      id={`hnav-${node.id.replace(/\./g, '_')}`}
      value={isOpen ? 'f' : null}
      onChange={(v: string | null) => {
        const next = v === 'f'
        if (next === isOpen) return
        if (next) accordionState.open(node.id)
        else accordionState.close(node.id)
      }}
      variant="call-out"
      chevronPosition="right"
      styles={{
        root: { background: 'transparent', borderColor: 'transparent' },
        item: { background: 'transparent', borderColor: 'transparent' },
        control: { background: controlBg, borderRadius: 8, padding: '8px 10px', fontSize: 13.5 },
        chevron: { color: 'var(--mantine-color-dimmed)' },
        panel: { paddingTop: 4, paddingBottom: 6, paddingLeft: 8, paddingRight: 4 },
      }}
    >
      <Accordion.Item value="f">
        <Accordion.Control>
          <Group gap={10} wrap="nowrap">
            <Icon name={node.icon} size={18} style={{ color: 'var(--mantine-color-dimmed)' }} />
            <Text size="sm" fw={650} style={{ flex: 1 }} ta="left">
              {node.label}
            </Text>
          </Group>
        </Accordion.Control>
        <Accordion.Panel>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
            {node.children!.map(c =>
              c.children && c.children.length > 0 ? (
                <Folder key={c.id} node={c} active={active} onSelect={onSelect} open={open} />
              ) : (
                <Leaf key={c.id} node={c} active={active} onSelect={onSelect} />
              )
            )}
          </div>
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  )
}
