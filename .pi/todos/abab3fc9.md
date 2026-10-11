{
  "id": "abab3fc9",
  "title": "[IV] Port notification2 → 6.3.0  [commits f8c645c3..head]",
  "tags": [
    "port",
    "notification2",
    "order-4",
    "depends-III"
  ],
  "status": "done",
  "created_at": 1788360827618,
  "last_change": 1788393400916,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Scope
Source: /data_1/image_mining/the_diff/notification2, range f8c645c378 .. e12641da7b (HEAD), 15 commits, ~31 files.

## Content
- notifications module: services/web/modules/notifications/app/src (8), test/unit (5)
- document-updater app/js (3) — emailNotifications TTL index etc.
- web frontend settings components (2): muteAllNotifications in project prefs GET
- server-ce/cron (2): fix cron runners — source /etc/container_environment.sh (REDIS_HOST, CRYPTO_RANDOM, OVERLEAF_MONGO_URL); run upstream enqueue .mts directly (node >= 22.18 native TS type stripping)

## How
Small branch — straightforward diff-apply onto the_diff/overleaf; check node version in image supports .mts cron path (6.3.0 base uses node 24 per buildscript) ; rebuild GREEN.

## Deps
Needs [I]+[II]+[III].
