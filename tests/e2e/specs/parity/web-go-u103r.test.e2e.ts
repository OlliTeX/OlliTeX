// u103r: private-API basic-auth wire contract — WEB profile, standalone.
//
// CONVERTED 2026-10-05 (owner): the 3-leg shadow comparison (Node web :4000
// vs Go shadow :4010 vs Node api :3000) is retired — in the P7 image the
// canonical stack IS Go, so :4000/:3000 are both Go profiles and the shadow
// :4010 no longer exists. The wire is now pinned DIRECTLY on canonical Go
// (web profile here; the api profile is pinned by web-go-uapi-doc) + 2-run
// byte-parity stability. Transcription, not rewrite (u101-history pattern).
//
// Surface (from u103r-matrix.cjs), canonical Go web :4000:
//   doc GET no-auth    -> 401 json (www OverleafLogin) / 302 -> /login (html+plain)
//   doc GET wrong cred -> 401 (ALL accepts)
//   doc POST no csrf   -> 403 "Forbidden" (authz boundary before authn; no 400)
//   (no valid-cred case: session-only route)
// csrf + nonce + sid are random: normalized where they appear.
import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import path from 'node:path'

const overleafC = 'ol-e2e-overleaf-1'

const MATRIX = ((): string => {
  const cands = [
    path.join(process.cwd(), 'specs/parity', 'u103r-matrix.cjs'),
    path.join(process.cwd(), 'u103r-matrix.cjs'),
    path.join('/data_1/image_mining/the_diff/overleaf/tests/e2e/specs/parity', 'u103r-matrix.cjs'),
  ]
  return cands.find((p) => existsSync(p)) || cands[0]
})()

function dexe(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], {
    encoding: 'utf8', stdio: 'pipe', maxBuffer: 1 << 28,
  }) || ''
}

function runMatrix(): string[] {
  execFileSync('docker', ['cp', MATRIX, overleafC + ':/tmp/u103r-matrix.cjs'], { stdio: 'ignore' })
  const out = dexe(overleafC, 'node /tmp/u103r-matrix.cjs 1')
  const lines = out.split('\n').filter(Boolean)
  if (lines.length === 0) throw new Error('matrix produced no output')
  return lines
}

test('u103r: private-API basic-auth wire contract (canonical Go web :4000)', async () => {
  test.setTimeout(120_000)
  const lines = runMatrix()
  const expected = [
  "doc-unauth-json|401|text/plain; charset=utf-8§§W/\"c-H\"§overleaf.sid=X;Path=X;Expires=X;HttpOnly;SameSite=X§§§OverleafLogin§same-origin-allow-popups§same-origin§origin-when-cross-origin§nosniff§noopen§SAMEORIGIN§none§0§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
    "doc-unauth-html|302|text/html; charset=utf-8§§§overleaf.sid=X;Path=X;Expires=X;HttpOnly;SameSite=X§/login§Accept§§same-origin-allow-popups§same-origin§origin-when-cross-origin§nosniff§noopen§SAMEORIGIN§none§0§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|<p>Found. Redirecting to /login</p>",
    "doc-unauth-plain|302|text/plain; charset=utf-8§§§overleaf.sid=X;Path=X;Expires=X;HttpOnly;SameSite=X§/login§Accept§§same-origin-allow-popups§same-origin§origin-when-cross-origin§nosniff§noopen§SAMEORIGIN§none§0§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Found. Redirecting to /login",
    "doc-wrong-json|401|text/plain; charset=utf-8§§W/\"c-H\"§overleaf.sid=X;Path=X;Expires=X;HttpOnly;SameSite=X§§§OverleafLogin§same-origin-allow-popups§same-origin§origin-when-cross-origin§nosniff§noopen§SAMEORIGIN§none§0§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
    "doc-wrong-html|401|text/plain; charset=utf-8§§W/\"c-H\"§overleaf.sid=X;Path=X;Expires=X;HttpOnly;SameSite=X§§§OverleafLogin§same-origin-allow-popups§same-origin§origin-when-cross-origin§nosniff§noopen§SAMEORIGIN§none§0§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
    "post-doc-nocsrf|403|text/plain; charset=utf-8§Express§W/\"9-H\"§overleaf.sid=X;Path=X;Expires=X;HttpOnly;SameSite=X§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Forbidden",
    "post-rej-nocsrf|403|text/plain; charset=utf-8§Express§W/\"9-H\"§overleaf.sid=X;Path=X;Expires=X;HttpOnly;SameSite=X§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Forbidden"
  ]
  expect(lines.join('\n')).toBe(expected.join('\n'))
})

test('u103r: 2-run stability (byte parity, canonical Go web :4000)', async () => {
  test.setTimeout(180_000)
  const a = runMatrix().join('\n')
  const b = runMatrix().join('\n')
  expect(b).toBe(a)
})
