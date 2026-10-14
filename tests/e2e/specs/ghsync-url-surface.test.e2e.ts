/**
 * ghsync server-URL surface — BH-4 surgical SSRF guard (owner-approved, 2026-10-14).
 *
 * History: round-2 bug hunt proved the surface dials ANY supplied http(s) URL with
 * scheme-only vetting — linkPAT @ internal forgejo returned the internal service's
 * 401 MAPPED INTO THE RESPONSE (oracle); linkPAT @ 169.254.169.254 HUNG 30s+
 * (cloud-metadata dial = full SSRF read on a real VM). The owner approved the
 * SURGICAL guard (not a full block — the feature legitimately dials the user's OWN
 * self-hosted git, often on private RFC1918/ULA networks or private DNS names):
 *
 *   - 400 at parse time, no dial: literal-IP hosts in 0/8, 127/8, 169.254/16 (v4),
 *     and ::, ::1, fe80::/10 (v6).
 *   - allowed: RFC1918, ULA, public IPs, and hostnames (incl. private DNS) —
 *     the self-hosted-git trust model.
 *   - import/export are transitively covered (unsaved URLs are rejected at
 *     credential resolution).
 *
 * Legs:
 *   a) blocked literal loopback / metadata dials → synchronous 400 (oracle + hang gone)
 *   b) feature preservation: RFC1918 self-hosted git (forgejo) still DIALED and its
 *      check response still surfaced
 *   c) self-cleaning probe state (mongo; the DELETE route cannot address '/' URLs)
 */
import { test, expect } from '@playwright/test'
import { execSync } from 'child_process'
import { writeFileSync, unlinkSync } from 'fs'
import { ADMIN } from '../fixtures/credentials'
import { loginRobust } from '../helpers/auth'
import { api } from '../parity/harness'

async function b(r: any): Promise<string> {
  try {
    return String(await r.text())
  } catch {
    return ''
  }
}

async function cleanGhsyncProbe(): Promise<void> {
  const script = `
  db.githubSyncUserCredentials.find({}).forEach(function (d) {
    var unset = {};
    if (d.servers && d.servers.gitlab) unset['servers.gitlab'] = '';
    if (d.tokens && d.tokens.gitlab) unset['tokens.gitlab'] = '';
    if (Object.keys(unset).length) db.githubSyncUserCredentials.updateOne({ _id: d._id }, { $unset: unset });
  });
  db.githubSyncUserCredentials.find({}).forEach(function (d) {
    if (JSON.stringify(d).indexOf('172.20.0.18') !== -1) { throw new Error('RESIDUE after cleanup'); }
  });
  print('cleanup OK');
  `
  const p = '/tmp/bh4guard-clean.mjs'
  writeFileSync(p, script)
  try {
    const out = execSync(`docker exec -i ol-e2e-mongo-1 mongosh --quiet sharelatex \u003c ${p}`, { encoding: 'utf8' })
    expect(out).toContain('cleanup OK')
  } finally {
    unlinkSync(p)
  }
}

test('BH-4 (guarded): literal loopback / link-local git URLs are rejected with no dial', async ({ page }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)

  for (const u of ['http://127.0.0.1:3000', 'http://169.254.169.254', 'http://0.0.0.0', 'http://[::1]']) {
    await api(page, 'POST', '/user/git-servers', { provider: 'gitlab', url: u, username: 'bh4g' })
    const link = await api(page, 'POST', '/user/git-pat/link', {
      provider: 'gitlab',
      url: u,
      username: 'bh4g',
      pat: 'bogus-bh4',
    })
    const body = await b(link)
    expect(link.status(), `expected blocked-400 for ${u}`).toBe(400)
    expect(body, 'expected the blocked-host message').toContain('blocked host')
  }
})

test('BH-4 (guard): RFC1918 self-hosted git still dials (feature preserved, oracle scoped)', async ({ page }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  try {
    const add = await api(page, 'POST', '/user/git-servers', {
      provider: 'gitlab',
      url: 'http://172.20.0.18',
      username: 'bh4feat',
    })
    expect(add.status()).toBeLessThan(400)

    const link = await api(page, 'POST', '/user/git-pat/link', {
      provider: 'gitlab',
      url: 'http://172.20.0.18',
      username: 'bh4feat',
      pat: 'bogus-bh4-fixture',
    })
    const body = await b(link)
    // RFC1918 (the user's own self-hosted git range) is still DIALED — this is the
    // legitimate self-hosted-git case the surgical guard preserves. (If this leg
    // changes, the owner's adjudication for this range changed — re-review.)
    expect(link.status()).toBe(200)
    expect(body).toContain('Token invalid or expired')
    expect(body).not.toContain('blocked host')
  } finally {
    await cleanGhsyncProbe()
  }
})
