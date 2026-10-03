#!/usr/bin/env node
// Init-gate: verify the mongo connection target is reachable + pings.
//
// REWRITTEN 2026-10-03 (deck / P7 step 4 + c10): the previous version imported
// ../../../app/src/infrastructure/mongodb.mjs, whose import graph reaches
// @overleaf/mongoose-wrapper — a workspace lib DELETED by c10 (a440cf76dc),
// so the script could no longer resolve under PnP and CRASH-LOOPED the
// container at my_init (caught live on deck cycle, rolled back). The init
// gate needs ONLY a raw connection probe — this version is self-contained
// (mongodb-legacy is declared in this workspace's package.json, no app tree).
import mongodb from 'mongodb-legacy';

const url = process.env.MONGO_URL || 'mongodb://overleafmongo:27017';
console.log(`mongo check: ${url.replace(/\/\/[^\s@/]+@/, '//***@')}`);

const client = await mongodb.MongoClient.connect(url, { serverSelectionTimeoutMS: 5000 });

try {
  const res = await client.db('sharelatex').command({ ping: 1 });
  if (!res || res.ok !== 1) throw new Error(`ping not ok: ${JSON.stringify(res)}`);
  console.log('mongo OK (ping=1)');
} finally {
  try { await client.close(); } catch { /* already closed */ }
}
