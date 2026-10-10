# Federation & SSO (admin)

Two related surfaces, one goal: users signing in with identities that are
not created on *your* instance.

## 1. SSO (identity providers for your users)

Configured under **Site settings → Integrations → SSO**:

- **SSO · Providers (multi)** — the current multi-provider manager
  (OIDC/SAML/LDAP entries, add/remove/enable per provider).
- **SSO · SAML / OIDC / LDAP (legacy)** — the classic single-provider pages,
  kept for parity with the legacy surface.

The login page renders one slot per enabled provider (the login slot engine
decides which sign-in options appear; provider metadata is validated on
save, and secrets are stored encrypted — the `"••••••••"` sentinel means
"keep the stored value", an empty value erases it).

See [08-sso-saml-oidc.md](08-sso-saml-oidc.md) for the per-protocol setup
details.

## 2. Federation (sharing between OlliTeX instances)

Federation lets two OlliTeX instances recognise each other's users via
**OIDC** and share projects through the standard export/import flow:

- Your instance acts as an **OpenID Provider (OP)** for partner instances,
  and as a **Relying Party (RP)** when importing identities from a partner.
- **Project export/import** is the S2S transport: an exported project bundle
  carries an identity claim; the importing instance maps it into its own
  project model (the `federation` feature module + `export_wizard` UI).
- **Security invariants (all enforced + live-verified):**
  - RP `redirect_uri` is matched against the **registered client's** URI set
    — arbitrary redirect targets are rejected.
  - **PKCE** is required on the RP authorization flow.
  - Login/logout redirects are validated on the server (only same-origin,
    root-relative targets survive) — open-redirect is not possible.
  - Consent screens are per-partner, per-scope; revoking a partner removes
    the mapping.

## Operational notes

- Federation is admin-only surface; end users only ever see the login slot +
  the export/import UI.
- Keep partner client registrations minimal: exact URIs, no wildcards.
- The `/internal/alerts` webhook (monitoring) is unrelated to federation;
  it is token-gated (shared secret header) and must never be exposed to the
  browser.

## Leak guard (hard rule)

- **No client secrets, no client keys, no partner credentials in docs or
  screenshots.** Document the *shape* of a registration (issuer, client id,
  redirect URIs) with placeholders (`client-id: my-client`), never the
  secret.
- Screenshots of the SSO providers page: redact secret fields (they render
  as dots in the UI by design — capture that state, not the revealed state).

## Verified against

OlliTeX v26 surface (2026-10-11): SSO multi-provider manager + legacy pages;
federation RP/OP flows live E2E (consent, exchange, PKCE, redirect_uri
pinned to registered URIs, s2s export/import); login/logout redirect
validation; CSRF on all mutating user routes.
