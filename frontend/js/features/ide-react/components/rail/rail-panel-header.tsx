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
    // live-audit 023 (symbol palette "X closes the railbar tab instead"): the
    // modern rail is multi-tab (rail-panel.tsx renders one Tab.Pane per rail
    // entry), so the pane header's X must close the ACTIVE PANE — return to
    // the default tab (file-tree) and keep the rail open — not collapse the
    // whole rail from inside a pane. X on the default tab itself falls back
    // to the previous collapse behaviour.
    if (selectedTab !== 'file-tree') {
      selectTab('file-tree')
      return
    }
    handlePaneCollapse()
    if (onClose) {
      onClose()
    }
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
