/**
 * Host-side helpers: run commands INSIDE the test stack (docker exec) —
 * the stack's mongo has no auth (disposable), so fixture management goes
 * through mongosh in the container. No host docker socket needed beyond
 * `docker exec`.
 */
import { execFileSync } from 'child_process'

function dockerExec(imageOrName: string, args: string[]): string {
  const out = execFileSync('docker', ['exec', '--', imageOrName, ...args], {
    encoding: 'utf8',
    timeout: 120_000,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  return out
}

/**
 * List running container names (disposable-stack discovery, shared).
 */
export function runningContainers(): string[] {
  return execFileSync('docker', ['ps', '--format', '{{.Names}}'], { encoding: 'utf8' })
    .split('\n')
    .filter(Boolean)
}

function mongoEvalScript(script: string): string {
  // the stack's mongo container is named ol-e2e-mongo-1 (ol-e2e project)
  const names = execFileSync('docker', ['ps', '--format', '{{.Names}}'], {
    encoding: 'utf8',
  })
    .split('\n')
    .filter(Boolean)
  const mongo = names.find(n => /-e2e-mongo-1$/.test(n))
  if (!mongo) {
    throw new Error('mongo container not found (expected name matching *-e2e-mongo-1)')
  }
  return dockerExec(mongo, ['mongosh', '--quiet', 'sharelatex', '--eval', script])
}

/**
 * Run an ad-hoc mongosh expression against the test mongo and return its
 * stdout (used by specs that assert PERSISTENCE at the source — e.g. the
 * pandoc site_settings round-trip). Not a substitute for API assertions:
 * use it only where the server contract is "stored state".
 * Pass a single-line expression (mongosh --eval mode).
 */
export function mongoEval(expression: string): string {
  return mongoEvalScript(expression)
}

export function userExists(email: string): boolean {
  const out = mongoEval(
    `const u = db.getSiblingDB("sharelatex").users.findOne({email: ${JSON.stringify(
      email
    )}}); print(u ? "yes" : "no");`
  )
  return /yes/.test(out)
}

export function promoteAdmin(email: string): void {
  // CE grants the admin gate via `isAdmin` (a plain boolean on the user
  // document); `permissions` is the legacy legacy-style list — set BOTH.
  mongoEval(
    `db.getSiblingDB("sharelatex").users.updateOne(
       { email: ${JSON.stringify(email)} },
       { $set: { isAdmin: true, permissions: ['admin'] } }
     );`
  )
}

export function deleteTestUser(email: string): boolean {
  try {
    mongoEval(
      `db.getSiblingDB("sharelatex").users.deleteMany({email: ${JSON.stringify(email)}});`
    )
    return true
  } catch {
    return false
  }
}

/**
 * Latest one-time 'password' token minted for the account (registration
 * mints one; no mail is dispatched because the test stack disables email).
 */
export function latestPasswordToken(email: string): string | null {
  const out = mongoEval(
    [
      `const sl = db.getSiblingDB("sharelatex");`,
      `const doc = sl.tokens.find({ "data.email": ${JSON.stringify(email)} })
         .sort({ _id: -1 }).limit(1).toArray()[0];`,
      `print(doc ? doc.token : "");`,
    ].join('\n')
  )
  const m = out.match(/[a-f0-9]{64}/)
  return m ? m[0] : null
}

/**
 * True when a project with the exact `name` exists (the projects collection
 * stores the display name in `name`, NOT `projectName` — the JSON API maps
 * it; mongo checks must use `name`).
 */
export function projectExists(name: string): boolean {
  const script = [
    `const n = ${JSON.stringify(name)};`,
    `print(db.getSiblingDB("sharelatex").projects.findOne({ name: n }) ? "yes" : "no");`,
  ].join('\n')
  return /yes/.test(mongoEval(script))
}

/** Project _id for a title (null when absent) — used by global-setup to
 *  seed interaction-dependent fixtures (e.g. the contacts list). */
export function projectIdOf(name: string): string | null {
  const id = mongoEval(
    `const p = db.getSiblingDB("sharelatex").projects.findOne({ name: ${JSON.stringify(name)} });`
    + ` print(p ? String(p._id) : "");`
  ).trim().split('\n').pop() || ''
  return /^[0-9a-f]{24}$/.test(id) ? id : null
}

/** Seed a bidirectional CONTACT pair between two fixture e-mails (2026-09
 *  mega-batch): on a fresh stack users have zero contacts (created only by
 *  interaction — e.g. joining a project), which leaves the share modal's
 *  invite autocomplete empty (modals-p4 "invite round-trips" used to lean on
 *  other specs creating the contact — order-fragile). Mirrors
 *  ContactManager.addContact: the `contacts` collection stores
 *  { user_id, contacts: { [contactId]: { n, ts } } }. Keeps both users
 *  NON-members (an invite+join would create the contact but change
 *  membership — the invite round-trip specs require non-members). */
export function seedContactPair(emailA: string, emailB: string): boolean {
  const out = mongoEval(
    [
      `const sl = db.getSiblingDB('sharelatex');`,
      `const ua = sl.users.findOne({ email: ${JSON.stringify(emailA)} });`,
      `const ub = sl.users.findOne({ email: ${JSON.stringify(emailB)} });`,
      `if (!ua || !ub) {`,
      `  print('pair:missing');`,
      `} else {`,
      `  sl.contacts.updateOne({ user_id: ua._id }, { $set: { ['contacts.' + String(ub._id)]: { n: 1, ts: new Date() } } }, { upsert: true });`,
      `  sl.contacts.updateOne({ user_id: ub._id }, { $set: { ['contacts.' + String(ua._id)]: { n: 1, ts: new Date() } } }, { upsert: true });`,
      `  print('pair:ok');`,
      `}`,
    ].join('\n')
  )
  return /pair:ok/.test(out)
}

/**
 * 2026-09 (T5/mega-batch): seed the TEMPLATE ADMIN fixture on a fresh stack
 * (parity PG-REG-1 path — this build has no /user/activate, so the fixture
 * user is upserted directly with the image's bcrypt, same as
 * parity/crawl/ensure-fixture-tpladmin.mjs). Idempotent: no-op when the
 * account already exists. NOT isAdmin — canManageTemplates only.
 */
export function ensureTpladmin(email: string, password: string): boolean {
  if (userExists(email)) {
    // 2026-09 (mega-batch): the login flow invariant requires analyticsId +
    // labsProgram on the user doc (AnalyticsManager._getIdsFromMongoUser
    // throws otherwise → 500). Fix any fixture that predates this.
    const fixed = mongoEval(
      [
        `const sl = db.getSiblingDB('sharelatex');`,
        `const u = sl.users.findOne({ email: ${JSON.stringify(email)} }) || {};`,
        `if (u.analyticsId === undefined || u.labsProgram === undefined) {`,
        `  sl.users.updateOne({ _id: u._id }, { $set: { analyticsId: u.analyticsId || ${JSON.stringify(email + '-analytics')} , labsProgram: u.labsProgram === undefined ? false : u.labsProgram } });`,
        `  print('patched:true'); } else { print('patched:false'); }`,
      ].join('\n')
    )
    return /patched:(true|false)/.test(fixed)
  }
  const names = runningContainers()
  const overleaf = names.find(n => /overleaf/.test(n))
  if (!overleaf) throw new Error('ensureTpladmin: overleaf container not found')
  const bpath = '/overleaf/.yarn/unplugged/bcrypt-npm-6.0.0-fb16e34c40/node_modules/bcrypt'
  const hash = dockerExec(overleaf, [
    'node',
    '-e',
    `console.log(require('${bpath}').hashSync(process.argv[1],12))`,
    password,
  ])
    .trim()
    .split('\n')
    .pop()
  if (!/^\$2[aby]\$\d{2}\$/.test(hash)) {
    throw new Error(`ensureTpladmin: bcrypt hash generation failed (len=${hash.length})`)
  }
  const out = mongoEval(
    [
      `const sl = db.getSiblingDB('sharelatex');`,
      `sl.users.updateOne({ email: ${JSON.stringify(email)} },`,
      `  { $set: { first_name: 'E2e', last_name: 'Tpladmin', hashedPassword: ${JSON.stringify(hash)}, permissions: [], activated: true, canManageTemplates: true, isAdmin: false, analyticsId: ${JSON.stringify(email + '-analytics')}, labsProgram: false } },`,
      `  { upsert: true });`,
      `print('ready:' + !!(sl.users.findOne({ email: ${JSON.stringify(email)} })?.canManageTemplates === true));`,
    ].join('\n')
  )
  if (!/ready:true/.test(out)) {
    throw new Error(`ensureTpladmin: upsert did not confirm (out=${out.slice(0, 160)})`)
  }
  return true
}
