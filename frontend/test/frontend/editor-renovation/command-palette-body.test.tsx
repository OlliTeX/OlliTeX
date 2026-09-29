/**
 * command-palette body — behavior baseline (editor renovation P0e, test-debt #1).
 *
 * Freezes the palette body's UI contract BEFORE the Mantine renovation (P6)
 * touches it: result rendering, keyboard navigation (wrapping), selection via
 * Enter and click, graceful command failure (still dismisses), outside-click
 * dismissal, analytics lifecycle. The data hook is stubbed with deterministic
 * results — the query→source rules are frozen separately in
 * command-palette-results.test.ts.
 */
import React from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import CommandPaletteBody from '@/features/command-palette/components/command-palette-body'
import { SplitTestProvider } from '@/shared/context/split-test-context'
import type { CommandPaletteSearchResult } from '@/features/command-palette/types'

function renderBody(props: { show: boolean; onHide: () => void }) {
  return render(
    <SplitTestProvider>
      <CommandPaletteBody show={props.show} onHide={props.onHide} />
    </SplitTestProvider>
  )
}

const sendEvent = vi.fn()

vi.mock('@/shared/hooks/use-editor-analytics', () => ({
  useEditorAnalytics: () => ({ sendEvent }),
}))

const STUB_RESULTS: CommandPaletteSearchResult[] = [
  {
    title: 'First command',
    description: 'desc-1',
    onSelect: () => undefined,
    score: 3,
    eventSegmentation: { source: 'command-registry', item: 'first' },
  },
  {
    title: 'Second command',
    onSelect: () => undefined,
    score: 2,
    eventSegmentation: { source: 'command-registry', item: 'second' },
  },
  {
    title: 'Third command',
    onSelect: () => undefined,
    score: 1,
    eventSegmentation: { source: 'command-registry', item: 'third' },
  },
]

vi.mock('@/features/command-palette/hooks/use-command-palette-results', () => ({
  default: () => STUB_RESULTS,
}))

function getPaletteInput(): HTMLInputElement {
  // OLModal renders through a portal (document.body), not the RTL container
  return document.querySelector<HTMLInputElement>('.command-palette input')
}

function selectedTitles() {
  return [...document.querySelectorAll('.command-palette-result-selected')].map(
    el => el.textContent || ''
  )
}

beforeEach(() => {
  sendEvent.mockClear()
})

describe('command palette body (baseline behavior)', () => {
  it('renders every result with its title and description', async () => {
    renderBody({ show: true, onHide: () => undefined })
    for (const t of STUB_RESULTS.map(r => r.title)) {
      expect(screen.getByText(t)).toBeTruthy()
    }
    expect(screen.getByText('desc-1')).toBeTruthy()
  })

  it('keyboard navigation wraps (ArrowUp from first → last, ArrowDown from last → first)', async () => {
    renderBody({ show: true, onHide: () => undefined })
    const input = getPaletteInput()
    expect(input).toBeTruthy()

    // default selection = first
    expect(selectedTitles()[0]).toContain('First command')
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(selectedTitles()[0]).toContain('Second command')
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(selectedTitles()[0]).toContain('Third command')
    // wrap: down from last → first
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(selectedTitles()[0]).toContain('First command')
    // wrap: up from first → last
    fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(selectedTitles()[0]).toContain('Third command')
  })

  it('Enter runs the selected command and dismisses the palette', async () => {
    const onHide = vi.fn()
    const onSelect = vi.fn()
    STUB_RESULTS[0].onSelect = onSelect
    renderBody({ show: true, onHide: onHide })
    const input = getPaletteInput()
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onSelect).toHaveBeenCalled()
    expect(onHide).toHaveBeenCalled()
  })

  it('click runs the command; a rejecting command still dismisses (graceful failure)', async () => {
    const onHide = vi.fn()
    STUB_RESULTS[1].onSelect = () => Promise.reject(new Error('boom'))
    renderBody({ show: true, onHide: onHide })
    await userEvent.click(screen.getByText('Second command'))
    await waitFor(() => expect(onHide).toHaveBeenCalled())
  })

  it('mousedown outside the palette dialog dismisses it', async () => {
    const onHide = vi.fn()
    renderBody({ show: true, onHide: onHide })
    fireEvent.mouseDown(document.body)
    await waitFor(() => expect(onHide).toHaveBeenCalled())
  })

  it('fires the analytics lifecycle: opened on mount, dismissed when unhidden without a selection', async () => {
    const view = renderBody({ show: true, onHide: vi.fn() })
    await waitFor(() => expect(sendEvent).toHaveBeenCalledWith('command-palette-opened'))
    // the parent unmounts the body when the palette hides (root's `!show → null`)
    view.unmount()
    expect(sendEvent).toHaveBeenCalledWith('command-palette-dismissed')
  })
})
