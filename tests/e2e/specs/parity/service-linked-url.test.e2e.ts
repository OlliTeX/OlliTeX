/**
 * B2 (GO_CUTOVER_PLAN.md) — linked-url-proxy Node↔Go parity gate.
 *
 * The contract is pinned by the deterministic battery in
 * `tests/e2e/parity/lup/battery.js` (21 cases: health, method/path 404s with
 * the EXACT Express page, missing-param 400, invalid-URL 500s with the exact
 * `Invalid url to pass to open(): <raw>` messages, blocked-IP 403s, DNS 421,
 * upstream 200/404/redirect/413/422/408 cases, >2000-char path 400) and is
 * derived 1:1 from the Node sources (services/linked-url-proxy/app/js/*,
 * strict-url-sanitise@0.0.1, als-normalize-urlpath@2.3.0,
 * libraries/fetch-utils).
 *
 * Legs (serial, offline):
 *   1. NODE CONTRACT   on the disposable ol-e2e stack (proxy forced to Node)
 *   2. GO PARITY       on the disposable ol-e2e stack (proxy forced to the
 *                      repo's freshly built Go binary)
 *   3. LIVE GO         on `overleafserver` (the serving instance, forced to
 *                      the same binary) — then test artifacts are cleaned.
 *
 * All three legs must be 21/21. Upstream-dependent cases use a loopback-only
 * test upstream (127.0.0.1:9991) exempted via OVERLEAF_LINKED_URL_ALLOWED_RESOURCES;
 * the exemption is removed again on the live container after the leg.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// Resolve the repo root from THIS spec file (specs/parity/x.test.e2e.ts → overleaf/) —
// cwd-independent, ESM-safe.
const THIS_FILE = fileURLToPath(import.meta.url)
const REPO_ROOT = path.resolve(path.dirname(THIS_FILE), '..', '..', '..', '..')
const LUP_DIR = path.join(REPO_ROOT, 'tests', 'e2e', 'parity', 'lup')
const BATTERY = path.join(LUP_DIR, 'battery.js')
const UPSTREAM = path.join(LUP_DIR, 'upstream.js')
const GO_BIN = path.join(REPO_ROOT, 'bin', 'go-services', 'linked-url-proxy')
const PROXY_SVC = 'linked-url-proxy-overleaf'
const FLAG_FILE = '/etc/overleaf/env.d/ollitex-lup-flag.sh'
const BATTERY_ENV_FILE = '/etc/overleaf/env.d/ollitex-lup-battery.sh'
const EXPECTED = 'BATTERY: 21 passed, 0 failed'

function e2eContainer() {
  try {
    return execFileSync('docker', ['ps', '--filter', 'name=ol-e2e-overleaf', '--format', '{{.Names}}'], { encoding: 'utf8' }).trim().split('\n')[0]
  } catch {
    return ''
  }
}

function ensureGoBin() {
  if (existsSync(GO_BIN)) return
  // Best effort: build from the repo Go sources when the toolchain is present.
  try {
    execFileSync('go', ['build', '-o', GO_BIN, './cmd/linked-url-proxy'], { cwd: REPO_ROOT, stdio: 'pipe', timeout: 180_000 })
  } catch {
    expect(existsSync(GO_BIN), `Go binary missing: build it with \`cd ${REPO_ROOT} && go build -o bin/go-services/linked-url-proxy ./cmd/linked-url-proxy\``).toBeTruthy()
  }
}

function dexe(container: string, cmd: string, { allowFail = false }: { allowFail?: boolean } = {}) {
  try {
    return execFileSync('docker', ['exec', container, 'bash', '-lc', cmd], { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024, timeout: 180_000 })
  } catch (e: any) {
    if (allowFail) return `${e.stdout || ''}${e.stderr || ''}`.trim()
    throw new Error(`docker exec ${container} failed: ${cmd}\n${e.stdout || ''}${e.stderr || ''}`)
  }
}

function sleepS(s: number) {
  execFileSync('sleep', [String(s)])
}

function dcp(src: string, container: string, dst: string) {
  execFileSync('docker', ['cp', src, `${container}:${dst}`], { timeout: 120_000 })
}

function provision(container: string) {
  dcp(UPSTREAM, container, '/tmp/lup-upstream.js')
  dcp(BATTERY, container, '/tmp/lup-battery.js')
  dexe(container, `
    mkdir -p /etc/overleaf/env.d
    cat > ${BATTERY_ENV_FILE} <<'EOF'
# B2 battery only: loopback test upstreams exempt from blocked-IP check; 1MB cap for the 413 case.
export OVERLEAF_LINKED_URL_ALLOWED_RESOURCES="^http://127\\.0\\.0\\.1:(9991|9992)/"
export MAX_UPLOAD_SIZE=1
EOF
    mkdir -p /etc/service/lup-upstream
    printf '#!/bin/bash\nexec node /tmp/lup-upstream.js >> /var/log/lup-upstream.log 2>&1\n' > /etc/service/lup-upstream/run
    chmod 755 /etc/service/lup-upstream/run
    ln -sfn /etc/service/lup-upstream /service/lup-upstream
    sv up lup-upstream 2>/dev/null || sv restart lup-upstream
  `, { allowFail: true })
  // wait for upstream readiness (up to 10s)
  let up = ''
  for (let i = 0; i < 40; i++) {
    up = dexe(container, 'curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:9991/ok', { allowFail: true }).trim()
    if (up === '200') break
    sleepS(0.25)
  }
  expect(up, `test upstream on 127.0.0.1:9991 inside ${container}`).toBe('200')
}

function stopUpstream(container: string) {
  dexe(container, 'sv down lup-upstream 2>/dev/null || true', { allowFail: true })
}

function setProxyMode(container: string, go: boolean) {
  dexe(container, `
    mkdir -p /etc/overleaf/env.d
    if [ ${go ? 1 : 0} -eq 1 ]; then
      printf 'export USE_GO_LINKED_URL_PROXY=true\\n' > ${FLAG_FILE}
    else
      printf 'export USE_GO_LINKED_URL_PROXY=\\n' > ${FLAG_FILE}
    fi
    sv restart ${PROXY_SVC}
  `)
  // wait for readiness (up to 10s)
  let ready = false
  for (let i = 0; i < 40; i++) {
    const out = dexe(container, 'curl -s -m 2 -o /dev/null -w "%{http_code}" http://127.0.0.1:3066/status', { allowFail: true })
    if (out.trim() === '200') { ready = true; break }
    sleepS(0.25)
  }
  expect(ready, `linked-url-proxy on ${container} not ready`).toBeTruthy()
}

function runBattery(container: string): string {
  const out = dexe(container, 'node /tmp/lup-battery.js up', { allowFail: true })
  return out
}

function cleanupContainer(container: string, keepGo: boolean) {
  dexe(container, `
    sv down lup-upstream 2>/dev/null || true
    [ ${keepGo ? 1 : 0} -eq 1 ] || rm -f ${FLAG_FILE}
    sv restart ${PROXY_SVC} 2>/dev/null || true
  `, { allowFail: true })
}

test.describe.configure({ mode: 'serial' })

test('1: NODE contract battery — 21/21 (ol-e2e stack, proxy=Node)', () => {
  const c = e2eContainer()
  expect(c, 'ol-e2e container running').toBeTruthy()
  ensureGoBin()
  provision(c)
  setProxyMode(c, false)
  const out = runBattery(c)
  expect(out, out).toContain(EXPECTED)
})

test('2: GO parity battery — 21/21 (ol-e2e stack, proxy=Go)', () => {
  const c = e2eContainer()
  expect(c, 'ol-e2e container running').toBeTruthy()
  ensureGoBin()
  dcp(GO_BIN, c, '/usr/local/bin/go-services/linked-url-proxy')
  provision(c)
  setProxyMode(c, true)
  const out = runBattery(c)
  expect(out, out).toContain(EXPECTED)
})

test('3: LIVE Go parity battery — 21/21 (overleafserver), then clean test artifacts', () => {
  const c = 'overleafserver'
  const up = execFileSync('docker', ['ps', '--filter', `name=${c}`, '--format', '{{.Names}}'], { encoding: 'utf8' }).trim()
  if (up !== c) {
    test.skip(true, `container ${c} not running — live leg skipped (e2e legs still gate the flip)`)
    return
  }
  ensureGoBin()
  dcp(GO_BIN, c, '/usr/local/bin/go-services/linked-url-proxy')
  dexe(c, 'test -f /etc/overleaf/env.d/ollitex-gocutover.sh', { allowFail: true })
  provision(c)
  setProxyMode(c, true)
  const out = runBattery(c)
  // Clean the loopback SSRF escape hatch + test upstream from the LIVE container.
  dexe(c, `rm -f ${BATTERY_ENV_FILE}; printf 'export USE_GO_LINKED_URL_PROXY=true\\n' > ${FLAG_FILE}; sv down lup-upstream 2>/dev/null || true; rm -rf /etc/service/lup-upstream; sv restart ${PROXY_SVC}`)
  const status = dexe(c, 'curl -s -m 3 http://127.0.0.1:3066/status', { allowFail: true })
  expect(out, out).toContain(EXPECTED)
  expect(status, 'live /status after cleanup').toContain('up')
})

test.afterAll(() => {
  // Leave ol-e2e in a clean Node state (batteries re-provision each run) with the
  // loopback test upstream stopped; live keeps its Go flag from the previous leg.
  const c = e2eContainer()
  if (!c) return
  try {
    dexe(c, `rm -f ${BATTERY_ENV_FILE} ${FLAG_FILE}; sv down lup-upstream 2>/dev/null || true; sv restart ${PROXY_SVC} 2>/dev/null || true`, { allowFail: true })
  } catch { /* disposable stack gone */ }
})
