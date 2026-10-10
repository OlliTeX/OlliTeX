/**
 * shared/ol/ol-modal — Mantine-surface SIZE MATRIX (K/M 2026-10-10).
 *
 * These stories pin the rendered width of every OLModal size on the
 * MANTINE surface (the /editor variant) — the surface where three bugs
 * lived and were fixed on 2026-10-10:
 *
 *   K — settings modal size={1440} rendered 440px
 *   M — hotkeys   size={1280} rendered 440px
 *   (root cause: Mantine 9 content is a FLEX item of the modal inner and
 *    the base rule sets `flex: 0 0 var(--modal-size)` = sm/440px; an
 *    inline `width` loses to a flex-basis. ol-modal.tsx now sets
 *    flexBasis + --modal-size inline from the requested size.)
 *
 * Rendered through the SAME code path as /editor (EditorUiContext:
 * variant 'mantine', shellReady, Provider = MantineProvider), so a
 * width regression breaks THIS story, not the IDE.
 */
import React from 'react'
import type { Meta, StoryObj } from '@storybook/react-webpack5'
import { MantineProvider, Text } from '@mantine/core'
import {
  OLModal,
  OLModalHeader,
  OLModalBody,
  OLModalFooter,
  OLModalTitle,
} from '@/shared/components/ol/ol-modal'
import { EditorUiContext } from '@/features/editor-v2/variant'

const MantineShell: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <MantineProvider>{children}</MantineProvider>
)

function SizingProbe({ expectPx }: { expectPx?: number }) {
  // A small in-story width readout so the story itself is self-checking
  // in the Storybook UI (and screenshot-diffable): it shows the real
  // rendered width of the modal content next to the expected value.
  const ref = React.useRef<HTMLDivElement>(null)
  const [w, setW] = React.useState<number | null>(null)
  React.useEffect(() => {
    const id = setInterval(() => {
      if (ref.current && ref.current.classList.contains('mantine-Modal-content')) {
        setW(Math.round(ref.current.getBoundingClientRect().width))
        clearInterval(id)
      }
    }, 50)
    const stop = setTimeout(() => clearInterval(id), 4000)
    return () => {
      clearInterval(id)
      clearTimeout(stop)
    }
  }, [])
  return (
    <div ref={ref} data-testid="ol-modal-size-probe" style={{ outline: '2px solid #006dcc' }}>
      <Text size="sm" c="dimmed">
        modal content width: {w !== null ? `${w}px` : '…'}
        {expectPx ? ` (requested ${expectPx}px)` : ''}
      </Text>
    </div>
  )
}

function SizedModal({ size, expectPx, title }: { size: any; expectPx?: number; title: string }) {
  const [open, setOpen] = React.useState(true)
  return (
    <EditorUiContext.Provider
      value={{ variant: 'mantine', shellReady: true, Provider: MantineShell }}
    >
      <button style={{ marginRight: 12 }} onClick={() => setOpen(true)}>
        open {title}
      </button>
      <OLModal show={open} onHide={() => setOpen(false)} size={size} themed>
        <OLModalHeader>
          <OLModalTitle>{title}</OLModalTitle>
        </OLModalHeader>
        <OLModalBody>
          <SizingProbe expectPx={expectPx} />
          <p>
            The width readout above measures the REAL modal content node on the
            Mantine surface. K/M regression = the readout shows 440px instead of
            the requested size.
          </p>
        </OLModalBody>
        <OLModalFooter>
          <button>Close</button>
        </OLModalFooter>
      </OLModal>
    </EditorUiContext.Provider>
  )
}

type Story = StoryObj

export default {
  title: 'Shared / OLModal / Mantine size matrix',
  tags: ['autodocs'],
  parameters: {
    docs: {
      description: {
        component:
          'OLModal size contract on the Mantine (editor) surface. sm=420, lg/800, xl=1000, ' +
          'numeric N=N, full=fullScreen. Mantine 9 content is flex-based; the fix pins ' +
          'flexBasis to the requested size (K: 1440 settings, M: 1280 hotkeys).',
      },
    },
  },
} as Meta

export const Small: Story = {
  render: () => <SizedModal size="sm" expectPx={420} title="size=sm (420)" />,
}

export const Large: Story = {
  render: () => <SizedModal size="lg" expectPx={800} title="size=lg (800)" />,
}

export const ExtraLarge: Story = {
  render: () => <SizedModal size="xl" expectPx={1000} title="size=xl (1000)" />,
}

export const Numeric1280: Story = {
  render: () => <SizedModal size={1280} expectPx={1280} title="size=1280 (M hotkeys)" />,
}

export const Numeric1440: Story = {
  render: () => <SizedModal size={1440} expectPx={1440} title="size=1440 (K settings)" />,
}

export const FullScreen: Story = {
  render: () => <SizedModal size="full" title="size=full (fullScreen)" />,
}
