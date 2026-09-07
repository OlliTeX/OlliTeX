// /hub → Site settings: native Mantine section registry (owner #32).
// leaf siteId → native section component. Absent ids fall back to the
// legacy AdminSiteSection fixedSection embed (misc/signup/zotero/...).

import type { ComponentType } from 'react'
import { SsoLdapSection, SsoOidcSection, SsoSamlSection } from './sso-sections'
import { EmailSection } from './email-section'
import {
  BrandingSection,
  DropboxSection,
  MendeleySection,
  GrammarSection,
  GitSection,
  GithubSection,
  LinkedFileTypesSection,
  PandocSection,
  ServicesSection,
  WebdavSection,
} from './simple-sections'
import { SandboxedSection } from './sandboxed-section'

export const NATIVE_SITE_SECTIONS: Record<string, ComponentType> = {
  'sso-saml': SsoSamlSection,
  'sso-oidc': SsoOidcSection,
  'sso-ldap': SsoLdapSection,
  email: EmailSection,
  branding: BrandingSection,
  services: ServicesSection,
  // leaf siteIds are the site-settings section ids (nav-tree.ts)
  languagetool: GrammarSection,
  'sandboxed-compiles': SandboxedSection,
  pandoc: PandocSection,
  'git-integration': GitSection,
  'github-sync': GithubSection,
  'linked-file-types': LinkedFileTypesSection,
  webdav: WebdavSection,
  dropbox: DropboxSection,
  mendeley: MendeleySection,
}
