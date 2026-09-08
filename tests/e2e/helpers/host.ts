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
