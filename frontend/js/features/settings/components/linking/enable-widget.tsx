import { useTranslation } from 'react-i18next'
import { sendMB } from '@/infrastructure/event-tracking'
import OLButton from '@/shared/components/ol/ol-button'

function trackUpgradeClick() {
  sendMB('settings-upgrade-click')
}

type ActionButtonProps = {
  hasFeature?: boolean
  linked?: boolean
  handleUnlinkClick: () => void
  handleLinkClick: () => void
  disabled?: boolean
  linkText?: string
  unlinkText?: string
}

export function ActionButton({
  linked,
  handleUnlinkClick,
  handleLinkClick,
  hasFeature,
  disabled,
  linkText,
  unlinkText,
}: ActionButtonProps) {
  const { t } = useTranslation()
  const linkingText = linkText || t('turn_on')
  const unlinkingText = unlinkText || t('turn_off')
  if (!hasFeature) {
    // OlliTeX fork (free-only): no paid upgrade path for this integration —
    // show a disabled state instead of linking to the removed SaaS plans page.
    return (
      <OLButton variant="primary" disabled>
        <span>{t('disabled')}</span>
      </OLButton>
    )
  } else if (linked) {
    return (
      <OLButton
        variant="danger-ghost"
        onClick={handleUnlinkClick}
        disabled={disabled}
      >
        {unlinkingText}
      </OLButton>
    )
  } else {
    return (
      <OLButton
        variant="secondary"
        disabled={disabled}
        onClick={handleLinkClick}
      >
        {linkingText}
      </OLButton>
    )
  }
}
