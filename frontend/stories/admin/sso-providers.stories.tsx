import React from 'react'
import { Meta, StoryObj } from '@storybook/react'
import useFetchMock from '../hooks/use-fetch-mock'
import { SsoProvidersSection } from '../../modules/ollitex-hub/frontend/js/sections/admin/site/sso-providers-section'

// -- Wave D (settings section bodies): the multi-provider SSO admin section
//    (owner TODO-4f5bdfa4; reference sso@fe4ceb6). The section talks to the
//    /admin/sso/config surface — the stories stub that endpoint (GET) and
//    pin the rendered contract for both the EMPTY state (env-mode only) and
//    the POPULATED state (providers + LDAP, masked secrets).

const emptyConfig = { ldap: null, providers: [], spMetadata: null }

const populatedConfig = {
  ldap: {
    enabled: true,
    identityServiceName: 'Corporate directory',
    url: 'ldap://ldap.example.edu:389',
    searchBase: 'ou=people,dc=example,dc=edu',
    bindDN: 'cn=svc,dc=example,dc=edu',
    bindCredentials: '••••••••',
    searchFilter: '(uid={0})',
    searchScope: 'sub',
    emailAtt: 'mail',
    firstNameAtt: 'givenName',
    lastNameAtt: 'sn',
    isAdminAtt: 'ou',
    valAdmin: 'admins',
    timeout: '5000',
  },
  providers: [
    {
      id: 'oidc-alpha',
      type: 'oidc',
      name: 'Primary IdC',
      buttonLabel: 'Log in with University SSO',
      enabled: true,
      order: 1,
      issuer: 'https://sso.example.edu/realms/uni',
      clientID: 'ollitex',
      clientSecret: '••••••••',
      scope: 'openid profile email',
      allowedEmailDomains: 'example.edu',
    },
    {
      id: 'saml-fed',
      type: 'saml',
      name: 'Federated SAML',
      buttonLabel: 'Log in with Shibboleth',
      enabled: false,
      order: 2,
      issuer: 'https://sp.example.edu',
      entryPoint: 'https://idp.example.edu/sso/saml',
      audience: 'https://sp.example.edu',
      idpCert: '••••••••',
      privateKey: '••••••••',
      wantAssertionsSigned: true,
      attEmail: 'email',
      attAdmin: 'groups',
      valAdmin: 'admins',
    },
  ],
  spMetadata: null,
}

type Story = StoryObj & { parameters?: Record<string, unknown> }

const base: Meta = {
  title: 'Admin / Settings / SSO Providers (multi)',
  component: SsoProvidersSection as unknown as React.ComponentType,
  tags: ['autodocs'],
  parameters: {
    docs: {
      description: {
        component:
          'The admin multi-provider SSO manager (provider list + per-provider ' +
          'enable/endpoints/keys + add/remove/reorder + LDAP searchAttributes + ' +
          'local-login safety valve note). API surface: /admin/sso/* (config ' +
          'GET/POST, provider add/delete/reorder, test provider/ldap).',
      },
    },
  },
}

export default base

export const EmptyState: Story = () => {
  useFetchMock(m => m.get('/admin/sso/config', emptyConfig))
  return <SsoProvidersSection />
}
EmptyState.parameters = { docs: { description: { story: 'No database providers yet — the env-mode OIDC button is the only login (note text explains).' } } }

export const PopulatedState: Story = () => {
  useFetchMock(m => m.get('/admin/sso/config', populatedConfig))
  return <SsoProvidersSection />
}
PopulatedState.parameters = { docs: { description: { story: 'One enabled OIDC + one disabled SAML provider (masked secrets show "leave empty to keep") + a configured LDAP block with all searchAttributes.' } } }
