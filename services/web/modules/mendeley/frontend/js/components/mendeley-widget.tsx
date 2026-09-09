import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import getMeta from '@/utils/meta'
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import useAsync from '@/shared/hooks/use-async'
import { debugConsole } from '@/utils/debugging'
import { useMantineSurface } from '@/features/editor-v2/mantine-surface'
import {
  OLModal,
  OLModalBody,
  OLModalFooter,
  OLModalHeader,
  OLModalTitle,
} from '@/shared/components/ol/ol-modal'
import OLNotification from '@/shared/components/notification'
import MendeleyLogo from '@/shared/svgs/mendeley-logo'

/**
 * Mendeley account linking widget for the references settings section.
 * Mendeley uses an OAuth 2.0 sign-in (unlike Zotero's API-key flow):
 * "link" opens the sign-in popup; "unlink" removes the stored credentials.
 *
 * Registered via overleafModuleImports.referenceLinkingWidgets.
 */
export const MendeleyWidget = function MendeleyWidget() {
  const { t } = useTranslation()
  const { Btn } = useMantineSurface() // module Mantine wave (M2)

  const { appName } = getMeta('ol-ExposedSettings')

  const {
    isLoading: isCheckingConn,
    isError: isErrorConnCheck,
    runAsync: runAsyncConnCheck,
    data: status,
  } = useAsync<{ configured: boolean; connected: boolean }>()

  const {
    isLoading: isUnlinking,
    isError: isErrorUnlink,
    runAsync: runAsyncUnlink,
  } = useAsync<void>()

  const [showUnlinkModal, setShowUnlinkModal] = useState(false)

  const isConnected = Boolean(status?.connected)

  const handleConnCheck = useCallback(() => {
    runAsyncConnCheck(getJSON('/user/mendeley/status'))
      .catch(err =>
        debugConsole.error(err?.data?.message || err?.message || err)
      )
  }, [runAsyncConnCheck])

  useEffect(() => {
    handleConnCheck()
    // the OAuth popup completes while this tab has the focus event
    const onFocus = () => handleConnCheck()
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [handleConnCheck])

  const handleUnlink = useCallback(() => {
    runAsyncUnlink(postJSON('/mendeley/unlink'))
      .catch(err =>
        debugConsole.error(err?.data?.message || err?.message || err)
      )
      .finally(() => setShowUnlinkModal(false))
  }, [runAsyncUnlink])

  // the instance has no Mendeley OAuth app configured — say so, don't 5xx
  if (!isCheckingConn && !isErrorConnCheck && status && !status.configured) {
    return (
      <div className="settings-widget-container">
        <div>
          <MendeleyLogo size={40} />
        </div>
        <div className="description-container">
          <div className="title-row">
            <h4 id="mendeley-link">{t('mendeley')}</h4>
          </div>
          <p className="small">{t('mendeley_not_configured')}</p>
        </div>
      </div>
    )
  }

  if (isCheckingConn) return null

  return (
    <>
      <div className="settings-widget-container">
        <div>
          <MendeleyLogo size={40} />
        </div>

        <div className="description-container">
          <div className="title-row">
            <h4 id="mendeley-link">{t('mendeley')}</h4>
          </div>

          <p className="small">
            {t('mendeley_sync_description', { appName })}
          </p>

          {isErrorConnCheck && (
            <OLNotification
              type="error"
              content={t('problem_checking_connection_with_provider', { provider: t('mendeley') })}
            />
          )}

          {isErrorUnlink && (
            <OLNotification
              type="error"
              content={t('generic_something_went_wrong')}
            />
          )}
        </div>

        <div>
          {isConnected ? (
            <Btn
              variant="danger-ghost"
              onClick={() => setShowUnlinkModal(true)}
              disabled={isUnlinking}
            >
              {isUnlinking ? t('unlinking') : t('unlink')}
            </Btn>
          ) : isErrorConnCheck ? (
            <Btn
              variant="secondary"
              onClick={handleConnCheck}
            >
              {t('reconnect')}
            </Btn>
          ) : (
            <Btn
              variant="secondary"
              href="/user/mendeley/oauth?popup=0"
            >
              {t('link')}
            </Btn>
          )}
        </div>
      </div>

      <OLModal
        id="mendeley-unlink-modal"
        show={showUnlinkModal}
        onHide={() => setShowUnlinkModal(false)}
        backdrop="static"
      >
        <OLModalHeader>
          <OLModalTitle>
            {t('unlink_reference', {
              provider: 'Mendeley',
            })}
          </OLModalTitle>
        </OLModalHeader>

        <OLModalBody>
          <p>
            {t('unlink_warning_reference', {
              provider: 'Mendeley',
            })}
          </p>
        </OLModalBody>

        <OLModalFooter>
          <Btn
            variant="secondary"
            onClick={() => setShowUnlinkModal(false)}
          >
            {t('cancel')}
          </Btn>

          <Btn
            variant="danger-ghost"
            onClick={handleUnlink}
            disabled={isUnlinking}
          >
            {isUnlinking ? t('unlinking') : t('unlink')}
          </Btn>
        </OLModalFooter>
      </OLModal>
    </>
  )
}
