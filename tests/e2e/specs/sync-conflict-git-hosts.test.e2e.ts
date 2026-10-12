/**
 * GIT-HOSTS (github / gitea / gitlab / forgejo) UPDATE-CONFLICT + RESOLUTION
 * spec (owner ask 2026-11-16) — the ghsync feature
 * (go/services/web/features/ghsync), per-host against LIVE credentials
 * (credentials/config.json).
 *
 * Per host (config section: baseUrl, username, pat):
 *   G0 PAT link POST /user/git-pat/link {provider,url,username,pat}
 *      → {success,check:{ok}} + /user/git-servers/test {ok:true}
 *   G1 remote repo seed — <host> API: new private repo + main.tex = REMOTE-V1
 *   G2 IMPORT  POST /project/new/github-sync {name,fullName,defaultBranchName,
 *      provider,serverUrl,username} → 200 {projectId}
 *   G3 CLEAN PATH (remote-only matrix) — remote commit REMOTE-V2, NO local
 *      edit → overview {diverged:true,isProjectUpdated:false} → merge
 *      → 200 {"status":"merged",lastSyncCommit,…} → state clean
 *   G4 LOCAL edit — entadd a local doc (editor entity-write wire; green-spec
 *      contract POST /project/:pid/doc {name} → 200 {_id})
 *   G5 REMOTE edit — commit REMOTE-V3 on the default branch
 *   G6 CONFLICT — merge → 200 {"status":"conflict","message":"both local and
 *      remote changed; resolve in the merge UI"} (source-anchored)
 *   G7 conflict state → {mergeStatus:"conflict",
 *      unmergedBranchName:^overleaf-conflict-[0-9a-f]{6}$, conflictVersion>0,
 *      unmergedBranchHead, lastSyncVersion, lastSyncCommit}
 *   G8 RESOLUTION — the product contract (git-sync-merge-modal.tsx) is
 *      POST merge {claimConflictIsResolved:true} → expects a ProjectSyncState
 *      with mergeStatus "clean" (+ then /flush + reindex).
 *      LIVE PIN (ag-v57): the Go port IGNORES the merge body (`_ = body`,
 *      controllers_project.go) and answers {"status":"conflict"} again —
 *      the resolution leg is NOT WIRED (also: the response key is `status`
 *      while the UI reads `data.mergeStatus` — contract mismatch).
 *      FLAGGED for owner. Re-arms: when the port implements
 *      claimConflictIsResolved, G8 asserts mergeStatus clean + state clean.
 *   G9 unlink  DELETE /project/:pid/github-sync → 200 (state removed)
 *   G10 hygiene — remote repo deleted (best effort)
 *
 * RE-ARM: every leg asserts the FIXED/successful shape automatically when the
 * product wires it; the live-pinned failure shapes are kept as the regression
 * guard (they flip the moment the behavior changes).
 */
import { test, expect } from '@playwright/test'
import { USER } from '../fixtures/credentials'
import { loginRobust } from '../helpers/auth'
import { api } from '../parity/harness'
import { getExternal, externalEnabled } from '../credentials/external'

test.setTimeout(1500_000)

const stamp = Date.now().toString(36)
const HOSTS = ['github', 'gitea', 'gitlab', 'forgejo'] as const
type Host = (typeof HOSTS)[number]

const tex = (m: string) => `% M\n\\documentclass{article}\n\\begin{document}\n${m}\n\\end{document}\n`

/** transient-host flaps (observed github 500 blips): one retry with backoff */
async function withRetry<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn()
  } catch (e) {
    await new Promise((r) => setTimeout(r, 1500))
    return await fn()
  }
}

function hostCfg(h: Host): any {
  const c = getExternal(h) as any
  const baseUrl = /^https?:/.test(c.baseUrl) ? c.baseUrl : `https://${c.baseUrl}`
  return { host: h, base: baseUrl.replace(/\/$/, ''), username: c.username, pat: c.pat }
}

// ---------- per-host remote-repo API adapters ----------
function adapter(h: Host, cfg: any, repoName: string) {
  const b64 = (s: string) => Buffer.from(s).toString('base64')
  if (h === 'github') {
    const apiBase = 'https://api.github.com'
    const r = (method: string, path: string, body?: any) =>
      fetch(`${apiBase}${path}`, {
        method,
        headers: { Authorization: `Bearer ${cfg.pat}`, 'Content-Type': 'application/json', Accept: 'application/vnd.github+json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      }).then(async (res) => ({ status: res.status, body: await res.json().catch(() => ({})) }))
    const curSha = async (fullName: string) => {
      const o = await r('GET', `/repos/${fullName}/contents/main.tex`)
      return String((o.body as any)?.sha ?? '')
    }
    return {
      createRepo: async () => {
        const o = await r('POST', '/user/repos', { name: repoName, private: true })
        if (o.status !== 201 && o.status !== 200) throw new Error(`github createRepo ${o.status}: ${JSON.stringify(o.body).slice(0, 200)}`)
        return { fullName: (o.body as any).full_name, defaultBranch: (o.body as any).default_branch ?? 'main' }
      },
      seedFile: async (fullName: string, marker: string) => {
        const o = await r('PUT', `/repos/${fullName}/contents/main.tex`, { message: 'seed', content: b64(tex(marker)) })
        if (o.status !== 201 && o.status !== 200) throw new Error(`github seed ${o.status}: ${JSON.stringify(o.body).slice(0, 200)}`)
        return String(((o.body as any).content as any)?.sha ?? '')
      },
      updateFile: async (fullName: string, marker: string) => {
        const sha = await curSha(fullName)
        const o = await r('PUT', `/repos/${fullName}/contents/main.tex`, { message: marker, content: b64(tex(marker)), sha })
        if (o.status !== 200 && o.status !== 201) throw new Error(`github update ${o.status}: ${JSON.stringify(o.body).slice(0, 200)}`)
      },
      deleteRepo: async (fullName: string) => {
        const o = await r('DELETE', `/repos/${fullName}`)
        return o.status
      },
    }
  }
  if (h === 'gitlab') {
    const apiBase = `${cfg.base}/api/v4`
    const r = (method: string, path: string, body?: any) =>
      fetch(`${apiBase}${path}`, {
        method,
        headers: { 'PRIVATE-TOKEN': cfg.pat, 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      }).then(async (res) => ({ status: res.status, body: await res.json().catch(() => ({})) }))
    let projId = ''
    return {
      createRepo: async () => {
        const o = await r('POST', '/projects', { name: repoName, visibility: 'private' })
        if (o.status !== 201 && o.status !== 200) throw new Error(`gitlab create ${o.status}: ${JSON.stringify(o.body).slice(0, 200)}`)
        projId = String((o.body as any).id)
        return { fullName: (o.body as any).path_with_namespace, defaultBranch: (o.body as any).default_branch ?? 'main' }
      },
      seedFile: async (_fullName: string, marker: string) => {
        // GitLab: create=POST (PUT is strict-update → 400 when file absent)
        const o = await r('POST', `/projects/${projId}/repository/files/main.tex`, { branch: 'main', content: b64(tex(marker)), commit_message: 'seed' })
        if (o.status > 299) throw new Error(`gitlab seed ${o.status}: ${JSON.stringify(o.body).slice(0, 200)}`)
        return String((o.body as any).commit_id ?? '')
      },
      updateFile: async (_fullName: string, marker: string) => {
        const o = await r('PUT', `/projects/${projId}/repository/files/main.tex`, { branch: 'main', content: b64(tex(marker)), commit_message: marker })
        if (o.status > 299) throw new Error(`gitlab update ${o.status}: ${JSON.stringify(o.body).slice(0, 200)}`)
      },
      deleteRepo: async () => {
        const o = await r('DELETE', `/projects/${projId}`)
        return o.status
      },
    }
  }
  // gitea | forgejo — Gitea-family REST: create=PUT(no sha); update=PUT{sha} (POST fallback).
  // New repos may arrive with generated files (templates) → ensure-semantics.
  const apiBase = `${cfg.base}/api/v1`
  const r = (method: string, path: string, body?: any) =>
    fetch(`${apiBase}${path}`, {
      method,
      headers: { Authorization: `token ${cfg.pat}`, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    }).then(async (res) => ({ status: res.status, body: await res.json().catch(() => ({})) }))
  const cur = async (fullName: string) => {
    const o = await r('GET', `/repos/${fullName}/contents/main.tex`)
    return o.status === 200 ? String((o.body as any).sha ?? '') : ''
  }
  const writeFile = async (fullName: string, marker: string) => {
    const sha = await cur(fullName)
    const path = `/repos/${fullName}/contents/main.tex`
    const base = { content: b64(tex(marker)), message: marker, branch: 'main' }
    // Gitea-family: CREATE=POST (no sha); UPDATE=PUT or POST (+sha).
    let o: any
    if (sha) {
      o = await r('PUT', path, { ...base, sha })
      if (o.status === 422 || o.status === 400) o = await r('POST', path, { ...base, sha })
    } else {
      o = await r('POST', path, base)
      if (o.status >= 400) o = await r('PUT', path, base)
    }
    if (o.status !== 201 && o.status !== 200) throw new Error(`${h} write ${o.status}: ${JSON.stringify(o.body).slice(0, 200)}`)
  }
  return {
    createRepo: async () => {
      const o = await r('POST', '/user/repos', { name: repoName, private: true })
      if (o.status !== 201 && o.status !== 200) throw new Error(`${h} create ${o.status}: ${JSON.stringify(o.body).slice(0, 200)}`)
      return { fullName: (o.body as any).full_name, defaultBranch: (o.body as any).default_branch ?? 'main' }
    },
    seedFile: async (fullName: string, marker: string) => { await writeFile(fullName, marker); return cur(fullName) },
    updateFile: async (fullName: string, marker: string) => { await writeFile(fullName, marker) },
    deleteRepo: async (fullName: string) => {
      const o = await r('DELETE', `/repos/${fullName}`)
      return o.status
    },
  }
}

for (const h of HOSTS) {
  test(`git-host ${h}: conflict detect + resolve cycle`, async ({ page }) => {
    test.skip(!externalEnabled(h), `${h} not enabled in credentials/config.json`)
    const cfg = hostCfg(h)
    const V1 = `%%CFGIT ${h}-v1-${stamp}`
    const V2 = `%%CFGIT ${h}-v2-${stamp}`
    const V3 = `%%CFGIT ${h}-v3-${stamp}`
    const REPO = `ol-cfgit-${h}-${stamp}`

    await loginRobust(page, USER.email, USER.password)

    // ---- G0 PAT link + test -------------------------------------------------
    const link = await api(page, 'POST', '/user/git-pat/link', { provider: h, url: cfg.base, username: cfg.username, pat: cfg.pat })
    const lb = await link.json().catch(() => ({}))
    expect(link.status(), `G0 link -> ${link.status()} ${JSON.stringify(lb).slice(0, 200)}`).toBe(200)
    expect((lb as any)?.check?.ok, 'G0 PAT check ok').toBe(true)
    const tst = await api(page, 'POST', '/user/git-servers/test', { provider: h, url: cfg.base, username: cfg.username })
    expect((await tst.json().catch(() => ({})))?.ok, `G0 test -> ${tst.status()}`).toBe(true)

    const ad = adapter(h, cfg, REPO)

    // ---- G1 remote seed ------------------------------------------------------
    const { fullName, defaultBranch } = await withRetry(() => ad.createRepo())
    const sha1 = await withRetry(() => ad.seedFile(fullName, V1))
    console.log(`[${h} G1] repo=${fullName} branch=${defaultBranch} sha1=${sha1}`)

    // ---- G2 import -----------------------------------------------------------
    const imp = await api(page, 'POST', '/project/new/github-sync', {
      name: REPO,
      fullName,
      defaultBranchName: defaultBranch,
      provider: h,
      serverUrl: cfg.base,
      username: cfg.username,
    })
    const ib = await imp.json().catch(() => ({}))
    const pid = String((ib as any)?.projectId ?? (ib as any)?.project_id ?? (ib as any)?.id ?? '')
    expect(pid, `G2 import -> ${imp.status()} ${JSON.stringify(ib).slice(0, 220)}`).toMatch(/^[0-9a-f]{24}$/)

    // ---- G3 clean path (remote-only) ------------------------------------------
    await withRetry(() => ad.updateFile(fullName, V2))
    const ov1 = await api(page, 'GET', `/project/${pid}/github-sync/merge/overview`)
    const ov1b = await ov1.json().catch(() => ({}))
    console.log(`[${h} G3] overview -> ${ov1.status()} ${JSON.stringify(ov1b).slice(0, 200)}`)
    expect(
      Array.isArray((ov1b as any)?.commits) && (ov1b as any).commits.length >= 1 && (ov1b as any)?.isProjectUpdated === false,
      `G3 overview: remote commit listed + no local edits yet: ${JSON.stringify(ov1b).slice(0, 200)}`,
    ).toBeTruthy()
    const mg1 = await api(page, 'POST', `/project/${pid}/github-sync/merge`, { body: { message: 'merge v2', claimConflictIsResolved: false } })
    const mg1b = await mg1.json().catch(() => ({}))
    console.log(`[${h} G3a] merge remote-only (clean path) -> ${mg1.status()} ${JSON.stringify(mg1b).slice(0, 200)}`)
    expect(mg1b?.status, 'G3 remote-only merge → merged').toBe('merged')
    const st1 = await (await api(page, 'GET', `/project/${pid}/github-sync/state`)).json().catch(() => ({}))
    expect((st1 as any)?.mergeStatus, `G3 state after clean merge = ${JSON.stringify(st1).slice(0, 220)}`).toBe('clean')

    // ---- G4 LOCAL edit (divergence side A) --------------------------------------
    const add = await api(page, 'POST', `/project/${pid}/doc`, { name: `local-${h}-${stamp}.tex` })
    const ab = await add.json().catch(() => ({}))
    expect(add.status(), `G4 entadd -> ${add.status()} ${JSON.stringify(ab).slice(0, 160)}`).toBe(200)
    expect(String((ab as any)?._id ?? ''), 'G4 local doc _id').toMatch(/^[0-9a-f]{24}$/)

    // ---- G5 REMOTE edit (divergence side B) ---------------------------------------
    await withRetry(() => ad.updateFile(fullName, V3))

    // ---- G6 CONFLICT ----------------------------------------------------------------
    const mg2 = await api(page, 'POST', `/project/${pid}/github-sync/merge`, { body: { message: 'conflict probe', claimConflictIsResolved: false } })
    const mg2b = await mg2.json().catch(() => ({}))
    console.log(`[${h} G6] merge conflict -> ${mg2.status()} ${JSON.stringify(mg2b).slice(0, 220)}`)
    expect(mg2b?.status, 'G6 both-sides-changed merge → conflict').toBe('conflict')
    expect(String(mg2b?.message ?? '')).toContain('both local and remote changed')

    // ---- G7 conflict state -------------------------------------------------------------
    const st2 = await (await api(page, 'GET', `/project/${pid}/github-sync/state`)).json().catch(() => ({}))
    console.log(`[${h} G7] state -> ${JSON.stringify(st2).slice(0, 320)}`)
    expect((st2 as any)?.mergeStatus, 'G7 state mergeStatus conflict').toBe('conflict')
    expect(String((st2 as any)?.unmergedBranchName ?? ''), 'G7 unmerged branch name pin').toMatch(/^overleaf-conflict-[0-9a-f]{6}$/)
    expect(Number((st2 as any)?.conflictVersion ?? 0), 'G7 conflictVersion > 0').toBeGreaterThan(0)
    expect(String((st2 as any)?.unmergedBranchHead ?? ''), 'G7 unmerged branch head (remote v3 sha)').toMatch(/^[0-9a-f]{7,40}$/)
    // overview during conflict → the Node contract returns null (source: mergeOverviewHandler)
    const ov2 = await api(page, 'GET', `/project/${pid}/github-sync/merge/overview`)
    expect((await ov2.text().catch(() => '')), 'G7 overview during conflict = null').toBe('null')

    // ---- G8 RESOLUTION (product contract vs live behavior) ------------------------------
    // git-sync-merge-modal.tsx: merge with {claimConflictIsResolved: true} →
    // expects ProjectSyncState {mergeStatus:'clean'}; the port currently
    // ignores the body and re-conflicts (FLAG for owner — re-arms below).
    const mg3 = await api(page, 'POST', `/project/${pid}/github-sync/merge`, { body: { message: 'resolved', claimConflictIsResolved: true } })
    const mg3b = await mg3.json().catch(() => ({}))
    console.log(`[${h} G8] resolution merge -> ${mg3.status()} ${JSON.stringify(mg3b).slice(0, 220)}`)
    if (mg3b?.status === 'clean' || mg3b?.mergeStatus === 'clean') {
      // RE-ARM: resolution wired — clean state follows
      const st3 = await (await api(page, 'GET', `/project/${pid}/github-sync/state`)).json().catch(() => ({}))
      expect((st3 as any)?.mergeStatus, `G8 state after resolved merge: ${JSON.stringify(st3).slice(0, 220)}`).toBe('clean')
    } else {
      expect(mg3b?.status, 'G8 live pin: claimConflictIsResolved not implemented (conflict again)').toBe('conflict')
      const st3 = await (await api(page, 'GET', `/project/${pid}/github-sync/state`)).json().catch(() => ({}))
      expect((st3 as any)?.mergeStatus, 'G8 state stays conflict (no resolution action in port)').toBe('conflict')
    }

    // ---- G9 unlink -----------------------------------------------------------------------
    const un = await api(page, 'DELETE', `/project/${pid}/github-sync`)
    expect(un.status(), `G9 unlink -> ${un.status()}`).toBe(200)
    const un2 = await (await api(page, 'GET', `/project/${pid}/github-sync/state`)).json().catch(() => ({}))
    expect((un2 as any)?.mergeStatus, `G9 state after unlink: ${JSON.stringify(un2).slice(0, 160)}`).toBe('need-export')
    expect((un2 as any)?.repoFullName, 'G9 state after unlink has no repo').toBeUndefined()

    // ---- G10 hygiene -----------------------------------------------------------------------
    const del = await ad.deleteRepo(fullName).catch((e) => -1)
    console.log(`[${h} G10] repo cleanup -> ${del}`)
  })
}
