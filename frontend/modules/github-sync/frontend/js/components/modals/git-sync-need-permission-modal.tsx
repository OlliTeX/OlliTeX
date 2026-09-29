import { useTranslation, Trans } from 'react-i18next'
import OLNotification from '@/shared/components/notification'
import {
  OLModalBody,
  OLModalFooter,
} from '@/shared/components/ol/ol-modal'
import { useMantineSurface } from '@/features/editor-v2/mantine-surface'
import { ProjectSyncState } from '../../types/git-sync-types'

type GitSyncNeedPermissionModalProps = {
  projectSyncState: ProjectSyncState
  handleHide: () => void
}

const GitSyncNeedPermissionModal = ({ projectSyncState, handleHide }: GitSyncNeedPermissionModalProps) => {
  const { t } = useTranslation()
  const { Btn } = useMantineSurface() // module Mantine wave (M3)

  return (
    <>
      <OLModalBody>
        <OLNotification
          type="warning"
          content={(
            <Trans
              i18nKey="ask_proj_owner_to_add_you_as_github_collaborator"
              shouldUnescape
              tOptions={{ interpolation: { escapeValue: true } }}
              values={{
                repoFullName: projectSyncState.repoFullName ?? '?',
                projectOwnerEmail: projectSyncState.ownerEmail ?? '?',
              }}
              components={[
                projectSyncState.repoFullName ? (
                  <a
                    href={`https://github.com/${projectSyncState.repoFullName}`}
                    target="_blank"
                    rel="noreferrer noopener"
                    aria-label={projectSyncState.repoFullName}
                  />
                ) : (
                  <></>
                ),
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

export default GitSyncNeedPermissionModal
