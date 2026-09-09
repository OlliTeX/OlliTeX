import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import useIsMounted from '@/shared/hooks/use-is-mounted'
import { useLocation } from '@/shared/hooks/use-location'
import { useMantineSurface } from '@/features/editor-v2/mantine-surface'
import DeleteTemplateModal from './modals/delete-template-modal'
import { useTemplateContext } from '../context/template-context'
import { deleteTemplate } from '../util/api'
import type { Template } from '../../../../../types/template'

function DeleteTemplateButton() {
  const { t } = useTranslation()
  const { Btn } = useMantineSurface() // module Mantine wave (M5)

  const [showModal, setShowModal] = useState(false)
  const isMounted = useIsMounted()
  const { template } = useTemplateContext()
  const location = useLocation()

  const handleOpenModal = () => {
    setShowModal(true)
  }

  const handleCloseModal = () => {
    if (isMounted.current) {
      setShowModal(false)
    }
  }

  const handleDeleteTemplate = async (template: Template) => {
    await deleteTemplate(template)
    handleCloseModal()
    const previousPage = document.referrer || '/templates'
    location.assign(previousPage)
  }

  return (
    <>
      <Btn variant="danger" onClick={handleOpenModal}>
        {t('delete')}
      </Btn>
      <DeleteTemplateModal
        template={template}
        actionHandler={handleDeleteTemplate}
        showModal={showModal}
        handleCloseModal={handleCloseModal}
      />
    </>
  )
}

export default DeleteTemplateButton
