type TemplateLink = {
  name: string
  url: string
  trackingKey: string
}

export type ExposedSettings = {
  adminEmail: string
  appName: string
  cookieDomain: string
  dropboxAppName: string
  emailConfirmationDisabled: boolean
  gaToken?: string
  gaTokenV4?: string
  hasAffiliationsFeature: boolean
  hasLinkUrlFeature: boolean
  hasLinkedProjectFileFeature: boolean
  hasLinkedProjectOutputFileFeature: boolean
  hasSamlBeta?: boolean
  hasSamlFeature: boolean
  hotjarId?: string
  hotjarVersion?: string
  ieeeBrandId: number
  isOverleaf: boolean
  env: 'saas' | 'server-pro' | 'server-ce'
  maxEntitiesPerProject: number
  mixpanelLabsToken?: string
  projectUploadTimeout: number
  propensityId?: string
  maxUploadSize: number
  recaptchaDisabled: {
    invite: boolean
    login: boolean
    passwordReset: boolean
    register: boolean
    addEmail: boolean
  }
  recaptchaSiteKeyV3?: string
  recaptchaSiteKey?: string
  recaptchaEnterpriseSiteKey?: string
  samlInitPath?: string
  sentryAllowedOriginRegex: string
  sentryDsn?: string
  sentryEnvironment?: string
  sentryRelease?: string
  siteUrl: string
  textExtensions: string[]
  editableFilenames: string[]
  validRootDocExtensions: string[]
  fileIgnorePattern: string
  templateLinks?: TemplateLink[]
  labsEnabled: boolean
  wikiEnabled?: boolean
  templatesEnabled?: boolean
  linkedInInsightsPartnerId?: string
  githubSyncEnabled: boolean
  webdavEnabled?: boolean
  dropboxEnabled?: boolean
  zoteroEnabled: boolean
  enablePandocConversions: boolean
  llmAllowUserSettings?: boolean
  llmEnabled?: boolean
  // overleaf-lab (grammar port): site-level grammar-checking availability
  // (ExpressLocals computes these per request from the LLM admin settings
  // file + env; see modules/languagetool).
  llmAdminEnabled?: boolean
  llmServerConfigured?: boolean
  llmAvailableForUser?: boolean
  languageToolAvailable?: boolean
  grammarSettings?: {
    llmAdminEnabled: boolean
    llmServerConfigured: boolean
    llmAvailableForUser: boolean
    ltAvailable: boolean
  }
}
