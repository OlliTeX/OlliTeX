import { useTranslation, Trans } from 'react-i18next'
import getMeta from '@/utils/meta'
import {
  OLModalBody,
  OLModalFooter,
} from '@/shared/components/ol/ol-modal'
import { useMantineSurface } from '@/features/editor-v2/mantine-surface'
import OLNotification from '@/shared/components/notification'

import { ProjectSyncState, GitSyncModalStatus } from '../../types/git-sync-types'

type GitSyncConflictModalProps = {
  projectSyncState: ProjectSyncState
  handleHide: () => void
  setModalStatus: (modalStatus: GitSyncModalStatus) => void
}

const GitSyncConflictModal = ({ projectSyncState, handleHide, setModalStatus }: GitSyncConflictModalProps) => {
  const { t } = useTranslation()
  const { Btn } = useMantineSurface() // module Mantine wave (M3)

  const { appName } = getMeta('ol-ExposedSettings')

  return (
    <>
      <OLModalBody>
        <OLNotification
          type="warning"
          content={t('github_merge_failed_error', { appName })}
        />
        <p className="mt-2">
          <Trans
            i18nKey="github_manual_merge_user_prompt"
            shouldUnescape
            tOptions={{ interpolation: { escapeValue: true } }}
            values={{sharelatex_branch: projectSyncState.unmergedBranchName }}
            components={[<b key="sharelatex_branch" />]}
          />
        </p>
      </OLModalBody>
      <OLModalFooter>
        <Btn
          variant="secondary"
          onClick={handleHide}
        >
          {t('close')}
        </Btn>
        <Btn
          variant="primary"
          onClick={
            () => setModalStatus('run-merge-resolved')
          }
        >
          {t('continue_github_merge')}
        </Btn>
      </OLModalFooter>
    </>
  )
}

export default GitSyncConflictModal
