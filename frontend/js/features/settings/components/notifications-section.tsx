import { useTranslation } from 'react-i18next'

function NotificationsSection() {
  const { t } = useTranslation()

  return (
    <>
      <h3>{t('email_preferences')}</h3>
      <a href="/hub#/mysettings.email">
        {t('manage_email_preferences')}
      </a>
    </>
  )
}

export default NotificationsSection
