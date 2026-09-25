import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import getMeta from '../../../utils/meta'
import ManagedAccountAlert from './managed-account-alert'
import AccountInfoSection from './account-info-section'
import PasswordSection from './password-section'
import LinkingSection from './linking-section'
import BetaProgramSection from './beta-program-section'
import LabsProgramSection from './labs-program-section'
import SessionsSection from './sessions-section'
import KeyBindingsCard from './key-bindings-card'
// overleaf-lab: BYO LLM provider management embedded in Account Settings (reviewer #2)
import LLMUserSection from './llm-user-section'
import NewsletterSection from './newsletter-section'
import LeaveSection from './leave-section'
import * as eventTracking from '../../../infrastructure/event-tracking'
import { UserProvider } from '../../../shared/context/user-context'
import { SSOProvider } from '../context/sso-context'
import useWaitForI18n from '../../../shared/hooks/use-wait-for-i18n'
import useScrollToIdOnLoad from '../../../shared/hooks/use-scroll-to-id-on-load'
import { SSOAlert } from './emails/sso-alert'
import OLRow from '@/shared/components/ol/ol-row'
import OLCol from '@/shared/components/ol/ol-col'
import OLPageContentCard from '@/shared/components/ol/ol-page-content-card'
import { isSplitTestEnabled } from '@/utils/splitTestUtils'
import NotificationsSection from './notifications-section'

function SettingsPageRoot() {
  const { isReady } = useWaitForI18n()
  useScrollToIdOnLoad()

  useEffect(() => {
    eventTracking.sendMB('settings-view')
  }, [])

  return (
    <div className="container">
      <OLRow>
        <OLCol xl={{ span: 10, offset: 1 }}>
          {isReady ? <SettingsPageContent /> : null}
        </OLCol>
      </OLRow>
    </div>
  )
}

function SettingsPageContent() {
  const { t } = useTranslation()
  const { isOverleaf, labsEnabled } = getMeta('ol-ExposedSettings')
  // overleaf-lab: gate for the LLM personal-settings section (instance flag).
  const llmAllowUserSettings = !!(getMeta('ol-ExposedSettings') as any)?.llmAllowUserSettings
  // 2026-09 (/user/mysettings shell): the shell already renders the page
  // heading ("Account settings") — drop this duplicate H1 there.
  const isMySettingsShell = !!(getMeta as any)('ol-mySettingsShell')
  const inNotificationsSplitTest = isSplitTestEnabled('email-notifications')
  return (
    <UserProvider>
      <div className="container account-settings-root">
        {/* 2026-09-04 (live-06 #11): golden /admin/site layout — each logical
            block is its OWN card (badge pill + title + body) instead of one
            large flat card. The shared LLM-settings stylesheet provides the
            .settings-sub-card + pinned light-surface recipe. */}
        <OLPageContentCard>
          {!isMySettingsShell ? (
            <div className="page-header">
              <h1>{t('account_settings')}</h1>
            </div>
          ) : null}
          <div>
            <ManagedAccountAlert />
            {/* live-07 #4: the Emails/affiliations card rendered empty in CE
                (email + name + password live under the "Update account info"
                card) — removed on owner request; keep the SSO alert visible. */}
            <SSOAlert />

            {/* 2026-09-09 (owner #3/#12): the account info + password block is
                KEPT (email + password editing); only the duplicated
                "Update account info" card-header line was removed — the
                sections carry their own headings (h3) now. */}
            <div
              id="account-info"
              className="settings-sub-card mb-4"
              style={{ backgroundColor: 'transparent', border: 'none' }}
            >
              <AccountInfoSection />
              <PasswordSection />
            </div>

            {/* live-07 #5: per-user key bindings (Default / Vim / Emacs — the
                keymaps the source editor actually ships; the choice persists
                user-wide and is applied in the editor via setKeybindings()). */}
            <div
              id="key-bindings"
              className="settings-sub-card mb-4"
              style={{ backgroundColor: 'transparent', border: 'none' }}
            >
              <KeyBindingsCard />
            </div>
            <div
              id="linked-settings"
              className="settings-sub-card mb-4"
              style={{ backgroundColor: 'transparent', border: 'none' }}
            >
              <SSOProvider>
                <LinkingSection />
              </SSOProvider>
            </div>
            {llmAllowUserSettings ? (
              <div
                id="llm-user-settings"
                className="settings-sub-card mb-4"
                style={{ backgroundColor: 'transparent', border: 'none' }}
              >
                <LLMUserSection />
              </div>
            ) : null}
            <div
              id="sessions-settings"
              className="settings-sub-card mb-4"
              style={{ backgroundColor: 'transparent', border: 'none' }}
            >
              <SessionsSection />
            </div>
            {isOverleaf ? (
              <>
                {inNotificationsSplitTest ? (
                  <div
                    id="notifications-settings"
                    className="settings-sub-card mb-4"
                    style={{ backgroundColor: 'transparent', border: 'none' }}
                  >
                    <div className="card-header settings-sub-block-header">
                      <strong>{t('email_notifications')}</strong>
                    </div>
                    <NotificationsSection />
                  </div>
                ) : (
                  <div
                    id="newsletter-settings"
                    className="settings-sub-card mb-4"
                    style={{ backgroundColor: 'transparent', border: 'none' }}
                  >
                    <div className="card-header settings-sub-block-header">
                      <strong>{t('newsletter_settings')}</strong>
                    </div>
                    <NewsletterSection />
                  </div>
                )}
                <div
                  id="leave-settings"
                  className="settings-sub-card mb-4"
                  style={{ backgroundColor: 'transparent', border: 'none' }}
                >
                  <div className="card-header settings-sub-block-header">
                    <strong>{t('leave_overleaf')}</strong>
                  </div>
                  <LeaveSection />
                </div>
              </>
            ) : (
              // [IV] CE: upstream only renders this section for saas (isOverleaf +
              // split test); the CE backend exists, so surface the entry point
              // here (mute-all + per-user delay for project-activity emails).
              <div
                id="notifications-settings"
                className="settings-sub-card mb-4"
                style={{ backgroundColor: 'transparent', border: 'none' }}
              >
                <NotificationsSection />
              </div>
            )}
          </div>
        </OLPageContentCard>
      </div>
    </UserProvider>
  )
}

export default SettingsPageRoot
