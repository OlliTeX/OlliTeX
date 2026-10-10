#!/bin/sh
# AG (owner 2026-10-09): render the scrape config + (when present) the alert
# rules into /tmp and boot prometheus. The rules are wired via the
# config-file `rule_files:` key (this prom/prometheus build lacks the
# --rule.files CLI flag). Pre-AG data dirs (no rules file) boot with the
# rules section simply absent.
set -e
sed -e 's/__OVERLEAF_HOST__/${OVERLEAF_HOST:-overleafserver}/g' -e 's/__GITBRIDGE_HOST__/${GITBRIDGE_HOST:-git-bridge}/g' /etc/prometheus/prometheus.yml > /tmp/prometheus-live.yml

if [ -f /etc/prometheus/prometheus-rules.yml ]; then
  sed -e 's/__OVERLEAF_HOST__/${OVERLEAF_HOST:-overleafserver}/g' -e 's/__GITBRIDGE_HOST__/${GITBRIDGE_HOST:-git-bridge}/g' /etc/prometheus/prometheus-rules.yml > /tmp/prometheus-live-rules.yml
  # inject rule_files into the rendered config (top-level key, before the
  # first top-level `scrape_configs:`) — plain sed insert, no YAML tools
  # inside the container.
  if ! grep -q '^rule_files:' /tmp/prometheus-live.yml; then
    sed -i 's/^scrape_configs:/rule_files: ["\/tmp\/prometheus-live-rules.yml"]\nscrape_configs:/' /tmp/prometheus-live.yml
  fi
fi

exec /bin/prometheus --config.file=/tmp/prometheus-live.yml --storage.tsdb.path=/prometheus --storage.tsdb.retention.time=15d --web.enable-lifecycle
