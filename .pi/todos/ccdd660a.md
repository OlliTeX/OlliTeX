{
  "id": "ccdd660a",
  "title": "002: hub site.general.stats not updated to grafana (OPEN)",
  "tags": [
    "live-audit",
    "psintern",
    "hub",
    "stats"
  ],
  "status": "done",
  "created_at": 1790773110384,
  "last_change": 1791506969718,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Live audit Issue 002 — hub site.general.stats empty — FIXED + live-verified (2026-09-30)
### Root cause (3 layers)
1. Collector route POST /internal/collect-instance-stats existed only in the RETIRED Node module (deleted in P7 step-4, c6101120f9; semantics recovered from git collectInstanceStats.mjs) — never ported to Go.
2. Container had NO cron daemon running (no crond process; no cron runit service) — crontab files baked but nothing ran (deletion/history crons also dead).
3. Cron script pointed at dead Node api profile 127.0.0.1:3000.
Result: instanceStats collection always empty → series [] → all hub stats cards 0 (the exact report).
### Fix
- go/services/web/features/instancestats/collector.go — POST /internal/collect-instance-stats (private-API basic auth, NoSession), Node-exact metrics (active projects/users 24h, new_users, shared_projects, counts, dbStats, du, redis INFO, df, /proc loadavg+meminfo), upsert {statKey,day,values,generatedAt} + 365d prune.
- core/redis.go — exported RawDo (INFO).
- server-ce/cron/collect-instance-stats.sh — URL 3000→4000.
- live ops: /etc/service/cron runit service + crontab installed; cron daemon now persistent.
### Evidence
- collector: {"day":"2026-09-30T00:00:00Z","ok":true}
- mongo: user_count 117, project_count 185, files 1647 (39 docs)
- series API 200 points:[{day:1790726400000,values:[117]}]
- hub leaf renders real series (Active projects latest 3; Files latest 1,647; Sep axes)
### REMAINING: bake the cron service + crontab into the image (server-ce Dockerfile/my_init) so rebuilds keep cron.
