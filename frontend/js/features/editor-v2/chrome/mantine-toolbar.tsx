/**
 * editor-v2 P2 — the renovated toolbar frame (Mantine).
 *
 * P2 strategy: the v2 frame is a Mantine presentation shell that hosts the
 * SAME action sub-components the legacy Toolbar uses (title rename, menu bar,
 * share, layout, history, online-users, request-access, submit, offline
 * indicator). 100% of the toolbar behavior is inherited from those
 * components + contexts; the Mantine layer (bar layout, spacing, tokens)
 * comes from the shell (OlliTProvider + token bridge).
 *
 * The three legacy states (focus mode / history view / normal) are all
 * reproduced with Mantine frames.
 */
import { useCallback } from 'react'
import { Box, Group } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { useLayoutContext } from '@/shared/context/layout-context'
import { useEditorContext } from '@/shared/context/editor-context'
import { useIdeReactContext } from '@/features/ide-react/context/ide-react-context'
import { useFeatureFlag } from '@/shared/context/split-test-context'
import useIsNetworkStalled from '@/features/ide-react/hooks/use-is-network-stalled'
import * as eventTracking from '@/infrastructure/event-tracking'

import { ToolbarLogos } from '@/features/ide-react/components/toolbar/logos'
import { ToolbarMenuBar } from '@/features/ide-react/components/toolbar/menu-bar'
import { ToolbarProjectTitle } from '@/features/ide-react/components/toolbar/project-title'
import { OnlineUsers } from '@/features/ide-react/components/toolbar/online-users'
import ShareProjectButton from '@/features/ide-react/components/toolbar/share-project-button'
import ChangeLayoutButton from '@/features/ide-react/components/toolbar/change-layout-button'
import ShowHistoryButton from '@/features/ide-react/components/toolbar/show-history-button'
import RequestAccessButton from '@/features/ide-react/components/toolbar/request-access-button'
import BackToEditorButton from '@/features/editor-navigation-toolbar/components/back-to-editor-button'
import OLIconButton from '@/shared/components/ol/ol-icon-button'
import OLTooltip from '@/shared/components/ol/ol-tooltip'
import OfflineIndicator from '@/features/ide-react/components/toolbar/offline-indicator'
import importOverleafModules from '../../../../macros/import-overleaf-module.macro'

const [publishModalModules] = importOverleafModules('publishModalToolbarButton')
const SubmitProjectButton = publishModalModules?.import.default

export function MantineToolbar() {
  const { view, restoreView, focusMode, setFocusMode, pdfLayout, setView } =
    useLayoutContext()
  const { cobranding, isRestrictedTokenMember } = useEditorContext()
  const { permissionsLevel } = useIdeReactContext()
  const improvedFlakyConnections = useFeatureFlag(
    'intermittent-connection-improvements'
  )
  const t = useTranslation().t
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
      <Box
        component="nav"
        className="ol-v2-toolbar"
        aria-label={t('project_actions')}
        data-ol-editor-v2="toolbar-focus"
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--mantine-spacing-sm)',
          padding: 'var(--mantine-spacing-xs) var(--mantine-spacing-sm)',
          background: 'var(--bg-primary-themed)',
          borderBottom: '1px solid var(--border-divider-themed)',
        }}
      >
        <ToolbarLogos cobranding={cobranding} />
        <Box component="span" data-testid="v2-focus-title" style={{ flex: 1 }} />
        <Group gap="xs">
          {showViewSwitcher && (
            <OLTooltip
              id="v2-tooltip-switch-view"
              description={switchTooltip}
              overlayProps={{ delay: 0, placement: 'bottom' }}
            >
              <OLIconButton
                icon={switchIcon}
                accessibilityLabel={switchTooltip}
                aria-label={switchTooltip}
                onClick={handleSwitchView}
              />
            </OLTooltip>
          )}
          <OLTooltip
            id="v2-tooltip-exit-focus-mode"
            description={t('exit_focus_mode')}
            overlayProps={{ delay: 0, placement: 'bottom' }}
          >
            <OLIconButton
              icon="close_fullscreen"
              accessibilityLabel={t('exit_focus_mode')}
              aria-label={t('exit_focus_mode')}
              onClick={handleExitFocusMode}
            />
          </OLTooltip>
        </Group>
      </Box>
    )
  }

  if (view === 'history') {
    return (
      <Box
        component="nav"
        className="ol-v2-toolbar"
        aria-label={t('project_actions')}
        data-ol-editor-v2="toolbar-history"
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--mantine-spacing-sm)',
          padding: 'var(--mantine-spacing-xs) var(--mantine-spacing-sm)',
          background: 'var(--bg-primary-themed)',
          borderBottom: '1px solid var(--border-divider-themed)',
        }}
      >
        <BackToEditorButton onClick={handleBackToEditorClick} />
        <ToolbarProjectTitle />
      </Box>
    )
  }

  return (
    <Box
      component="nav"
      className="ol-v2-toolbar"
      aria-label={t('project_actions')}
      data-ol-editor-v2="toolbar"
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--mantine-spacing-sm)',
        padding: 'var(--mantine-spacing-xs) var(--mantine-spacing-sm)',
        background: 'var(--bg-primary-themed)',
        borderBottom: '1px solid var(--border-divider-themed)',
        position: 'relative',
        zIndex: 10,
      }}
    >
      <Group gap="sm" style={{ flexShrink: 0 }}>
        <ToolbarLogos cobranding={cobranding} />
        <ToolbarMenuBar />
      </Group>
      <Box style={{ flex: 1, minWidth: 0 }}>
        <ToolbarProjectTitle />
      </Box>
      <Group gap="sm" style={{ flexShrink: 0 }}>
        {improvedFlakyConnections && (
          <OfflineIndicator isOffline={isOfflineDueToNetworkStall} />
        )}
        {!isOfflineDueToNetworkStall && <OnlineUsers />}
        <RequestAccessButton />
        {!isRestrictedTokenMember && <ShowHistoryButton />}
        <ChangeLayoutButton />
        {shouldDisplaySubmitButton && cobranding && (
          <SubmitProjectButton cobranding={cobranding} />
        )}
        <ShareProjectButton />
      </Group>
    </Box>
  )
}

export default MantineToolbar
