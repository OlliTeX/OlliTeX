#!/bin/sh
# AG (owner 2026-10-09): relay FIRING Prometheus alerts to the OlliTeX
# instance-stats webhook (POST /internal/alerts) — the same mail pipeline
# the classic admin page's "send test" uses. This build of prometheus has
# no webhook notification route (its config schema trims webhook_configs),
# so the relay is the notification path: rules fire in Prometheus, this
# cron posts them, the web service mails the configured recipients.
#
# Dedup: an alert that stays firing for hours is NOT re-mailed every
# minute — the state file tracks (rule+labels) → last-sent time; a change
# in the firing set or a 4h staleness re-sends. A RESOLVED set clears the
# state (no re-mail on the next firing storm... the storm starts fresh).
set -u
PROM_URL="${PROMETHEUS_ALERTS_URL:-http://ollitex-prometheus:9090/api/v1/alerts}"
WEB_URL="${WEB_PRIVATE_URL:-http://127.0.0.1:4000}"
STATE_DIR=/var/lib/overleaf
mkdir -p "$STATE_DIR"
STATE="$STATE_DIR/alert-relay-state.json"

ALERTS_JSON=$(curl -fsS --max-time 15 "$PROM_URL" 2>/dev/null) || ALERTS_JSON='{"status":"success","data":{"alerts":[]}}'

python3 - "$ALERTS_JSON" "$WEB_URL" "$STATE" <<'PYEOF'
import json, sys, time, hashlib, os, urllib.request

alerts_json, web_url, state_path = sys.argv[1], sys.argv[2], sys.argv[3]
try:
    data = json.loads(alerts_json)
except Exception:
    sys.exit(0)  # prometheus unreachable — skip quietly (next minute retries)

alerts = (data.get("data") or {}).get("alerts") or []
# Prometheus /api/v1/alerts marks each entry with "state" (firing/pending) —
# NOT "status" (that field does not exist on this endpoint).
firing = [a for a in alerts if a.get("state") == "firing"]

# --- dedup state -------------------------------------------------------
state = {}
try:
    with open(state_path) as f:
        state = json.load(f)
except Exception:
    state = {}

now = time.time()
fingerprint = {}
for a in firing:
    labels = a.get("labels") or {}
    key = hashlib.md5(
        (labels.get("alertname", "") + "|" + json.dumps(labels, sort_keys=True)).encode("utf-8")
    ).hexdigest()[:16]
    fingerprint[key] = {
        "alertname": labels.get("alertname", ""),
        "labels": labels,
        "annotations": a.get("annotations") or {},
        "lastSent": state.get(key, {}).get("lastSent", 0),
    }

# which fingerprints need (re)sending? new ones, or > 4h since last send
due = []
for key, fn in fingerprint.items():
    if now - fn["lastSent"] > 4 * 3600:
        due.append((key, fn))

# --- send ---------------------------------------------------------------
sent = 0
if due:
    payload = {
        "version": "4",
        "status": "firing",
        "alerts": [
            {
                "status": "firing",
                "labels": fn["labels"],
                "annotations": fn["annotations"],
            }
            for _, fn in due
        ],
    }
    # X-Internal-Token: the instance-stats webhook guard (shared-secret
    # header; set in the web container env — same container as this cron).
    headers = {"Content-Type": "application/json"}
    token = os.environ.get("INTERNAL_ALERTS_TOKEN", "")
    if token:
        headers["X-Internal-Token"] = token
    req = urllib.request.Request(
        web_url.rstrip("/") + "/internal/alerts",
        data=json.dumps(payload).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            body = resp.read(4096)
            if resp.status != 200:
                # webhook answered non-200 (auth shape change / 5xx): don't
                # mark as sent — retry next minute.
                sys.stderr.write("alert-relay: webhook HTTP %s: %s\n" % (resp.status, body.decode("utf-8", "replace")))
            else:
                sent = len(due)
    except Exception as e:
        sys.stderr.write("alert-relay: webhook POST failed: %s\n" % e)
        sys.exit(0)

# --- persist state -------------------------------------------------------
if sent:
    for key, _ in due:
        fingerprint[key]["lastSent"] = now
new_state = {k: v for k, v in fingerprint.items()}
# drop fingerprints that are no longer firing (resolved)
for k in list(state.keys()):
    if k not in fingerprint:
        new_state.pop(k, None)
    else:
        new_state[k] = state[k] if k not in fingerprint else fingerprint[k]
tmp = state_path + ".tmp"
with open(tmp, "w") as f:
    json.dump(new_state, f)
os.replace(tmp, state_path)

if sent:
    sys.stderr.write("alert-relay: relayed %d firing alert(s)\n" % sent)
PYEOF
exit 0
