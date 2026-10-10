# WakaTime (users)

Track your editing time across OlliTeX projects without any third-party
browser extension or plugin. OlliTeX relays WakaTime heartbeats to a
**self-hosted [Wakapi](https://wakapi.dev/) instance that you (the admin)
run** — no data leaves your infrastructure, and the official WakaTime cloud
is never contacted by the instance.

> Status: admin must enable WakaTime in
> [`/admin-settings/site.integrations.wakatime`](#) (see
> [admin → WakaTime](../admins/10-wakatime.md)) before the user page below is
> available. If the feature is disabled, your settings sidebar simply has no
> WakaTime leaf.

## Where to find it

`/user-settings/mysettings.wakatime` (in the app: **My settings → WakaTime**,
or the keyboard-accessible leaf in the settings sidebar).

## What the page does

| Control | What it does |
| --- | --- |
| **Enable WakaTime** | Starts the local heartbeat relay for your account (per-keystroke activity, batched). |
| **Your API key** | A personal key generated when you enable. This key works against the *self-hosted Wakapi* endpoint, not against wakatime.com. **Do not paste anyone else's cloud key here and do not share yours** — it is credential material. Display the key on a screen only when you must copy it; treat it like a password. |
| **Regenerate / revoke** | Rotates your personal key (old key stops working immediately). |
| **Status / recent activity** | Whether heartbeats are being accepted by the relay right now, and the most recent relayed entries (file, language, line) — so you can verify attribution before trusting it. |

## How the relay works (what you are signing up for)

1. While you edit, the editor emits activity (file path, language, cursor
   line) — the same shape Wakapi accepts.
2. OlliTex (your instance) batches these and forwards them to the local
   Wakapi service over the instance network. **Your browser never talks to
   Wakapi or to wakatime.com directly.**
3. Wakapi stores the entries locally (Postgres on the instance) and exposes
   the standard WakaTime-compatible API if you want to query it.

Because the relay is per-user and rate-limited (`200/1h` per the site
default; the admin can change it), accidental bursts cannot flood the
Wakapi service.

## Privacy note

- Files you edit on **this instance** are attributed to **your account on
  the local Wakapi**. There is no external transmission.
- If the admin later migrates the Wakapi data source, the same rule applies
  to the new target — check with your admin what the relay's target URL is
  (it is visible on the admin WakaTime page, not on yours).

## Troubleshooting

- **Key rejected (401):** regenerate the key on this page and copy the
  *new* key.
- **No activity appearing:** confirm you are logged in, that you are editing
  an open document (heartbeats start after the first edit of a session), and
  that the admin has WakaTime enabled.
- **Wrong file attributed:** heartbeats attribute to the file focused at
  emit time — switch editor panes and wait a heartbeat interval before
  judging.

## Verified against

OlliTeX v26 surface (2026-10-11). User page leaf
`mysettings.wakatime`; verified relay behaviour: enable `PUT /user/wakatime`
(200), heartbeat (HTTP 202), bulk flush (HTTP 204), Wakapi accept (201)
against the instance's local Wakapi through its
`/api/compat/wakatime/v1/*` compat endpoint.
