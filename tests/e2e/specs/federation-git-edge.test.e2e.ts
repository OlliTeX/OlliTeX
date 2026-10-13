// FEDERATION git_url EDGE SUITE (ebb76488 — owner decision: Option 1 "serve it
// properly", 2026-10-13). The federation export payload advertises
// https://<host>/git/<pid> (go/services/web/features/federation/s2s_actions.go
// exportGitURL; shape asserted server-side in f2_export_test.go). This suite
// pins the EDGE that must serve it:
//   - allowlist (upstream shape, saveli/overleaf-cep 44a4454)
//       ^/git/[0-9a-f]{24}(\.git)?(/|$)  → proxied to git-bridge
//       (served = the git-bridge auth challenge, NOT an edge error)
//   - everything else under /git/ → 404 AT THE EDGE (was a blanket proxy)
//   - the dormant unauthenticated DELETE /api/projects/<id>
//     (go/services/gitbridge/server/delete.go) MUST NOT be reachable at the
//     edge (it is not under /git/ and the allowlist does not cover it)
// Canonical source: images/main-amd64/nginx/overleaf.conf.template
// (location ~ "^/git/[0-9a-f]{24}(\.git)?(/|$)" + the 404 catch-all).
// Note: a plain `location ~ pattern` with an UNQUOTED regex containing `{24}`
// breaks the nginx conf lexer (observed 2026-10-13) — the pattern is quoted.
import { test, expect } from '@playwright/test'

// a well-formed but unknown project id (24 hex) — the AUTH CHALLENGE must
// come back (route served), a 404/500 would mean the edge is not serving.
const PID = '64b3f8a1a4d6f9a53e2df001'

test.describe.configure({ mode: 'serial' })

test('G1: allowlisted clone paths reach git-bridge (401 auth challenge, served)', async ({ page }) => {
  for (const suffix of ['/info/refs?service=git-upload-pack', '', '/.git/HEAD']) {
    const res = await page.request.get(`/git/${PID}${suffix}`, { failOnStatusCode: false })
    const body = await res.text()
    console.log(`G1 /git/${PID}${suffix} → ${res.status()} :: ${body.slice(0, 60)}`)
    expect(res.status(), `G1 ${suffix || '(bare)'} served (auth challenge, not edge error)`).toBe(401)
    expect(body).toContain('Log in with the username')
  }
})

test('G2: non-allowlisted /git/ sub-paths 404 AT THE EDGE (blanket proxy closed)', async ({ page }) => {
  for (const p of ['/git/zzz/whatever', '/git/api/projects/' + PID, '/git/projects', '/git/']) {
    const res = await page.request.get(p, { failOnStatusCode: false })
    expect(res.status(), `G2 ${p} → 404 at the edge`).toBe(404)
  }
})

test('G3: the dormant unauthenticated DELETE /api/projects/<id> is NOT exposed', async ({ page }) => {
  const r1 = await page.request.delete(`/api/projects/${PID}`, { failOnStatusCode: false })
  const b1 = await r1.text()
  // The web CSRF gate currently answers 403 'Forbidden'; the acceptance is
  // the negative: it must NOT be 200/204 (a successful deletion via the edge
  // would be the security hole the upstream allowlist shape rules out).
  console.log(`G3 DELETE /api/projects/${PID} → ${r1.status()} :: ${b1.slice(0, 60)}`)
  expect([200, 204], 'G3 the dormant delete MUST NOT succeed via the edge').not.toContain(r1.status())
  // and the same shape under the git mount (allowlist does not cover /git/api)
  const r2 = await page.request.delete(`/git/api/projects/${PID}`, { failOnStatusCode: false })
  expect([200, 204], 'G3 /git/api/projects delete MUST NOT succeed').not.toContain(r2.status())
  console.log(`G3 DELETE /git/api/projects/${PID} → ${r2.status()}`)
})

test('G4: app plane unaffected (login page still served; /git/ hardening did not shadow routes)', async ({ page }) => {
  const res = await page.request.get('/login', { failOnStatusCode: false })
  expect(res.status()).toBe(200)
  const hub = await page.request.get('/hub', { failOnStatusCode: false })
  // anonymous /hub bounces to login (302) or serves the app page — both prove
  // the default route is intact.
  expect([200, 302]).toContain(hub.status())
})
