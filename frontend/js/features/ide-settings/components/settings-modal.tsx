import {
  OLModal,
  OLModalBody,
  OLModalHeader,
  OLModalTitle,
} from '@/shared/components/ol/ol-modal'
import { useTranslation } from 'react-i18next'
import { SettingsModalBody } from './settings-modal-body'
import {
  SettingsModalProvider,
  useSettingsModalContext,
} from '../context/settings-modal-context'
import useFocusOnSetting from '../hooks/use-focus-on-setting'
import useOpenSettingsViaQueryParam from '../hooks/use-open-settings-via-query-param'
import { useFeatureFlag } from '@/shared/context/split-test-context'

const SettingsModalWrapper = () => {
  return (
    <SettingsModalProvider>
      <SettingsModal />
    </SettingsModalProvider>
  )
}

const SettingsModal = () => {
  const { t } = useTranslation()
  const { show, setShow, settingsTabs, activeTab, setActiveTab } =
    useSettingsModalContext()
  const themed = useFeatureFlag('themed-modals')

  useFocusOnSetting()
  useOpenSettingsViaQueryParam()

  return (
    <OLModal
      show={show}
      onHide={() => setShow(false)}
      // Owner #19 (2026-09-13 editor wave): a taller/wider modal — the
      // Compiler (and other) tabs no longer need their own scrollbars,
      // and the label|control rows get room instead of compressed labels
      // (owner #18).
      size="xl"
      backdropClassName={
        activeTab === 'appearance'
          ? 'ide-settings-modal-transparent-backdrop'
          : undefined
      }
      themed={themed}
    >
      <OLModalHeader>
        <OLModalTitle>{t('settings')}</OLModalTitle>
      </OLModalHeader>
      <OLModalBody className="ide-settings-modal-body">
        <SettingsModalBody
          activeTab={activeTab}
          setActiveTab={setActiveTab}
          settingsTabs={settingsTabs}
        />
      </OLModalBody>
    </OLModal>
  )
}

export default SettingsModalWrapper
