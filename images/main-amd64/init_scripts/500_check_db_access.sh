#!/bin/sh
set -e

echo "Checking can connect to mongo and redis"
# 2026-10-05 (owner directive: no junk/ in the image): the old node check
# scripts (modules/server-ce-scripts) lived inside the retired
# junk/services-web tree. Replaced with dependency-free probes — pure node
# "net", so they work under PnP without any package resolution.
# (connectivity fail-fast is the purpose; deep version/admin checks were a
#  warning-level extra of the old script.)
MONGO_HOST=${MONGO_HOST:-mongo} MONGO_PORT=${MONGO_PORT:-27017} \
REDIS_HOST=${REDIS_HOST:-redis} REDIS_PORT=${REDIS_PORT:-6379} \
/sbin/setuser www-data node -e '
const net = require("node:net");
function probe (host, port, name, cmd, expectRe) {
  return new Promise((resolve, reject) => {
    const s = net.connect({ host, port });
    const fail = e => { clearTimeout(t); try { s.destroy(); } catch (_) {} reject(e); };
    const t = setTimeout(() => fail(new Error(name + " timeout")), 10000);
    s.once("connect", () => {
      if (!cmd) { clearTimeout(t); s.destroy(); return resolve(); }
      s.once("data", d => {
        clearTimeout(t); try { s.destroy(); } catch (_) {}
        const txt = d.toString();
        expectRe.test(txt)
          ? resolve()
          : fail(new Error(name + " bad response: " + txt.trim()));
      });
      s.once("error", fail);
      s.write(cmd);
    });
    s.once("error", fail);
  });
}
(async () => {
  const mh = process.env.MONGO_HOST || "mongo", mp = +(process.env.MONGO_PORT || 27017);
  const rh = process.env.REDIS_HOST || "redis", rp = +(process.env.REDIS_PORT || 6379);
  try {
    await probe(mh, mp, "mongo", null, null);
    console.log("Mongo reachable at " + mh + ":" + mp);
  } catch (e) {
    console.error("Cannot connect to mongo at " + mh + ":" + mp + " — " + e.message);
    process.exit(1);
  }
  try {
    await probe(rh, rp, "redis", "PING\r\n", /PONG/i);
    console.log("Redis is up at " + rh + ":" + rp);
  } catch (e) {
    console.error("Cannot connect to redis at " + rh + ":" + rp + " — " + e.message);
    process.exit(1);
  }
  console.log("All checks passed");
})().catch(e => { console.error(e.message); process.exit(1); });
'
