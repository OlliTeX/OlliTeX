# SSO & integrations (admin)

Goal: let people sign in with the institution's identity provider and
connect reference tools.

Home: **Hub → Site settings → Integrations** (`/hub#/site.integrations/…`).

## Single sign-on

| Provider | Leaf | Notes |
|---|---|---|
| SAML | `/hub#/site.integrations.sso-saml` | IdP entity id, SSO/ACS URLs, certificate |
| OIDC | `/hub#/site.integrations.sso-oidc` | discovery URL or issuer, client id/secret |
| LDAP | `/hub#/site.integrations.sso-ldap` | server, base DN, bind settings |

![The SSO · SAML configuration leaf](../assets/admins/08-sso-saml.png)

When a provider is enabled, the login page offers **Institutional login**
alongside e-mail/password; account claims (e-mail, first/last name) map to
the OlliTeX user automatically.

> Secrets (IdP private keys, OIDC client secrets, LDAP bind passwords) are
> stored encrypted server-side. Describe them in runbooks by **name and
> purpose only** — never paste the actual values.

## Reference & file integrations

| Leaf | Purpose |
|---|---|
| Zotero | `/hub#/site.integrations.zotero` — enable + connector settings ([users: references](../users/06-references.md)) |
| Mendeley | `/hub#/site.integrations.mendeley` — connector credentials |
| External URLs | `/hub#/site.integrations.externalurl` — allowed import URLs |

The compilation-side switches (GitHub sync, WebDAV, Dropbox, linked file
types) live under
**Compilation** — see [Site settings](04-site-settings.md) and
[users: sync & integrations](../users/08-sync-integrations.md).

***

Verified against: OlliTeX @ `f827b5ceb9` (2026-09-16)
