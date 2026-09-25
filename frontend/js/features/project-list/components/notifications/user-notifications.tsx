import { useTranslation } from 'react-i18next'
import Common from './groups/common'
import Institution from './groups/institution'
import ConfirmEmail from './groups/confirm-email'
import ReconfirmationInfo from './groups/affiliation/reconfirmation-info'
import AccessibilitySurveyBanner from './accessibility-survey-banner'
import {
  DeprecatedBrowser,
  isDeprecatedBrowser,
} from '@/shared/components/deprecated-browser'

// OlliTeX fork (free-only): SaaS group-subscription enrollment banners,
// geo-based plan upsell banners, groups/enterprise upsell banner and the
// US-gov upsell banner were removed.

function UserNotifications() {
  const { t } = useTranslation()

  return (
    <section
      className="user-notifications notification-list"
      aria-label={t('notification')}
    >
      <ul className="list-unstyled">
        <Common />
        <Institution />
        <ConfirmEmail />
        <ReconfirmationInfo />
        <AccessibilitySurveyBanner />
        {isDeprecatedBrowser() && <DeprecatedBrowser />}
      </ul>
    </section>
  )
}

export default UserNotifications
