/**
 * git-bridge-edge-probe — the git-bridge EXPOSURE MAP, pinned as the
 * standing contract (what the outside world can reach, and under what
 * credentials).
 *
 * Verified wire reality (2026-11-11, e2e stack ag-v44 + hardened bridge):
 * the bridge container publishes NO host ports, but the overleaf edge
 * (nginx `location ^~ /git/` → rewrite → http://git-bridge:8000) PROXIES
 * `/git/*` into the bridge — that IS the advertised git URL surface
 * (clone `git:<token>@host/git/<pid>`). Consequences pinned here:
 *
 *   1. the bridge container itself exposes no published ports (no raw
 *      host-side reach; network adjacency is via the edge proxy);
 *   2. every CREDENTIAL- or DESTRUCTIVE-facing bridge surface is
 *      auth-gated end-to-end — anonymous access (edge or intra-network)
 *      is rejected 401 at the auth layer, including the project-deletion
 *      endpoint (DELETE /git/api/projects/<hex24> → 401, NEVER 204;
 *      the gate was added as a hardening OVER the Java original, which
 *      was authless — board rule: destructive endpoints never open);
 *   3. the NON-destructive observability surfaces are open by design
 *      (health/metrics/diags — operational practice, no credentials or
 *      content involved) and are ACCEPTED-with-rationale:
 *      - GET /git/status  → 200 "ok"
 *      - GET /git/metrics → 200 Prometheus text
 *      - GET /git/diags   → 200 native-memory-tracking dump
 *      (no secrets, no writes; blocking them would break the
 *      operational health/metrics probes — accepted, pinned so a change
 *      is a conscious decision).
 *
 * NOT pinned here: the federation deep-link URL contract (owner queue
 * TODO-eb764888); throwaway 24-hex ids only in the DELETE leg — no real
 * project ids ever.
 *
 * Run alone: npx playwright test specs/git-bridge-edge-probe.test.e2e.ts
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'child_process'
import { loginRobust } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const THROWAWAY = '00000000000000000000dead' // valid 24-hex, no such project

function containerOf(re: RegExp): string {
  const names: string[] = execFileSync('docker', ['ps', '--format', '{{.Names}}'], { encoding: 'utf8' })
    .split('\n')
    .filter(Boolean)
  const hit = names.find(n => re.test(n))
  if (!hit) throw new Error('container not found for ' + re)
  return hit
}

/** Edge (host-reachable) request. */
async function edge(method: string, path: string): Promise<{ code: number; body: string }> {
  const r = await fetch(BASE + path, { method, redirect: 'manual' })
  const body = await r.text().catch(() => '')
  return { code: r.status, body: body.slice(0, 200) }
}

/** Intra-network request (overleaf container = same compose network; it
 * ships python3, so any method/path is reachable). */
function intra(method: string, path: string): { code: number; out: string } {
  const url = 'http://git-bridge:8000' + path
  const py = `import urllib.request,urllib.error\nreq=urllib.request.Request('${url}',method='${method}')\ntry:\n r=urllib.request.urlopen(req,timeout=10);print(r.status);print(r.read()[:200])\nexcept urllib.error.HTTPError as e:\n print(e.code);print(e.read()[:200])\nexcept Exception as e:\n print('ERR',e)`
  const out = execFileSync('docker', ['exec', '--', containerOf(/-e2e-overleaf-1$/), 'python3', '-c', py], {
    encoding: 'utf8',
    timeout: 30_000,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const lines = out.trim().split('\n')
  return { code: Number(lines[0]) || 0, out: out.slice(0, 300) }
}

test.describe.configure({ mode: 'serial' })

let page: any = null
let ctx: any = null

test.beforeAll(async ({ browser }) => {
  ctx = await browser.newContext()
  page = await ctx.newPage()
  await loginRobust(page, ADMIN.email, ADMIN.password)
})

test.afterAll(async () => {
  if (ctx) await ctx.close().catch(() => {})
})

test('1. the bridge container publishes NO host ports (no raw host reach)', async () => {
  const out = execFileSync('docker', ['port', containerOf(/-e2e-git-bridge-1$/)], { encoding: 'utf8' })
  expect(out.trim(), 'no published ports for the git-bridge container').toBe('')
})

test('2. edge: credential & destructive surfaces are auth-gated; the git URL works only with credentials', async () => {
  // smart-HTTP advertisement (both services) → 401 challenge, never a
  // live refs document.
  for (const svc of ['git-upload-pack', 'git-receive-pack']) {
    const r = await edge('GET', `/git/${THROWAWAY}/info/refs?service=${svc}`)
    expect(r.code, `GET info/refs?service=${svc} must be 401 anonymous — got ${r.code}`).toBe(401)
  }
  // DESTRUCTIVE: the throwaway DELETE at the edge must be rejected (401)
  // — never 204 (no project deleted through the open surface).
  const d = await edge('DELETE', `/git/api/projects/${THROWAWAY}`)
  expect(d.code, `anonymous DELETE /git/api/projects → 401 (got ${d.code}: ${d.body})`).toBe(401)
  expect(d.code, 'anonymous DELETE must never 204').not.toBe(204)
})

test('3. edge: observability surfaces are open by design (accepted-with-rationale)', async () => {
  const st = await edge('GET', '/git/status')
  expect(st.code, 'GET /git/status open health surface').toBe(200)
  expect(st.body, 'health body "ok"').toMatch(/ok/)
  const me = await edge('GET', '/git/metrics')
  expect(me.code, 'GET /git/metrics open metrics surface').toBe(200)
  expect(me.body, 'prometheus text').toMatch(/# (HELP|TYPE)/)
  const dg = await edge('GET', '/git/diags')
  expect(dg.code, 'GET /git/diags open diags surface').toBe(200)
})

test('4. intra-network: the same gates hold on the bridge directly (no edge exception)', async () => {
  const adv = intra('GET', `/git/${THROWAWAY}/info/refs?service=git-upload-pack`)
  expect(adv.code, `intra anonymous info/refs must be 401 — got ${adv.code}`).toBe(401)
  const del = intra('DELETE', `/api/projects/${THROWAWAY}`)
  expect(del.code, `intra anonymous DELETE must be 401 — got ${del.code}`).toBe(401)
  expect(del.code, 'intra anonymous DELETE must never 204').not.toBe(204)
})
