# Admin overview

Goal: understand the admin surface and where each control lives.

Anyone with the site-administrator role sees the **Site settings** rail on
the hub (`/hub`). It is organized by concern:

| Section | Leaves | What it covers |
|---|---|---|
| Overview & activity | `/hub#/overview` | instance health, shortcuts to the important pages |
| General | users, projects, active projects, templates, sign-up, system messages, stats, editor controls | the day-to-day administration |
| Integrations | Zotero, Mendeley, external URLs, SSO (SAML/OIDC/LDAP) | identity + connectors |
| Services | Email/SMTP, services, branding, grammar (LT) | outbound e-mail + language tools |
| Compilation | sandbox, pandoc, git, GitHub sync, linked file types, WebDAV, Dropbox | what may compile / where files come from |
| LLM Settings | Rate Limiter, features, connection, models, prompts, usage | the AI surface (instance-wide) |

The old standalone admin pages still work through 301 redirects into this
rail (bookmarks and SSO deep links land here automatically).

## Day-to-day map

| I want to… | Go to |
|---|---|
| Create / suspend / purge a user | [Users](02-users.md) |
| Inspect / share / trash / purge a project | [Projects](03-projects.md) |
| Change site name, e-mail, sign-up | [Site settings](04-site-settings.md) |
| Turn AI features on/off, set rate limits | [Rate Limiter](05-llm-rate-limiter.md) |
| Manage the template gallery | [Templates](06-templates.md) |
| Watch instance health | [Instance statistics](07-instance-stats.md) |
| Configure SSO | [SSO & integrations](08-sso-saml-oidc.md) |
| Send a system message banner | [Operations](09-operations.md) |

![The hub in admin mode — the Site settings rail below the workspace](../assets/users/01-getting-started-hub-admin.png)

***

Verified against: OlliTeX @ `f827b5ceb9` (2026-09-16)
