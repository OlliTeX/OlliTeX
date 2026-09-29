/**
 * Typst toolbar button group (F2.8).
 *
 * Registered via `Settings.overleafModuleImports.sourceEditorToolbarButtonGroups`
 * (baked into the bundle at build time — flag OFF does not remove it from
 * the bundle; it simply renders nothing unless the open doc is `.typ`, so
 * the component is a no-op on flag-off instances that have no `.typ`
 * project).
 *
 * Salvaged from the old typst fork (Levi Zim, kxxt). Bold/italic use
 * Typst-style `*…*`/`_…_` wrapping (the lezer grammar marks these as
 * Strong/Emph spans); Format runs the in-browser typstyle wasm formatter
 * (F3.7, `source-editor/extensions/toolbar/typst-format.ts`).
 *
 * The component is shown only when the open doc is a `.typ` file (the
 * generic toolbar button-group loop does not check language per module).
 */
import { FC } from 'react'
import { useTranslation } from 'react-i18next'
import { ToolbarButton } from '@/features/source-editor/components/toolbar/toolbar-button'
import * as commands from '@/features/source-editor/extensions/toolbar/commands'
import { typstFormatDocument } from '@/features/source-editor/extensions/toolbar/typst-format'
import { isMac } from '@/shared/utils/os'
import { useEditorOpenDocContext } from '@/features/ide-react/context/editor-open-doc-context'

export const overflowGroupId = 'group-format'

const TypstToolbarButtons: FC = () => {
  const { t } = useTranslation()
  const { openDocName } = useEditorOpenDocContext()

  // Only for .typ documents (this group has no latex counterpart — the
  // latex bold/italic group is rendered by the core toolbar items).
  if (!/\.typ$/i.test(openDocName || '')) {
    return null
  }

  return (
    <div
      className="ol-cm-toolbar-button-group"
      data-overflow={overflowGroupId}
      aria-label={t('toolbar_text_style')}
    >
      <ToolbarButton
        id="toolbar-typst-bold"
        label={t('toolbar_bold')}
        command={commands.typstToggleBold}
        icon="format_bold"
        shortcut={isMac ? '⌘B' : 'Ctrl+B'}
      />
      <ToolbarButton
        id="toolbar-typst-italic"
        label={t('toolbar_italic')}
        command={commands.typstToggleItalic}
        icon="format_italic"
        shortcut={isMac ? '⌘I' : 'Ctrl+I'}
      />
      <ToolbarButton
        id="toolbar-typst-format"
        label={t('toolbar_format_code')}
        command={typstFormatDocument}
        icon="code"
        shortcut={isMac ? '⇧⌘F' : 'Ctrl+Shift+F'}
      />
    </div>
  )
}

export default TypstToolbarButtons
