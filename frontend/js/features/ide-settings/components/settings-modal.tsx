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
      // AK-4 (owner 2026-10-08): "Settings modal is unnecessary narrow" —
      // widened further (xl 1000 → 1200) so the Compiler pane (now with
      // the sandbox compile-image select, AK-5) has comfortable room.
      // owner 2026-10-10 (item K): STILL too narrow — widen to 1440
      // (maxWidth:100% in OLModal keeps small viewports safe).
      size={1440}
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
