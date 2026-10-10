/**
 * FIXTURE CREDENTIALS — the controlled identities every e2e spec runs as.
 *
 * Owner decision 2026-09-05: "tests run against a controlled test instance
 * (like forgejo)" — so identities are FIXED and documented here, created by
 * global-setup (deterministic seed), and NEVER by a spec at random.
 *
 * Policy:
 *  - these accounts exist ONLY in the disposable test stack (ol-e2e volumes);
 *  - stack-reset.sh wipes them (fresh volume + re-seed);
 *  - passwords are test-only dummies (no real credential anywhere in this
 *    tree — owner rule).
 */
export const ADMIN = {
  email: 'e2e-admin@e2e.test',
  // NOTE: CE rejects passwords too SIMILAR to the address (password must
  // not contain e2e/admin/test parts and must stay in the allowed char set)
  password: 'Ol-Fixture-9x7K',
  first_name: 'E2e',
  last_name: 'Admin',
  isAdmin: true,
} as const

export const USER = {
  email: 'e2e-user@e2e.test',
  password: 'Ol-Fixture-3m2Q',
  first_name: 'E2e',
  last_name: 'User',
  isAdmin: false,
} as const

/**
 * USER2 — the second collaborator (Q: two-cooperator same-file concurrency
 * matrix, 2026-10-10). A second, independent identity so the browser runs two
 * GENUINE sessions (two accounts, two contexts) against the same project
 * file — the single-user two-tab pattern cannot exercise the cross-user
 * privilege/collab path. Same fixture policy as the others (test-only
 * dummy password, stack-disposable).
 */
export const USER2 = {
  email: 'e2e-collab@e2e.test',
  password: 'Ol-Fixture-8p4R',
  first_name: 'E2e',
  last_name: 'Collab',
  isAdmin: false,
} as const

/**
 * TEMPLATE ADMIN (owner role, 2026-09-07): canManageTemplates=true, NOT a site
 * admin. Grants /templates/manage without the rest of /admin. Created by the
 * phase-0 fixture step (register+activate) then promoted via mongo — mirrors
 * promoteAdmin's mongo path (tests/e2e/helpers/host.ts#promoteAdmin).
 */
export const TPLADMIN = {
  email: 'e2e-tpladmin@e2e.test',
  // same policy as the others: test-only dummy, must not contain e2e/admin/test
  password: 'Ol-Fixture-7tW4',
  first_name: 'E2e',
  last_name: 'Tpladmin',
  isAdmin: false,
  canManageTemplates: true,
} as const

/** Seeded project (created by the seed step via the app API, so it goes
 *  through the real project-creation code path). */
export const SEED_PROJECT = {
  name: 'e2e-seed-project',
  owner: ADMIN.email,
} as const
