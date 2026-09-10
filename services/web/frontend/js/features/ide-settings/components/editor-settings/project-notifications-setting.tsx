import { useTranslation } from 'react-i18next'
import RadioButtonSetting, { RadioOption } from '../radio-button-setting'
import {
  SettableNotificationLevel,
  useProjectNotificationPreferences,
} from '../../hooks/use-project-notification-preferences'
import LoadingSpinner from '@/shared/components/loading-spinner'

export default function ProjectNotificationsSetting() {
  const { t } = useTranslation()
  const { notificationLevel, setNotificationLevel, isLoading } =
    useProjectNotificationPreferences()

  if (isLoading) {
    return <LoadingSpinner loadingText={t('loading')} />
  }

  // Owner #19b/#21 (2026-09-13 editor wave): the fine-grained choice
  // ("All project activity" / "Replies to your activity only") and the
  // per-activity-type table moved to the hub (My settings › Email —
  // /hub#/mysettings.email). Here the setting is just On/Off + the link.
  // (The old link pointed at /user/notification-preferences, which is not
  // the canonical settings surface on this deployment.)
  const options: Array<RadioOption<SettableNotificationLevel>> = [
    {
      value: 'all',
      label: t('on'),
      description: t('all_project_activity_description'),
    },
    {
      value: 'off',
      label: t('off'),
      description: t('no_project_notifications_description'),
    },
  ]

  return (
    <>
      {notificationLevel === 'global-off' ? (
        <div className="ide-setting-description">
          {t('project_notifications_muted_description')}{' '}
          <a
            href="/hub#/mysettings.email"
            target="_blank"
            rel="noopener noreferrer"
          >
            {t('change_settings')}
          </a>
        </div>
      ) : (
        <>
          <RadioButtonSetting
            id="projectNotifications"
            options={options}
            // a project saved with the legacy "replies" level reads as "on"
            // here (its exact type mix is managed on the hub page)
            value={
              notificationLevel === 'all' || notificationLevel === 'replies'
                ? 'all'
                : 'off'
            }
            onChange={value => setNotificationLevel(value)}
          />

          <div className="global-notifications-link">
            <a
              href="/hub#/mysettings.email"
              target="_blank"
              rel="noopener noreferrer"
            >
              {t('manage_overleaf_email_preferences')}
            </a>
          </div>
        </>
      )}
    </>
  )
}
