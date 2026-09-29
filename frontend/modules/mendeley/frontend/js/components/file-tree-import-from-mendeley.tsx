import { useTranslation } from 'react-i18next'
import { FormEventHandler, useEffect, useState } from 'react'
import * as eventTracking from '@/infrastructure/event-tracking'
import { useFileTreeActionable } from '@/features/file-tree/contexts/file-tree-actionable'
import { useFileTreeCreateForm } from '@/features/file-tree/contexts/file-tree-create-form'
import { useFileTreeCreateName } from '@/features/file-tree/contexts/file-tree-create-name'
import FileTreeCreateNameInput from '@/features/file-tree/components/file-tree-create/file-tree-create-name-input'
import ErrorMessage from '@/features/file-tree/components/file-tree-create/error-message'
import OLFormGroup from '@/shared/components/ol/ol-form-group'
import OLFormLabel from '@/shared/components/ol/ol-form-label'
import OLFormSelect from '@/shared/components/ol/ol-form-select'
import OLNotification from '@/shared/components/notification'

type MendeleyGroup = {
  id: string
  name: string
}

type FileTreeImportFromMendeleyProps = { groups: MendeleyGroup[] }

/**
 * Mendeley create-file pane: pick a library ("My Library" or a group),
 * name the .bib file, import. Mirrors the Zotero pane; Mendeley's API
 * exports BibTeX only (no BibLaTeX).
 */
export default function FileTreeImportFromMendeley({
  groups,
}: FileTreeImportFromMendeleyProps) {
  const { t } = useTranslation()
  const { name } = useFileTreeCreateName()
  const { setValid } = useFileTreeCreateForm()
  const {
    finishCreatingLinkedFile,
    error,
    inFlight
  } = useFileTreeActionable()
  const [selectedGroupId, setSelectedGroupId] = useState<string>('')

  useEffect(() => {
    setValid(!!name)
  }, [name])

  const handleSubmit: FormEventHandler = event => {
    event.preventDefault()
    eventTracking.sendMB('new-file-created', {
      method: 'mendeley',
      extension: name.split('.').length > 1 ? name.split('.').pop() : ''
    })

    finishCreatingLinkedFile({
      name,
      provider: 'mendeley',
      data: {
        ...(selectedGroupId && { mendeleyGroupId: selectedGroupId })
      }
    })
  }

  return (
    <>
      <p>{t('import_a_bibtex_file_from_your_provider_account', { provider: 'Mendeley' })}</p>
      <form
        className="form-controls"
        id="create-file"
        noValidate
        onSubmit={handleSubmit}
      >
        <OLFormGroup controlId="mendeley-library-select">
          <OLFormLabel>{t('library')}</OLFormLabel>
          <OLFormSelect
            id="mendeley-library-select"
            value={selectedGroupId}
            disabled={inFlight}
            onChange={(e: React.ChangeEvent<HTMLSelectElement>) =>
              setSelectedGroupId(e.target.value)
            }
          >
            <option value="">{t('my_library')}</option>
            {groups?.map(g => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </OLFormSelect>
        </OLFormGroup>

        <FileTreeCreateNameInput
          label={t('file_name_in_this_project')}
          placeholder="mendeley.bib"
          error={error}
          inFlight={inFlight}
        />

        {inFlight && (
          <div role="status" className="loading d-flex justify-content-center align-items-center fs-5">
            <div aria-hidden="true" className="spinner-border spinner-border-sm"></div>
            {t('importing') + '…'}
          </div>
        )}

        {error && <ErrorMessage error={error} />}
      </form>
    </>
  )
}
