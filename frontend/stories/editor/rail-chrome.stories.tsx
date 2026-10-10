import { FC } from 'react'
import type { Meta } from '@storybook/react-webpack5'
import {
  RAIL_MENU_PATHS,
  RailHomeGlyph,
  RailMenuGlyph,
} from '@/features/editor-v2/chrome/mantine-rail'

// -- Wave C (editor chrome): the v2 rail icon CONTRACT.
//
// These stories pin the exact glyph set + the top/bottom cluster layout the
// owner approved (2026-10-08/09): five menus on top (File/Edit/Insert/
// View/Format), Help moved DOWN to the bottom cluster beside the house
// home entry. Rendered from the SAME exported components the live rail
// uses (mantine-rail.tsx) so a glyph regression breaks here, not in
// production. Context-free by design (pure SVG — no editor contexts,
// no Mantine provider needed).

const ICON_BOX: React.CSSProperties = {
  width: 40,
  height: 40,
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  borderRadius: 6,
  color: '#31343a',
}

const RailBox: FC<{ label: string }> = ({ label }) => (
  <div style={ICON_BOX} title={label} data-testid={`rail-glyph-${label}`}>
    <RailMenuGlyph label={label} />
  </div>
)

const TOP_MENUS = ['File', 'Edit', 'Insert', 'View', 'Format']

type Story = FC & { parameters?: Record<string, unknown> }

const meta: Meta<FC> = {
  title: 'Editor / Rail / v2 Icon Chrome',
  component: RailMenuGlyph,
  tags: ['autodocs'],
  parameters: {
    docs: {
      description: {
        component:
          'The renovated rail icon chrome (editor-v2 P2). Glyphs are inline SVG ' +
          '(stroke=currentColor, 20px) — the owner contract: File · Edit · Insert · ' +
          'View · Format on top, Help at the bottom beside the house home entry. ' +
          'Source: features/editor-v2/chrome/mantine-rail.tsx.',
      },
    },
  },
}

export default meta

export const MenuGlyphs: Story = () => (
  <div style={{ display: 'inline-flex', gap: 6, padding: 12, border: '1px solid #d9dce0', borderRadius: 8 }}>
    {TOP_MENUS.map(m => <RailBox key={m} label={m} />)}
    <div style={{ width: 1, margin: '4px 6px', background: '#d9dce0' }} />
    <RailBox label="Help" />
  </div>
)
MenuGlyphs.parameters = { docs: { description: { story: 'All six menu glyphs (five top menus + Help) as rendered in the rail.' } } }

export const HomeGlyph: Story = () => (
  <div style={{ ...ICON_BOX, background: '#f4f5f7' }} data-testid="rail-glyph-home">
    <RailHomeGlyph />
  </div>
)
HomeGlyph.parameters = { docs: { description: { story: 'The house home-entry glyph (owner 2026-10-09: replaces the old brand-logo paint).' } } }

export const RailLayoutContract: Story = () => (
  <div
    style={{
      width: 56,
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      gap: 6,
      padding: '10px 0',
      background: '#f4f5f7',
      border: '1px solid #d9dce0',
      borderRadius: 8,
    }}
    data-testid="rail-layout-contract"
  >
    {TOP_MENUS.map(m => <RailBox key={m} label={m} />)}
    <div style={{ flex: 1, width: 24, minHeight: 120, border: '1px dashed #c3c9cf', borderRadius: 6, opacity: 0.6 }} />
    <RailBox label="Help" />
    <div style={{ ...ICON_BOX, background: '#e6e9ec' }}>
      <RailHomeGlyph />
    </div>
  </div>
)
RailLayoutContract.parameters = { docs: { description: { story: 'The approved final layout: top cluster (File…Format), spacer, bottom cluster (Help + house Home).' } } }

export const GlyphPathTable: Story = () => (
  <div style={{ fontFamily: 'ui-monospace, monospace', fontSize: 12, lineHeight: 1.6, maxHeight: 320, overflow: 'auto', border: '1px solid #d9dce0', borderRadius: 8, padding: 12 }}>
    {Object.entries(RAIL_MENU_PATHS).map(([k, v]) => (
      <div key={k} style={{ marginBottom: 8 }}>
        <b>{k}</b>
        <div style={{ wordBreak: 'break-all', opacity: 0.75 }}>{String(v)}</div>
      </div>
    ))}
  </div>
)
GlyphPathTable.parameters = { docs: { description: { story: 'Byte-pin of every glyph path (regression sentinel).' } } }
