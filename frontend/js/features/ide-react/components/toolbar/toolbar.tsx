import { useTranslation } from 'react-i18next'
import { ToolbarMenuBar } from './menu-bar'
import { ToolbarProjectTitle } from './project-title'
import { OnlineUsers } from './online-users'
import ShareProjectButton from './share-project-button'
import ChangeLayoutButton from './change-layout-button'
import RequestAccessButton from './request-access-button'
import { useLayoutContext } from '@/shared/context/layout-context'
import BackToEditorButton from '@/features/editor-navigation-toolbar/components/back-to-editor-button'
import { useCallback } from 'react'
import * as eventTracking from '../../../../infrastructure/event-tracking'
import { ToolbarLogos } from './logos'
import { useEditorContext } from '@/shared/context/editor-context'
import importOverleafModules from '../../../../../macros/import-overleaf-module.macro'
import getMeta from '@/utils/meta'
import { useIdeReactContext } from '@/features/ide-react/context/ide-react-context'
import { useFeatureFlag } from '@/shared/context/split-test-context'
import useIsNetworkStalled from '@/features/ide-react/hooks/use-is-network-stalled'
import OLIconButton from '@/shared/components/ol/ol-icon-button'
import OLTooltip from '@/shared/components/ol/ol-tooltip'
import OfflineIndicator from './offline-indicator'

const [publishModalModules] = importOverleafModules('publishModalToolbarButton')
const SubmitProjectButton = publishModalModules?.import.default

export const Toolbar = () => {
  const { view, restoreView, focusMode, setFocusMode, pdfLayout, setView } =
    useLayoutContext()
  const { cobranding } = useEditorContext()
  const { permissionsLevel } = useIdeReactContext()
  const improvedFlakyConnections = useFeatureFlag(
    'intermittent-connection-improvements'
  )
  const { t } = useTranslation()

  const isOfflineDueToNetworkStall = useIsNetworkStalled()
  const shouldDisplaySubmitButton =
    (permissionsLevel === 'owner' || permissionsLevel === 'readAndWrite') &&
    SubmitProjectButton

  const handleBackToEditorClick = useCallback(() => {
    eventTracking.sendMB('navigation-clicked-history', { action: 'close' })
    restoreView()
  }, [restoreView])

  const handleExitFocusMode = useCallback(() => {
    setFocusMode(false)
    eventTracking.sendMB('focus-mode-exit')
  }, [setFocusMode])

  const handleSwitchView = useCallback(() => {
    const newView = view === 'pdf' ? 'editor' : 'pdf'
    setView(newView)
    eventTracking.sendMB('focus-mode-switch-view', { view: newView })
  }, [view, setView])

  if (focusMode) {
    const showViewSwitcher = pdfLayout === 'flat'
    const switchTooltip =
      view === 'pdf' ? t('switch_to_editor') : t('switch_to_pdf')
    const switchIcon = view === 'pdf' ? 'edit' : 'picture_as_pdf'

    return (
      <nav className="ide-redesign-toolbar" aria-label={t('project_actions')}>
        <div className="ide-redesign-toolbar-menu">
          <ToolbarLogos cobranding={cobranding} />
        </div>
        <ToolbarProjectTitle />
        <div className="ide-redesign-toolbar-actions-wrapper">
          {improvedFlakyConnections && (
            <OfflineIndicator isOffline={isOfflineDueToNetworkStall} />
          )}
          <div className="ide-redesign-toolbar-actions">
            {showViewSwitcher && (
              <div className="ide-redesign-toolbar-button-container">
                <OLTooltip
                  id="tooltip-switch-view"
                  description={switchTooltip}
                  overlayProps={{ delay: 0, placement: 'bottom' }}
                >
                  <OLIconButton
                    icon={switchIcon}
                    className="ide-redesign-toolbar-button-subdued ide-redesign-toolbar-button-icon"
                    onClick={handleSwitchView}
                    accessibilityLabel={switchTooltip}
                  />
                </OLTooltip>
              </div>
            )}
            <ChangeLayoutButton />
            <div className="ide-redesign-toolbar-button-container">
              <OLTooltip
                id="tooltip-exit-focus-mode"
                description={t('exit_focus_mode')}
                overlayProps={{ delay: 0, placement: 'bottom' }}
              >
                <OLIconButton
                  icon="close_fullscreen"
                  className="ide-redesign-toolbar-button-subdued ide-redesign-toolbar-button-icon"
                  onClick={handleExitFocusMode}
                  accessibilityLabel={t('exit_focus_mode')}
                />
              </OLTooltip>
            </div>
          </div>
        </div>
      </nav>
    )
  }

  if (view === 'history') {
    return (
      <nav className="ide-redesign-toolbar" aria-label={t('project_actions')}>
        <div className="d-flex align-items-center">
          <BackToEditorButton onClick={handleBackToEditorClick} />
        </div>
        <ToolbarProjectTitle />
        <div /> {/* Empty div used for spacing */}
      </nav>
    )
  }

  return (
    <nav className="ide-redesign-toolbar" aria-label={t('project_actions')}>
      <div className="ide-redesign-toolbar-menu">
        <ToolbarLogos cobranding={cobranding} />
        <ToolbarMenuBar />
      </div>
      <ToolbarProjectTitle />
      <div className="ide-redesign-toolbar-actions-wrapper">
        {improvedFlakyConnections && (
          <OfflineIndicator isOffline={isOfflineDueToNetworkStall} />
        )}
        <div className="ide-redesign-toolbar-actions">
          {!isOfflineDueToNetworkStall && <OnlineUsers />}
          <RequestAccessButton />
          {/* 2026-10-07 owner item L: removed the toolbar History button —
              "Show Version History" is already in the File menu, so the
              toolbar icon was a duplicate. View switch (editor<->pdf) and
              the View menu remain the canonical entry points. */}
          {/* 2026-10-07 owner item N: removed the toolbar Layout options
              dropdown (#layout-dropdown-btn) from the NORMAL toolbar —
              "Change Layout" lives in the View menu. NOTE: the focus-mode
              toolbar above intentionally keeps its ChangeLayoutButton (the
              menu bar is hidden in focus mode, so that button is the only
              layout entry point there). */}
          {shouldDisplaySubmitButton && cobranding && (
            <SubmitProjectButton cobranding={cobranding} />
          )}
          <ShareProjectButton />
        </div>
      </div>
    </nav>
  )
}
