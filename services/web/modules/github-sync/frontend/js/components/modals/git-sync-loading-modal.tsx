import { useTranslation } from 'react-i18next'
import {
  OLModalBody,
  OLModalFooter,
} from '@/shared/components/ol/ol-modal'
import { useMantineSurface } from '@/features/editor-v2/mantine-surface'
import OLNotification from '@/shared/components/notification'
import { GitSyncModalStatus } from '../../types/git-sync-types'

type GitSyncLoadingModalProps = {
  handleHide: () => void
  setModalStatus: (modalStatus: GitSyncModalStatus) => void
  errorMessage: string | null
}
const GitSyncLoadingModal = ({ handleHide, setModalStatus, errorMessage }: GitSyncLoadingModalProps) => {
  const { t } = useTranslation()
  const { Btn } = useMantineSurface() // module Mantine wave (M3)


  return (
    <>
      <OLModalBody>
        <div role="status" className="loading align-items-start">
          <div aria-hidden="true" data-testid="ol-spinner" className="spinner-border spinner-border-sm"></div>
          {t('checking_project_github_status')}
        </div>
      </OLModalBody>

      {errorMessage && (
        <OLNotification
          type="error"
          content={errorMessage}
        />
      )}

      <OLModalFooter>
        {errorMessage && (
          <Btn
            variant="danger-ghost"
            onClick={() => setModalStatus('confirm-unlink')}
          >
            {t('unlink')}
          </Btn>
        )}
        <Btn
          variant="secondary"
          onClick={handleHide}
        >
          {t('cancel')}
        </Btn>
      </OLModalFooter>
    </>
  )
}

export default GitSyncLoadingModal
