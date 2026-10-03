#!/usr/bin/env node
// Init-gate: verify the redis connection target is reachable and answers PING.
//
// REWRITTEN 2026-10-03 (deck / P7 step 4 + c10): the previous version imported
// app/src/infrastructure/RedisWrapper.mjs → @overleaf/redis-wrapper, a
// workspace lib DELETED by c10 (a440cf76dc) — unresolvable under PnP, which
// CRASH-LOOPED the container at my_init (caught live on deck cycle). This
// self-contained probe needs no npm client at all: raw TCP + one RESP PING
// (node:net is built in). Contract unchanged: exit 0 + "redis OK" on success,
// non-zero exit on failure (my_init runs set -e).
import net from 'node:net';

// REDIS_URL looks like redis://overleafredis:6379 (compose env), optionally
// redis://user:pass@host:port.
// Env chain: REDIS_URL → REDIS_HOST/OVERSEAF_REDIS_HOST (+port/db) → default host name.
const rawHost = process.env.REDIS_HOST || process.env.OVERSEAF_REDIS_HOST || 'overleafredis';
const rawPort = process.env.REDIS_PORT || '6379';
const rawDb = process.env.REDIS_DB || '0';
const raw = process.env.REDIS_URL || `redis://${rawHost}:${rawPort}/${rawDb}`;
const u = new URL(raw);
const host = u.hostname || 'overleafredis';
const port = Number(u.port) || 6379;
let pre = '';
if (u.password) {
  const user = u.username ? u.username : '';
  pre =
    `*2\r\n$4\r\nAUTH\r\n$${user.length}\r\n${user}\r\n` +
    `$${Buffer.byteLength(u.password)}\r\n${u.password}\r\n`;
}
console.log(`redis check: ${host}:${port}${u.password ? ' (auth)' : ''}`);

function finish(rc, msg) {
  clearTimeout(deadline);
  console.log(rc === 0 ? msg : `redis check FAILED: ${msg}`);
  sock.destroy();
  process.exit(rc);
}

const sock = new net.Socket();
const deadline = setTimeout(() => finish(1, 'timeout (connect+PING window 5s)'), 5000);

sock.once('error', (e) => finish(1, e.message));
sock.on('data', (d) => {
  const s = d.toString();
  if (s.includes('-ERR') || s.startsWith('-')) return finish(1, s.trim().slice(0, 120));
  if (s.includes('+PONG')) finish(0, 'redis OK (PONG)');
  // AUTH's +OK (or a chunked first frame) — keep waiting for PONG.
});

let sent = false;
sock.once('connect', () => {
  if (sent) return;
  sent = true;
  sock.write(`${pre}*1\r\n$4\r\nPING\r\n`);
});
sock.connect(port, host);
