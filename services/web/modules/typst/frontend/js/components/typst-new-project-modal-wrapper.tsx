import React, { useState } from 'react'
import { useTranslation } from 'react-i18next'
import useAsync from '@/shared/hooks/use-async'
import { getUserFacingMessage, postJSON } from '@/infrastructure/fetch-json'
import { useRefWithAutoFocus } from '@/shared/hooks/use-ref-with-auto-focus'
import { useLocation } from '@/shared/hooks/use-location'
import Notification from '@/shared/components/notification'
import {
  OLModal,
  OLModalBody,
  OLModalFooter,
  OLModalHeader,
  OLModalTitle,
} from '@/shared/components/ol/ol-modal'
import OLFormControl from '@/shared/components/ol/ol-form-control'
import OLButton from '@/shared/components/ol/ol-button'
import OLForm from '@/shared/components/ol/ol-form'
import OLFormLabel from '@/shared/components/ol/ol-form-label'
import OLFormGroup from '@/shared/components/ol/ol-form-group'
import { useFeatureFlag } from '@/shared/context/split-test-context'

type NewProjectData = {
  project_id: string
}

type Props = {
  onHide: () => void
}

// F3.8: 'basic' default + the one 'article' template (plan §11 "empty + one
// article"). The modal radio sends `template` in the POST body.
const TEMPLATES = [
  { id: 'basic', labelKey: 'blank_typst_project' },
  { id: 'article', labelKey: 'typst_project_template_article' },
]

function TypstNewProjectModalWrapper({ onHide }: Props) {
  const { t } = useTranslation()
  const themed = useFeatureFlag('themed-modals')
  const { autoFocusedRef } = useRefWithAutoFocus<HTMLInputElement>()
  const [projectName, setProjectName] = useState('')
  const [template, setTemplate] = useState('basic')
  const [redirecting, setRedirecting] = useState(false)
  const { isLoading, isError, error, runAsync } = useAsync<NewProjectData>()
  const location = useLocation()

  const createNewProject = () => {
    runAsync(
      postJSON('/project/new/typst', {
        body: { projectName, template },
      })
    )
      .then(data => {
        if (data.project_id) {
          setRedirecting(true)
          location.assign(`/project/${data.project_id}`)
        }
      })
      .catch(() => {})
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    createNewProject()
  }

  return (
    <OLModal show animation themed={themed} onHide={onHide} id="typst-new-project-modal" backdrop="static">
      <OLModalHeader>
        <OLModalTitle>{t('new_project')}</OLModalTitle>
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
        <OLForm onSubmit={handleSubmit}>
          <OLFormGroup controlId="typst-project-name">
            <OLFormLabel>{t('project_name')}</OLFormLabel>
            <OLFormControl
              type="text"
              ref={autoFocusedRef}
              onChange={e => setProjectName(e.currentTarget.value)}
              value={projectName}
            />
          </OLFormGroup>
          <OLFormGroup controlId="typst-project-template">
            <OLFormLabel>{t('project_template')}</OLFormLabel>
            {TEMPLATES.map(({ id, labelKey }) => (
              <OLFormLabel key={id} htmlFor={`typst-template-${id}`}>
                <input
                  id={`typst-template-${id}`}
                  type="radio"
                  name="typst-template"
                  value={id}
                  checked={template === id}
                  onChange={() => setTemplate(id)}
                />
                {' '}
                {t(labelKey)}
              </OLFormLabel>
            ))}
          </OLFormGroup>
        </OLForm>
      </OLModalBody>

      <OLModalFooter>
        <OLButton variant="secondary" onClick={onHide}>
          {t('cancel')}
        </OLButton>
        <OLButton
          variant="primary"
          onClick={createNewProject}
          disabled={projectName === '' || isLoading || redirecting}
          isLoading={isLoading}
          loadingLabel={t('creating')}
        >
          {t('create')}
        </OLButton>
      </OLModalFooter>
    </OLModal>
  )
}

export default TypstNewProjectModalWrapper
