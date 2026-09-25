// 2026-09 (owner request M): symbol palette v2 in the left rail.
//
// The classic palette lives in modules/symbol-palette (its own rail entry,
// key "symbol-palette"); this entry exposes the redesigned React palette
// pane (the same component the editor split renders) as a second rail tab
// so users can choose which palette they want (both stay available).
import SymbolPalettePane from './symbol-palette-pane'

const symbolPaletteV2RailEntry = {
    key: 'symbol-palette-v2',
    icon: 'functions',
    title: 'Symbol palette (new)',
    component: <SymbolPalettePane />,
}

export default symbolPaletteV2RailEntry
