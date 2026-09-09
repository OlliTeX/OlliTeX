import { useTranslation, Trans } from 'react-i18next'
import useAsync from '@/shared/hooks/use-async'
import { deleteJSON } from '@/infrastructure/fetch-json'
import { debugConsole } from '@/utils/debugging'
import { useState, useEffect } from 'react'
import {
  OLModal,
  OLModalHeader,
  OLModalTitle,
  OLModalBody,
  OLModalFooter,
} from '@/shared/components/ol/ol-modal'
import { useMantineSurface } from '@/features/editor-v2/mantine-surface'
import OLNotification from '@/shared/components/notification'

type ZoteroLinkInfoModalModalProps = {
  show: boolean
  isError: any
  handleHide: () => void
}

const ZoteroLinkInfoModal = ({ show, isError, handleHide }: ZoteroLinkInfoModalModalProps) => {
  const { t } = useTranslation()
  const { Btn } = useMantineSurface() // module Mantine wave (M2)

  const [showUnlinkInfo, setShowUnlinkInfo] = useState(false)

  const {
    isLoading: isUnlinking,
    runAsync: runAsyncUnlink,
  } = useAsync<void>()

  const handleUnlink = () => {
    runAsyncUnlink(deleteJSON('/user/zotero'))
      .catch(err => debugConsole.error(err?.data?.message || err?.message || err))
    handleHide()
  }

  useEffect(() => {
    if (!show) return
    setShowUnlinkInfo(false)
  }, [show])

  return (
    <>
      <OLModal show={show} onHide={handleHide} backdrop="static">
      <OLModalHeader closeButton>
        <OLModalTitle>{t('zotero_integration')}</OLModalTitle>
      </OLModalHeader>

      <OLModalBody>
        {showUnlinkInfo ? (
          <OLNotification
            type="warning"
            content={t('unlink_warning_reference', { provider: t('zotero') })}
          />
        ) : isError ? (
          <OLNotification
            type="error"
            content={t('problem_checking_connection_with_provider', {
              provider: t('zotero'),
            })}
          />
        ) : (
          <OLNotification
            type="info"
            content={
              <Trans
                i18nKey="you_currently_have_x_linked_with_your_overleaf_account"
                values={{ managers: t('zotero') }}
                components={[<b key="b" />]}
              />
            }
          />
        )}
      </OLModalBody>

      <OLModalFooter>
        {showUnlinkInfo ? (
          <Btn
            variant="danger"
            disabled={isUnlinking}
            onClick={handleUnlink}
          >
            {t('confirm')}
          </Btn>
        ) : (
          <Btn
            variant="danger-ghost"
            disabled={isUnlinking}
            onClick={() => setShowUnlinkInfo(true)}
          >
            {t('unlink')}
          </Btn>
        )}
        <Btn
          variant="secondary"
          onClick={handleHide}
        >
          {t('close')}
        </Btn>
      </OLModalFooter>
      </OLModal>
    </>
  )
}

export default ZoteroLinkInfoModal
