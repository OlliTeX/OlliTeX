import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import useIsMounted from '@/shared/hooks/use-is-mounted'
import { useMantineSurface } from '@/features/editor-v2/mantine-surface'
import EditTemplateModal from './modals/edit-template-modal'
import { useTemplateContext } from '../context/template-context'
import { updateTemplate } from '../util/api'
import type { Template } from '../../../../../types/template'

export default function EditTemplateButton() {
  const { t } = useTranslation()
  const { Btn } = useMantineSurface() // module Mantine wave (M5)

  const [showModal, setShowModal] = useState(false)
  const isMounted = useIsMounted()
  const { template, setTemplate } = useTemplateContext()

  const handleOpenModal = () => {
    setShowModal(true)
  }

  const handleCloseModal = () => {
    if (isMounted.current) {
      setShowModal(false)
    }
  }

  const handleEditTemplate = async (editedTemplate: Template) => {
    const updated = await updateTemplate({ editedTemplate, template })
    if (updated) {
      setTemplate(prev => ({ ...prev, ...updated }))
    }
  }

  return (
    <>
      <Btn variant="secondary" onClick={handleOpenModal}>
        {t('edit')}
      </Btn>

      <EditTemplateModal
        showModal={showModal}
        handleCloseModal={handleCloseModal}
        actionHandler={handleEditTemplate}
      />
    </>
  )
}
