#!/usr/bin/env bash

set -eu

echo "-----------------------------------------------"
echo "Dispatching scheduled notification emails"
echo "(emailNotifications -> send mail)"
echo "-----------------------------------------------"
date

source /etc/container_environment.sh
source /etc/overleaf/env.sh

# Go port of the node process_notifications.mjs cron: same claim protocol,
# byte-exact mail (oracle-pinned templates), same retry/dead-lettering.
cd /overleaf && /sbin/setuser www-data /usr/local/bin/go-services/cronmail

echo "Done."
