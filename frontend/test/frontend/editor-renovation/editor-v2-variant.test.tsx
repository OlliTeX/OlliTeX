/**
 * editor-v2 variant runtime — P1 baseline (EDITOR_RENOVATION_PLAN.md).
 *
 * Freezes the variant-selection contract that every renovated surface
 * depends on: which URL routes to which UI variant, and the single
 * predicate that gates Mantine rendering behind a ready shell.
 */
import { describe, it, expect } from 'vitest'
import {
  variantForPathname,
  detectEditorUiVariant,
  canUseMantineSurface,
  EditorUiShell,
  MantineSurfaceGate,
  EditorUiContext,
} from '@/features/editor-v2/variant'
import React from 'react'
import { render, screen } from '@testing-library/react'

describe('editor-v2 variant runtime (P1 baseline)', () => {
  it('maps /editor* to the mantine variant and everything else to legacy', () => {
    expect(variantForPathname('/editor/abc123')).toBe('mantine')
    expect(variantForPathname('/editor/abc123/detached')).toBe('mantine')
    expect(variantForPathname('/Project/abc123')).toBe('legacy')
    expect(variantForPathname('/project/abc123')).toBe('legacy')
    // /editor as a PREFIX of another resource is NOT the editor
    expect(variantForPathname('/editorial-notes')).toBe('legacy')
  })

  it('detectEditorUiVariant follows the live pathname', () => {
    const original = window.location.pathname
    try {
      Object.defineProperty(window, 'location', {
        configurable: true,
        value: { ...window.location, pathname: '/editor/xyz' },
      })
      expect(detectEditorUiVariant()).toBe('mantine')
      Object.defineProperty(window, 'location', {
        configurable: true,
        value: { ...window.location, pathname: '/Project/xyz' },
      })
      expect(detectEditorUiVariant()).toBe('legacy')
    } finally {
      Object.defineProperty(window, 'location', {
        configurable: true,
        value: { ...window.location, pathname: original },
      })
    }
  })

  it('canUseMantineSurface is true ONLY when mantine AND shell ready AND provider present', () => {
    expect(canUseMantineSurface({ variant: 'mantine', shellReady: false, Provider: () => null })).toBe(false)
    expect(canUseMantineSurface({ variant: 'legacy', shellReady: true, Provider: () => null })).toBe(false)
    expect(canUseMantineSurface({ variant: 'mantine', shellReady: true, Provider: null })).toBe(false)
    expect(canUseMantineSurface({ variant: 'mantine', shellReady: true, Provider: (() => null) as any })).toBe(true)
  })

  it('EditorUiShell renders children through a STABLE passthrough (IDE is never remounted)', () => {
    // jsdom default pathname is / (not /editor) → legacy variant path.
    const first = render(
      <EditorUiShell>
        <div data-testid="child">still here</div>
      </EditorUiShell>
    )
    expect(screen.getByTestId('child')).toBeTruthy()
    // re-rendering must keep the exact same parent element (no remount of
    // the IDE tree underneath — the hard rule behind the P1 #130 crash)
    const parentBefore = screen.getByTestId('child').parentElement
    first.rerender(
      <EditorUiShell>
        <div data-testid="child">still here</div>
      </EditorUiShell>
    )
    expect(screen.getByTestId('child').parentElement).toBe(parentBefore)
  })

  it('MantineSurfaceGate passes children through when the shell is not ready', () => {
    render(
      <MantineSurfaceGate>
        <span data-testid="surface">legacy surface</span>
      </MantineSurfaceGate>
    )
    const el = screen.getByTestId('surface')
    expect(el.parentElement?.getAttribute('data-mantine') ?? null).toBe(null)
  })

  it('MantineSurfaceGate wraps children in the shell provider when the context says ready', () => {
    const FakeProvider = ({ children }: { children: React.ReactNode }) => (
      <div data-mantine="fake-provider">{children}</div>
    )
    render(
      <EditorUiContext.Provider
        value={{ variant: 'mantine', shellReady: true, Provider: FakeProvider }}
      >
        <MantineSurfaceGate>
          <span data-testid="mantined">mantine surface</span>
        </MantineSurfaceGate>
      </EditorUiContext.Provider>
    )
    const el = screen.getByTestId('mantined')
    expect(el.parentElement?.getAttribute('data-mantine')).toBe('fake-provider')
  })
})
