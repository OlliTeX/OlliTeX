/**
 * hub-string-id-parity — ag-72 PIN (2026-10-15 live incident: davrot@
 * /hub 500 while every full-bundle local user's hub rendered fine).
 *
 * ROOT CAUSE (Node-parity gap): SSO JIT users may store users.`_id` as a
 * 24-hex *string* (SaaS-legal; finishlogin.go ssoUserIDFilter documents
 * "string-hex _id docs exist (SSO-created users)" and the 2026-10-06 probe
 * verified driver v2 matches _id STRICTLY by BSON type). hub.loadUserDoc
 * filtered `_id` with an ObjectID only → lookup miss → silent 500 for
 * exactly the SSO-cohort shape (and only there — hub is the single site
 * without the ObjectID-first/string-fallback pattern the SSO paths use).
 * Node's mongoose findById is type-tolerant — both shapes must work.
 *
 * ISOLATION: users are seeded as clones of the known-good e2e-admin doc
 * (identical full bundle, admin's own bcrypt hash) differing ONLY in _id
 * type — string vs ObjectID. That isolates the incident variable.
 *
 * RED on ag-71 (500 on the string-_id user), GREEN on ag-72.
 */
import { test, expect } from '@playwright/test'
import { execSync } from 'node:child_process'
import * as fs from 'node:fs'
import * as path from 'node:path'
import * as os from 'node:os'
import { loginRobust } from '../helpers/auth'

const BASE = 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const SEED_PW = ADMIN.password // users are seeded with the admin's own hash fixture

// --- e2e mongo (world container) helpers --------------------------------
function mongoEval(code: string): string {
  const f = path.join(os.tmpdir(), `hubid_${Date.now()}_${Math.ceil(Math.random() * 1e6)}.js`)
  fs.writeFileSync(f, code)
  try {
    return execSync(`docker exec -i ol-e2e-mongo-1 mongosh --quiet sharelatex < ${f}`, {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    }).trim()
  } finally {
    try { fs.unlinkSync(f) } catch { /* best effort */ }
  }
}

// seed a full-bundle user (clone of the e2e-admin doc — bundle parity, known
// bcrypt hash) with _id of the requested shape; returns the 24-hex id.
// NOTE: mongosh (stdin pipe) echoes REPL prompts + per-statement values onto
// stdout — the sentinel is the LAST statement and parsed loosely (house
// pattern: admin-user-lifecycle pops the last line).
async function seedUser(email: string, idShape: 'oid' | 'string'): Promise<string> {
  const suffix = idShape === 'oid' ? 'aaaaaaaaaa01' : 'bbbbbbbbbb02'
  const hex = suffix + Date.now().toString(16).padEnd(12, '0') // 24 valid hex, unique per run
  const js = `
const admin = db.users.findOne({email: ${JSON.stringify(ADMIN.email)}});
if (!admin) { print('SEED-ERROR: admin fixture not found'); quit(1); }
const doc = JSON.parse(JSON.stringify(admin));
doc.email = ${JSON.stringify(email)};
doc.emails = [{ email: ${JSON.stringify(email)}, confirmed: true }];
doc._id = ${idShape === 'oid' ? `ObjectId('${hex}')` : `'${hex}'`};
db.users.deleteOne({email: ${JSON.stringify(email)}});
db.users.insertOne(doc);
const ok = db.users.findOne({email: ${JSON.stringify(email)}}) !== null;
print(ok ? 'SEEDED ' + ${JSON.stringify(idShape)} + ' ' + doc._id.toString() : 'SEED-FAIL ' + ${JSON.stringify(email)});
`
  const out = mongoEval(js)
  const m = out.match(/SEEDED (string|objectid) ([0-9a-f]{24})/)
  if (!m) throw new Error('seed failed; mongosh tail: ' + out.trim().slice(-500))
  return m[2]
}

function deleteUser(email: string): void {
  try { mongoEval(`db.users.deleteOne({email: ${JSON.stringify(email)}}); print('cleaned')`) } catch { /* best effort */ }
}

// --- the pin -------------------------------------------------------------
test.describe('hub renders for BOTH users._id shapes (Node mongoose parity)', () => {
  test('SSO-cohort shape: 24-hex STRING _id → /hub 200 + ol-user meta (ag-71: 500)', async ({ page }) => {
    const email = `hubid.${Date.now()}s@example.test`
    try {
      await seedUser(email, 'string')
      await loginRobust(page, email, SEED_PW)
      const hub = await page.request.get(BASE + '/hub')
      expect(hub.status(),
        'GET /hub with string _id must be 200 (ag-71 returns 500 — the davrot@ incident shape)')
        .toBe(200)
      const body = await hub.text()
      expect(body, 'ol-user meta carries the user email').toContain(email)
    } finally {
      deleteUser(email)
    }
  })

  test('control: ObjectID _id → /hub 200 (local-user shape, always worked)', async ({ page }) => {
    const email = `hubid.${Date.now()}o@example.test`
    try {
      await seedUser(email, 'oid')
      await loginRobust(page, email, SEED_PW)
      const hub = await page.request.get(BASE + '/hub')
      expect(hub.status(), 'GET /hub with ObjectID _id (control) must be 200').toBe(200)
      const body = await hub.text()
      expect(body, 'ol-user meta carries the user email').toContain(email)
    } finally {
      deleteUser(email)
    }
  })
})
