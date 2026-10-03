#!/bin/sh
set -e

echo "Checking can connect to mongo and redis"
# P7 step 4 (2026-10-03): Node web retired to junk/services-web (RETIRED.md)
cd /overleaf/junk/services-web
/sbin/setuser www-data node modules/server-ce-scripts/scripts/check-mongodb.mjs
/sbin/setuser www-data node modules/server-ce-scripts/scripts/check-redis.mjs
echo "All checks passed"
