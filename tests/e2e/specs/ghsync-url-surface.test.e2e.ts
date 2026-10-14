/**
 * ⚑ OWNER FLAG (round-2 bug hunt, 2026-10-14) — BH-4 ghsync server-URL SSRF surface.
 *
 * Observed (pinned below): the github-surface git-server endpoints DIAL any
 * http(s) URL the user supplies, with NO range/name/DNS vetting
 * (gsAssertHttpServerUrl = scheme check only):
 *   • linkPAT @ internal forgejo (172.20.0.18) → the 401 from the internal
 *     service is MAPPED INTO THE RESPONSE ("Token invalid or expired…") —
 *     a live content/status oracle over internal git infrastructure.
 *   • linkPAT @ 169.254.169.254 (cloud metadata) → the request HANGS (the
 *     server dials the metadata IP; on a real cloud VM this returns 200 with
 *     the metadata payload — the classic SSRF read).
 *   • cross-PROJECT repo cloning is (accidentally) defended by git-bridge's
 *     per-repo token gate — not by any URL guard.
 *
 * DESIGN NUANCE (why this is an owner call, not a silent fix): the feature
 * legitimately dials the USER'S OWN self-hosted git instance (GHE/Forgejo —
 * often on a private LAN), so a full SSRF block (like LLM/WebDAV) would
 * BREAK the feature. Surgical recommendation (owner's call): block ONLY
 * the ranges that are NEVER a legitimate user git host — loopback 127/8,
 * link-local 169.254/16, 0/8, :: — while allowing RFC1918 + public + names.
 *
 * Self-cleaning: the probe's `gitlab` provider entries are removed from
 * githubSyncUserCredentials after the test (route DELETE can't express
 * slash-bearing server URLs — a separate minor state bug, R2-2).
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

test('BH-4 (pin): ghsync linkPAT dials the supplied git URL and maps its auth semantics', async ({ page }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)

  await api(page, 'POST', '/user/git-servers', {
    provider: 'gitlab',
    url: 'http://172.20.0.18',
    username: 'bh4pin',
  })
  const link = await api(page, 'POST', '/user/git-pat/link', {
    provider: 'gitlab',
    url: 'http://172.20.0.18',
    username: 'bh4pin',
    pat: 'bogus-bh4-pin',
  })
  const body = await b(link)
  // The pin: the INTERNAL git server's 401 reached the product and was
  // mapped into the caller-facing message (oracle proof). If a range guard
  // is later adopted, this assertion is the one to flip.
  expect(body, 'expected the dialed-401 mapping (oracle pin) — see spec header for the owner flag').toContain(
    'Token invalid or expired',
  )

  // ---- self-cleanup (mongo: the DELETE route cannot address '/'-bearing server URLs) ----
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
  const p = '/tmp/bh4pin-clean.mjs'
  writeFileSync(p, script)
  const out = execSync(`docker exec -i ol-e2e-mongo-1 mongosh --quiet sharelatex < ${p}`, { encoding: 'utf8' })
  unlinkSync(p)
  expect(out).toContain('cleanup OK')
})
