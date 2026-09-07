import { ElementType } from 'react'
import { useTranslation } from 'react-i18next'
import importOverleafModules from '../../../../macros/import-overleaf-module.macro'
import { useSSOContext, SSOSubscription } from '../context/sso-context'
import { SSOLinkingWidget } from './linking/sso-widget'
import getMeta from '../../../utils/meta'
import { useBroadcastUser } from '@/shared/hooks/user-channel/use-broadcast-user'
import Notification from '@/shared/components/notification'

const allAvailableIntegrationLinkingWidgets = importOverleafModules(
  'integrationLinkingWidgets'
) as any[]
const availableReferenceLinkingWidgets = importOverleafModules(
  'referenceLinkingWidgets'
) as any[]
const availableLangFeedbackLinkingWidgets = importOverleafModules(
  'langFeedbackLinkingWidgets'
) as any[]

const gitBridgeEnabled = getMeta('ol-gitBridgeEnabled')
const githubSyncEnabled = getMeta('ol-ExposedSettings').githubSyncEnabled
const zoteroEnabled = getMeta('ol-ExposedSettings').zoteroEnabled
const webdavEnabled = getMeta('ol-ExposedSettings').webdavEnabled
const dropboxEnabled = !!getMeta('ol-ExposedSettings').dropboxEnabled

// 2026-09 (owner report A): only render a link-provider widget when its
// backend is actually enabled — otherwise the widget fires
// /user/github-sync/status, /user/git-servers, /user/dropbox/status on
// mount and every page load logged 404/500 for the disabled features.
// The PAT-based “Git Provider” widget (git-bridge) needs no OAuth and stays
// always available.
const availableIntegrationLinkingWidgets = allAvailableIntegrationLinkingWidgets.filter(
  ({ path }) => {
    if (path.includes('webdav')) return webdavEnabled
    if (path.includes('dropbox')) return dropboxEnabled
    if (path.includes('github-sync')) return githubSyncEnabled
    return true
  }
)

/**
 * owner #6b (2026-09-07): the unified hub renders the two leaves
 * mysettings.sync and mysettings.references from this ONE section — `only`
 * restricts the rendered block(s) so each leaf shows its own content.
 * Default (no prop) = legacy /user/mysettings behaviour, unchanged.
 */
function LinkingSection({
  only,
}: {
  only?: 'projectSync' | 'references'
} = {}) {
  useBroadcastUser()
  const { t } = useTranslation()
  const { subscriptions } = useSSOContext()
  const ssoErrorMessage = getMeta('ol-ssoErrorMessage')
  const cannotUseAi = getMeta('ol-cannot-use-ai')
  const projectSyncSuccessMessage = getMeta('ol-projectSyncSuccessMessage')
  const projectSyncErrorMessage = getMeta('ol-projectSyncErrorMessage')
  const referenceLinkingErrorMessage = getMeta(
    'ol-referenceLinkingErrorMessage'
  )

  // hide linking widgets in CI
  const integrationLinkingWidgets = getMeta('ol-hideLinkingWidgets')
    ? []
    : availableIntegrationLinkingWidgets
  const referenceLinkingWidgets =
    getMeta('ol-hideLinkingWidgets') || !zoteroEnabled
      ? []
      : availableReferenceLinkingWidgets
  const langFeedbackLinkingWidgets = getMeta('ol-hideLinkingWidgets')
    ? []
    : availableLangFeedbackLinkingWidgets

  const oauth2ServerComponents =
    !gitBridgeEnabled ? [] : importOverleafModules('oauth2Server') as {
    import: { default: ElementType }
    path: string
  }[]

  const renderSyncSection =
    getMeta('ol-isSaas') || gitBridgeEnabled || githubSyncEnabled || webdavEnabled

  const allIntegrationLinkingWidgets = integrationLinkingWidgets.concat(
    oauth2ServerComponents
  )

  // since we only have Writefull here currently, we should hide the whole section if they cant use ai
  const haslangFeedbackLinkingWidgets =
    langFeedbackLinkingWidgets.length && !cannotUseAi
  const hasIntegrationLinkingSection =
    renderSyncSection && allIntegrationLinkingWidgets.length
  const hasReferencesLinkingSection = referenceLinkingWidgets.length

  // Filter out SSO providers that are not allowed to be linked by
  // managed users. Allow unlinking them if they are already linked.
  const hideGoogleSSO = getMeta('ol-cannot-link-google-sso')
  const hideOtherThirdPartySSO = getMeta('ol-cannot-link-other-third-party-sso')

  for (const providerId in subscriptions) {
    const isLinked = subscriptions[providerId].linked
    if (providerId === 'google') {
      if (hideGoogleSSO && !isLinked) {
        delete subscriptions[providerId]
      }
    } else {
      if (hideOtherThirdPartySSO && !isLinked) {
        delete subscriptions[providerId]
      }
    }
  }

  const hasSSOLinkingSection = Object.keys(subscriptions).length > 0

  // per-leaf visibility (owner #6b)
  const showAi = only !== 'references'
  const showSync = only !== 'references'
  const showRefs = only === undefined || only === 'references'
  const showSso = only === undefined

  if (
    !haslangFeedbackLinkingWidgets &&
    !hasIntegrationLinkingSection &&
    !hasReferencesLinkingSection &&
    !hasSSOLinkingSection
  ) {
    return null
  }
  if (only === 'references' && !hasReferencesLinkingSection) return null
  if (
    only === 'projectSync' &&
    !hasIntegrationLinkingSection &&
    !haslangFeedbackLinkingWidgets
  ) {
    return null
  }

  return (
    <>
      {showAi && haslangFeedbackLinkingWidgets ? (
        <>
          <h3 id="language-feedback">{t('ai_features')}</h3>
          {langFeedbackLinkingWidgets.map(
            ({ import: { default: widget }, path }, widgetIndex) => (
              <ModuleLinkingWidget
                key={path}
                ModuleComponent={widget}
                isLast={widgetIndex === langFeedbackLinkingWidgets.length - 1}
              />
            )
          )}
        </>
      ) : null}
      {showSync && hasIntegrationLinkingSection ? (
        <>
          <h3 id="project-sync">{t('project_synchronisation')}</h3>
          {projectSyncSuccessMessage ? (
            <div className="notification-list">
              <Notification
                type="success"
                content={projectSyncSuccessMessage}
              />
            </div>
          ) : null}
          {projectSyncErrorMessage ? (
            <Notification type="error" content={projectSyncErrorMessage} />
          ) : null}
          <div className="settings-widgets-container">
            {allIntegrationLinkingWidgets.map(
              ({ import: importObject }, widgetIndex) => (
                <ModuleLinkingWidget
                  key={Object.keys(importObject)[0]}
                  ModuleComponent={Object.values(importObject)[0]}
                  isLast={
                    widgetIndex === allIntegrationLinkingWidgets.length - 1
                  }
                />
              )
            )}
          </div>
        </>
      ) : null}
      {showRefs && hasReferencesLinkingSection ? (
        <>
          <h3 id="references">{t('reference_managers')}</h3>
          {referenceLinkingErrorMessage ? (
            <div className="notification-list">
              <Notification
                type="error"
                content={referenceLinkingErrorMessage}
              />
            </div>
          ) : null}
          <div className="settings-widgets-container">
            {referenceLinkingWidgets.map(
              ({ import: importObject }, widgetIndex) => (
                <ModuleLinkingWidget
                  key={Object.keys(importObject)[0]}
                  ModuleComponent={Object.values(importObject)[0]}
                  isLast={widgetIndex === referenceLinkingWidgets.length - 1}
                />
              )
            )}
          </div>
        </>
      ) : null}
      {showSso && hasSSOLinkingSection ? (
        <>
          <h3 id="linked-accounts">{t('linked_accounts')}</h3>
          {ssoErrorMessage ? (
            <div className="notification-list">
              <Notification
                type="error"
                content={`${t('sso_link_error')}: ${ssoErrorMessage}`}
              />
            </div>
          ) : null}
          <div className="settings-widgets-container">
            {Object.values(subscriptions).map(
              (subscription, subscriptionIndex) => (
                <SSOLinkingWidgetContainer
                  key={subscription.providerId}
                  subscription={subscription}
                  isLast={
                    subscriptionIndex === Object.keys(subscriptions).length - 1
                  }
                />
              )
            )}
          </div>
        </>
      ) : null}
      {haslangFeedbackLinkingWidgets ||
      hasIntegrationLinkingSection ||
      hasReferencesLinkingSection ||
      hasSSOLinkingSection ? (
        <hr />
      ) : null}
    </>
  )
}

type LinkingWidgetProps = {
  ModuleComponent: any
  isLast: boolean
}

function ModuleLinkingWidget({ ModuleComponent, isLast }: LinkingWidgetProps) {
  return (
    <>
      <ModuleComponent />
      {isLast ? null : <hr />}
    </>
  )
}

type SSOLinkingWidgetContainerProps = {
  subscription: SSOSubscription
  isLast: boolean
}

function SSOLinkingWidgetContainer({
  subscription,
  isLast,
}: SSOLinkingWidgetContainerProps) {
  const { t } = useTranslation()
  const { unlink } = useSSOContext()

  let description = ''
  switch (subscription.providerId) {
    case 'collabratec':
      description = t('linked_collabratec_description')
      break
    case 'google':
      description = `${t('login_with_service', {
        service: subscription.provider.name,
      })}.`
      break
    case 'orcid':
      description = t('oauth_orcid_description')
      break
  }
  if (!description) {
    description =
      subscription.provider.description ||
      `${t('login_with_service', {
        service: subscription.provider.name,
      })}.`
  }

  return (
    <>
      <SSOLinkingWidget
        providerId={subscription.providerId}
        title={subscription.provider.name}
        description={description}
        helpPath={subscription.provider.descriptionOptions?.link}
        linked={subscription.linked}
        linkPath={subscription.provider.linkPath}
        onUnlink={() => unlink(subscription.providerId)}
      />
      {isLast ? null : <hr />}
    </>
  )
}

export default LinkingSection
