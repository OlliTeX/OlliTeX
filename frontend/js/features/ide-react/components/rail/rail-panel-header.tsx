import { useTranslation } from 'react-i18next'
import { useRailContext } from '@/features/ide-react/context/rail-context'
import OLIconButton from '@/shared/components/ol/ol-icon-button'
import React, { useCallback } from 'react'
import OLTooltip from '@/shared/components/ol/ol-tooltip'

export default function RailPanelHeader({
  title,
  actions,
  onClose,
}: {
  title: React.ReactNode
  actions?: React.ReactElement
  onClose?: () => void
}) {
  const { t } = useTranslation()
  const { handlePaneCollapse, selectedTab, selectTab } = useRailContext()

  const handleClose = useCallback(() => {
    // overleaf-lab (owner request I, 2026-10-07, re: live-audit 023): a pane
    // that supplies its OWN close action (e.g. the symbol-palette bottom panel
    // hiding itself) wins — close the PANE, never collapse the whole rail from
    // inside it. Only when a pane has no bespoke close do we fall back to the
    // generic rail behaviour: return to the default tab if one is active, else
    // collapse the rail (X on the file-tree tab keeps its original collapse).
    if (onClose) {
      onClose()
      return
    }
    if (selectedTab !== 'file-tree') {
      selectTab('file-tree')
      return
    }
    handlePaneCollapse()
  }, [selectedTab, selectTab, handlePaneCollapse, onClose])

  return (
    <div className="rail-panel-header">
      <h4 className="rail-panel-title">{title}</h4>

      <div className="rail-panel-header-actions">
        {actions}
        <OLTooltip
          id="close-rail-panel"
          description={t('close')}
          overlayProps={{ placement: 'bottom' }}
        >
          <OLIconButton
            onClick={handleClose}
            className="rail-panel-header-button-subdued"
            icon="close"
            accessibilityLabel={t('close')}
            size="sm"
          />
        </OLTooltip>
      </div>
    </div>
  )
}
