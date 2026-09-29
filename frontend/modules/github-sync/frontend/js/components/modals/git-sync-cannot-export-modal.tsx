import { useTranslation, Trans } from 'react-i18next'
import OLNotification from '@/shared/components/notification'
import {
  OLModalBody,
  OLModalFooter,
} from '@/shared/components/ol/ol-modal'
import { useMantineSurface } from '@/features/editor-v2/mantine-surface'
import { ProjectSyncState } from '../../types/git-sync-types'

type GitSyncCannotExportModalProps = {
  projectSyncState: ProjectSyncState
  handleHide: () => void
}

const GitSyncCannotExportModal = ({ projectSyncState, handleHide }: GitSyncCannotExportModalProps) => {
  const { t } = useTranslation()
  const { Btn } = useMantineSurface() // module Mantine wave (M3)

  return (
    <>
      <OLModalBody>
        <OLNotification
          type="warning"
          content={(
            <Trans
              i18nKey="ask_proj_owner_to_export_to_github"
              shouldUnescape
              tOptions={{ interpolation: { escapeValue: true } }}
              values={{
                projectOwnerEmail: projectSyncState.ownerEmail ?? '?',
              }}
              components={[
                projectSyncState.ownerEmail
                  ? <a href={`mailto:${projectSyncState.ownerEmail}`} aria-label={projectSyncState.ownerEmail} />
                  : <></>
              ]}
            />
          )}
        />
      </OLModalBody>
      <OLModalFooter>
        <Btn
          variant="secondary"
          onClick={handleHide}
        >
          {t('close')}
        </Btn>
      </OLModalFooter>
    </>
  )
}

export default GitSyncCannotExportModal
