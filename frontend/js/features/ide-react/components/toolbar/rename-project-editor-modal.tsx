import { memo, useCallback, useState, FC, FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { postJSON, getUserFacingMessage } from '@/infrastructure/fetch-json'
import { useProjectContext } from '@/shared/context/project-context'
import { debugConsole } from '@/utils/debugging'
import { useFeatureFlag } from '@/shared/context/split-test-context'
import useAsync from '@/shared/hooks/use-async'
import Notification from '@/shared/components/notification'
import {
  OLModal,
  OLModalBody,
  OLModalFooter,
  OLModalHeader,
  OLModalTitle,
} from '@/shared/components/ol/ol-modal'
import OLButton from '@/shared/components/ol/ol-button'
import OLForm from '@/shared/components/ol/ol-form'
import OLFormGroup from '@/shared/components/ol/ol-form-group'
import OLFormLabel from '@/shared/components/ol/ol-form-label'
import OLFormControl from '@/shared/components/ol/ol-form-control'

// Owner 2026-10-07 item S: File → "Rename" (rename the project), placed
// below "Make a Copy". Server side: POST /project/:pid/rename
// {newProjectName} (projectlist/rename.go) — owner/admin only.
export interface RenameProjectEditorModalProps {
  show: boolean
  handleHide: () => void
}

const RenameProjectEditorModal: FC<RenameProjectEditorModalProps> = ({
  show,
  handleHide,
}) => {
  const { t } = useTranslation()
  const { project, updateProject } = useProjectContext()
  const themed = useFeatureFlag('themed-modals')
  const { error, isError, isLoading, runAsync } = useAsync()

  const currentName = project?.name ?? ''
  const [newName, setNewName] = useState(currentName)

  // keep the draft in sync with the live project name on (re)open
  const shownName = show ? newName || currentName : currentName

  const trimmed = newName.trim()
  const isValid = trimmed.length > 0 && trimmed !== currentName

  const handleSubmit = useCallback(
    (event: FormEvent) => {
      event.preventDefault()
      if (!isValid || !project?._id) return
      void runAsync(
        postJSON(`/project/${project._id}/rename`, {
          body: { newProjectName: trimmed },
        })
      )
        .then(() => {
          updateProject({ name: trimmed })
          setNewName(trimmed)
          handleHide()
        })
        .catch(debugConsole.error)
    },
    [isValid, project?._id, trimmed, updateProject, handleHide, runAsync]
  )

  return (
    <OLModal
      animation
      show={show}
      onHide={handleHide}
      id="editor-rename-project-modal"
      backdrop="static"
      themed={themed}
    >
      <OLModalHeader>
        <OLModalTitle>{t('rename_project')}</OLModalTitle>
      </OLModalHeader>
      <OLModalBody>
        {isError && (
          <div className="notification-list">
            <Notification
              type="error"
              content={getUserFacingMessage(error) as string}
            />
          </div>
        )}
        <OLForm id="editor-rename-project-form" onSubmit={handleSubmit}>
          <OLFormGroup controlId="editor-rename-project-form-name">
            <OLFormLabel>{t('new_name')}</OLFormLabel>
            <OLFormControl
              type="text"
              required
              value={shownName}
              onChange={event => setNewName((event.target as HTMLInputElement).value)}
            />
          </OLFormGroup>
        </OLForm>
      </OLModalBody>
      <OLModalFooter>
        <OLButton variant="secondary" onClick={handleHide}>
          {t('cancel')}
        </OLButton>
        <OLButton
          variant="primary"
          type="submit"
          form="editor-rename-project-form"
          disabled={isLoading || !isValid}
        >
          {t('rename')}
        </OLButton>
      </OLModalFooter>
    </OLModal>
  )
}

export default memo(RenameProjectEditorModal)
