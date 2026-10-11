# WakaTime (users)

Track your editing time across OlliTeX projects without any third-party
browser extension or plugin. OlliTeX relays WakaTime heartbeats to a
**self-hosted [Wakapi](https://wakapi.dev/) instance that you (the admin)
run** — no data leaves your infrastructure, and the official WakaTime cloud
is never contacted by the instance.

> Status: the admin enables WakaTime in
> [`/admin-settings/site.integrations.wakatime`](#) (see
> [admin → WakaTime](../admins/10-wakatime.md)) — the feature toggle also
> exposes the user opt-in below. Before that, your settings sidebar simply
> has no WakaTime leaf.

## Where to find it

`/user-settings/mysettings.wakatime` (in the app: **My settings → WakaTime**,
or the keyboard-accessible leaf in the settings sidebar).

## What the page does

| Control | What it does |
| --- | --- |
| **Opt-in / enable** | Starts the local heartbeat relay for your account (per-keystroke activity, batched server-side). |
| **Status** | Whether your wakapi account is active and heartbeats are being accepted by the relay right now — so you can verify attribution before trusting it. |
| **Your dashboard** | The local Wakapi UI at `/wakapi` on this host (e.g. `https://<your-instance>/wakapi`) — the admin page's footer shows the same link. Your wakapi username is the **local part of your OlliTeX e-mail** (e.g. `jdoe` for `jdoe@example.org`). |

**Key handling (important):** your wakapi API key is **created and stored
server-side** by OlliTeX (auto-provisioned from your e-mail; the admin can
regenerate it for you). You never have to type or keep a key — and the
browser never talks to wakapi at all, so the key never leaves the server.
If a key ever does surface (admin tooling, logs), treat it like a password:
redact it in tickets and screenshots. 

## How the relay works (what you are signing up for)

1. While you edit, the editor emits activity (file path, language, cursor
   line) — the same shape Wakapi accepts.
2. OlliTex (your instance) batches these and forwards them to the local
   Wakapi service over the instance network **using the server-side key**
   (the key never appears in the browser).
3. Wakapi stores the entries locally (its database on the instance) and
   exposes the standard WakaTime-compatible API if you want to query it —
   your dashboard is at `/wakapi`.

Because the relay is per-user and rate-limited (`200/1h` — the default
Wakapi-side ceiling), accidental bursts cannot flood the Wakapi service.

## Privacy note

- Files you edit on **this instance** are attributed to **your account on
  the local Wakapi**. There is no external transmission by default; if the
  admin points the relay at an off-box wakapi, the same rule applies to the
  new target — check with your admin.
- Your wakapi username is derived from your OlliTeX e-mail (local part), so
  a wakapi account name reveals the e-mail *domain* to anyone who can see
  wakapi user lists — instance-local by default, worth knowing before you
  enable it against a public relay target.

## Troubleshooting

- **No activity after enabling:** ask the admin to `Provision wakapi user`
  for your e-mail (admin → WakaTime) — the key is server-side; you opt in
  on THIS page, and heartbeats start after your next edit.
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
