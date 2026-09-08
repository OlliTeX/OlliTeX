import React from 'react'
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
 * - every folder folds independently (accordion-state store, persisted)
 * - plain custom buttons (no widget CSS surprises — owner review #7:
 *   folder heads must behave exactly like leaf rows)
 * - tone (nav-tree): admin branches render RED text (danger),
 *   user-settings branches BLUE (owner review #24)
 * - open chevron rotates; active leaf highlighted in brand green
 */
const TONE_COLOR: Record<string, string> = {
  admin: 'var(--mantine-color-red-6)',
  user: 'var(--mantine-color-blue-6)',
}

function toneOf(node: HubNode, inherit?: string): string | null {
  if (node.tone) return node.tone
  return inherit
}

function Leaf({
  node,
  active,
  onSelect,
  tone,
}: {
  node: HubNode
  active: string | null
  onSelect: (id: string) => void
  tone?: string | null
}) {
  const isActive = active === node.id
  const toneColor = TONE_COLOR[toneOf(node, tone) || '']
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
        if (!isActive) (e.currentTarget as HTMLButtonElement).style.background = 'color-mix(in srgb, var(--mantine-color-ollitex-6) 12%, transparent)'
      }}
      onMouseLeave={e => {
        if (!isActive) (e.currentTarget as HTMLButtonElement).style.background = 'transparent'
      }}
    >
      {/* a11y 2026-09-08: tone color now decorative only (8px dot) — the label
          text uses --mantine-color-text for a guaranteed 4.5:1 contrast */}
      <span
        aria-hidden="true"
        style={{ width: 7, height: 7, borderRadius: 4, background: toneColor || 'transparent', flexShrink: 0, alignSelf: 'center', marginRight: 2 }}
      />
      <Icon
        name={node.icon}
        size={18}
        style={{
          color: isActive ? 'var(--mantine-color-white)' : 'var(--mantine-color-text)',
          flexShrink: 0,
        }}
      />
      <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{node.label}</span>
    </button>
  )
}

/** A folder row: button head + collapsible body (own open state). */
function Folder({
  node,
  active,
  onSelect,
  open,
  tone,
}: {
  node: HubNode
  active: string | null
  onSelect: (id: string) => void
  open: Set<string>
  tone?: string | null
}) {
  const isOpen = open.has(node.id)
  const containsActive = active !== null && (active === node.id || active.startsWith(node.id + '.'))
  const toneHere = toneOf(node, tone)
  return (
    <div>
      <button
        type="button"
        aria-expanded={isOpen}
        onClick={() => {
          if (isOpen) accordionState.close(node.id)
          else accordionState.open(node.id)
        }}
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
          fontWeight: 650,
          textAlign: 'left',
          background: containsActive ? 'color-mix(in srgb, var(--mantine-color-ollitex-6) 10%, transparent)' : 'transparent',
          color: 'var(--mantine-color-text)',
          transition: 'background 120ms ease',
        }}
        onMouseEnter={e => {
          if (!containsActive) (e.currentTarget as HTMLButtonElement).style.background = 'color-mix(in srgb, var(--mantine-color-ollitex-6) 12%, transparent)'
        }}
        onMouseLeave={e => {
          if (!containsActive) (e.currentTarget as HTMLButtonElement).style.background = 'transparent'
        }}
      >
        <Icon
          name={node.icon}
          size={18}
          style={{
            color: 'var(--mantine-color-text)',
            flexShrink: 0,
          }}
        />
        <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {node.label}
        </span>
        <span
          aria-hidden="true"
          style={{
            display: 'inline-flex',
            transition: 'transform 160ms ease',
            transform: isOpen ? 'rotate(90deg)' : 'rotate(0deg)',
            color: 'var(--mantine-color-text)',
          }}
        >
          <Icon name="chevron_right" size={18} />
        </span>
      </button>
      {isOpen ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2, padding: '2px 0 4px 10px' }}>
          {node.children!.map(c =>
            c.children && c.children.length > 0 ? (
              <Folder
                key={c.id}
                node={c}
                active={active}
                onSelect={onSelect}
                open={open}
                tone={toneHere || tone}
              />
            ) : (
              <Leaf key={c.id} node={c} active={active} onSelect={onSelect} tone={toneHere || tone} />
            )
          )}
        </div>
      ) : null}
    </div>
  )
}

export default function Rail({ nav, active, onSelect }: RailProps) {
  const open = useOpenSet()
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      {nav.map(n =>
        n.children && n.children.length > 0 ? (
          <Folder key={n.id} node={n} active={active} onSelect={onSelect} open={open} />
        ) : (
          <Leaf key={n.id} node={n} active={active} onSelect={onSelect} />
        )
      )}
    </div>
  )
}
