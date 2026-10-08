import { useContext } from 'react'
import RailPanelHeader from '@/features/ide-react/components/rail/rail-panel-header'
import { useTranslation } from 'react-i18next'
import SymbolPaletteContent from './symbol-palette-content'
import { SymbolPaletteConfigProvider } from '../context/symbol-palette-context'
// overleaf-lab (owner request I, 2026-10-07): the symbol palette is also the
// editor's bottom panel (editor.tsx, toggled by the toolbar). When it is that
// panel, its X must HIDE the palette (not collapse the left rail). Read the
// editor-properties context safely (the panel is also a rail tab where the
// provider is guaranteed, but read via useContext so a stray mount outside it
// degrades to the default rail-close behaviour instead of crashing).
import { EditorPropertiesContext } from '@/features/ide-react/context/editor-properties-context'

export default function SymbolPalettePanel() {
    const { t } = useTranslation()
    const editorProps = useContext(EditorPropertiesContext)
    const showPalette = editorProps?.showSymbolPalette ?? false

    const handleSelect = (symbol) => {
        window.dispatchEvent(new CustomEvent('editor:insert-symbol', { detail: symbol }))
    }

    return (
        <SymbolPaletteConfigProvider>
            <div className="symbol-palette-rail-panel">
                <RailPanelHeader
                    title={t('symbol_palette')}
                    // owner request I: as the editor bottom panel, X hides the
                    // palette; as a rail tab (showPalette false) we leave onClose
                    // unset so the header falls back to the rail tab-close.
                    onClose={showPalette ? () => editorProps.setShowSymbolPalette(false) : undefined}
                />
                <div className="symbol-palette-rail-panel-body">
                    <SymbolPaletteContent handleSelect={handleSelect} />
                </div>
            </div>
        </SymbolPaletteConfigProvider>
    )
}
