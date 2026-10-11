# WakaTime integration (admin)

Site-level WakaTime relay: one instance-local Wakapi service, per-user keys,
central switch. Users see it at /user-settings/mysettings.wakatime (see
[user → WakaTime](../users/11-wakatime.md)); admins configure it here.

## Page

`/admin-settings/site.integrations.wakatime`
(hub: **Site settings → Integrations → WakaTime**).

## What the admin controls

| Control | Meaning | Notes |
| --- | --- | --- |
| **Enabled (toggle)** | Master switch. Disabled → the user WakaTime leaf is hidden and relays stop. | Toggling off does not delete stored Wakapi entries. The toggle also exposes the user opt-in. |
| **Wakapi server URL** | The self-hosted Wakapi the relays forward to. Default: **this instance's own `/wakapi` route**. | Keep it instance-local by default; an off-box URL makes this a remote time-tracking sink — a deliberate decision, make it a deliberate one. |
| **Check connection** | Probes the Wakapi endpoint server-side and reports ok/message. | Use after any URL change, before onboarding users. |
| **Provision wakapi user (e-mail)** | Creates the wakapi account for that OlliTeX user (username = the e-mail's local part) and stores the API key server-side for them. | Use to onboard before the first heartbeat or to **regenerate** a user's key. The key never reaches the browser — redact it in any log/screenshot where it does surface. |

> Rates: heartbeats are per-user and the default Wakapi-side ceiling is
> **200/hour** (the relay does not queue indefinitely — bursts beyond the
> ceiling are dropped).

## Architecture on the standard toolkit

```
browser ──> OlliTeX web (relay) ──> Wakapi (local) ──> wakapi Postgres
        PUT /user/wakatime …      /api/compat/wakatime/v1/*
```

- The browser never talks to wakatime.com or directly to Wakapi.
- Every relay call is per-user (session-bound + CSRF-checked) and
  per-user rate-limited; the shared site secret is never exposed to the
  browser and is stored encrypted at rest by the settings service.
- Wakapi runs as its own container (`ollitex-wakapi` on the dev instance)
  with its own Postgres volume.

## Operational notes

- **Health:** check the Wakapi container (`docker ps` — `ollitex-wakapi`
  HEALTHY) and its Postgres volume; heartbeats failing to reach Wakapi are
  dropped at the relay, not queued indefinitely.
- **Backups:** include the Wakapi Postgres volume in your backups, exactly
  like the main Mongo — it is user-generated activity data.
- **Migrations:** changing the relay target mid-history does not replay old
  entries; point the new instance at its own data source.

## Leak guard (hard rule)

- This page, the user page, and every screenshot of either: **no API key
  values, no user e-mails, no Wakapi admin passwords.**
- Use placeholder identities in docs/screenshots (`user@example.org`).
- The provisioned local Wakapi admin password is a credential: it lives in
  the instance secret store / env, never in the README, wiki, or tickets.

## Verified against

OlliTeX v26 surface (2026-10-11): admin page section
`site.integrations.wakatime`; relay E2E proven (enable 200, heartbeat 202,
bulk 204, Wakapi accept 201); rate default 200/h; Wakapi + Postgres
containers on the standard toolkit.
