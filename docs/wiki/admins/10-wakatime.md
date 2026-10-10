# WakaTime integration (admin)

Site-level WakaTime relay: one instance-local Wakapi service, per-user keys,
central switch. Users see it at /user-settings/mysettings.wakatime (see
[user → WakaTime](../users/11-wakatime.md)); admins configure it here.

## Page

`/admin-settings/site.integrations.wakatime`
(hub: **Site settings → Integrations → WakaTime**).

## What the admin controls

| Setting | Meaning | Notes |
| --- | --- | --- |
| **Enabled** | Master switch. Disabled → user page hidden, relay stops forwarding. | Toggling off does not delete stored Wakapi entries. |
| **Relay target (Wakapi base URL)** | The self-hosted Wakapi instance the relays forward to. | On the standard toolkit deployment this is the `wakapi` service (its Postgres on `pg`). Keep it instance-local; setting it to a public URL makes this a remote time-tracking sink — a deliberate decision, make it a deliberate one. |
| **Rate limit** | Per-user heartbeat ceiling (default **200/hour**). | Protects Wakapi from client misbehaviour; raise only with care. |
| **User access** | List of users with keys, key age, last activity. | Keys are personal credentials — **never copy them into tickets, wikis, or chat**; redact them in logs/screenshots. |

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
