/**
 * share-project modal — API contract baseline (editor renovation P0e).
 *
 * Freezes the EXACT endpoint contract the share surface speaks (paths,
 * methods, bodies). The P4 Mantine share modal must issue identical calls —
 * this spec is the mechanical check (the e2e modals spec proves the UI flow).
 *
 * The legacy component spec for this modal lives in the old mocha suite
 * (test/frontend/features/share-project-modal) which this fork's mocha runner
 * no longer executes reliably — so the contract is re-asserted here in the
 * live vitest suite BEFORE renovation can change a thing.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  getSharingLink,
  updateSharingLink,
  sendInviteParams,
  resendInvite,
  revokeInvite,
  updateMember,
  removeMemberFromProject,
  requestAccess,
  declineAccessRequest,
  grantAccessRequest,
  transferProjectOwnership,
  setPublicAccessLevel,
  listProjectMembers,
  listProjectAccessRequests,
  listProjectInvites,
} from '@/features/share-project-modal/utils/api'

const PROJECT_ID = 'p0e123456789012345678901'
const MEMBER_ID = 'm0e123456789012345678901'

let calls: { method: string; url: string; body?: any }[] = []

beforeEach(() => {
  calls = []
  if (typeof window !== 'undefined' && !window.metaAttributesCache?.has('ol-csrfToken')) {
    window.metaAttributesCache?.set('ol-csrfToken', { token: 'test-csrf' })
  }
  globalThis.fetch = vi.fn(
    (input: any, init: any) => {
      const url = typeof input === 'string' ? input : input.url
      const method = (init?.method || 'GET').toUpperCase()
      const body = init?.body ? JSON.parse(String(init.body)) : undefined
      calls.push({ method, url, body })
      const respond = () => ({
        ok: true,
        status: 200,
        statusText: 'OK',
        headers: { get: (k: string) => (k.toLowerCase() === 'content-type' ? 'application/json' : null) },
        json: async () => ({ ok: true }),
        text: async () => '',
      })
      return Promise.resolve(respond())
    }
  ) as unknown as typeof fetch
})

describe('share modal API contract (baseline)', () => {
  it('sharing link: GET/POST /project/:id/sharing-link with the privilege body', async () => {
    await getSharingLink(PROJECT_ID)
    expect(calls.at(-1)).toMatchObject({
      method: 'GET',
      url: `/project/${PROJECT_ID}/sharing-link`,
    })

    await updateSharingLink(PROJECT_ID, { privileges: 'readOnly' })
    expect(calls.at(-1)).toMatchObject({
      method: 'POST',
      url: `/project/${PROJECT_ID}/sharing-link`,
      body: { privileges: 'readOnly' },
    })
  })

  it('invite flow: POST /project/:id/invite {email, privileges}; resend + revoke hit invite/:id', async () => {
    const [path, opts] = sendInviteParams(PROJECT_ID, 'a@b.c', 'readAndWrite')
    expect(path).toBe(`/project/${PROJECT_ID}/invite`)
    expect(opts.body).toEqual({ email: 'a@b.c', privileges: 'readAndWrite' })

    await resendInvite(PROJECT_ID, { _id: 'invite1', email: 'a@b.c' } as any)
    expect(calls.at(-1)).toMatchObject({
      method: 'POST',
      url: `/project/${PROJECT_ID}/invite/invite1/resend`,
    })

    await revokeInvite(PROJECT_ID, { _id: 'invite1', email: 'a@b.c' } as any)
    expect(calls.at(-1)).toMatchObject({
      method: 'DELETE',
      url: `/project/${PROJECT_ID}/invite/invite1`,
    })
  })

  it('member management: PUT member privilege, DELETE member', async () => {
    await updateMember(PROJECT_ID, { _id: MEMBER_ID } as any, {
      privilegeLevel: 'readOnly',
    })
    expect(calls.at(-1)).toMatchObject({
      method: 'PUT',
      url: `/project/${PROJECT_ID}/users/${MEMBER_ID}`,
      body: { privilegeLevel: 'readOnly' },
    })

    await removeMemberFromProject(PROJECT_ID, { _id: MEMBER_ID } as any)
    expect(calls.at(-1)).toMatchObject({
      method: 'DELETE',
      url: `/project/${PROJECT_ID}/users/${MEMBER_ID}`,
    })
  })

  it('access requests: request / grant / decline round-trips', async () => {
    await requestAccess(PROJECT_ID, 'readAndWrite')
    expect(calls.at(-1)).toMatchObject({
      method: 'POST',
      url: `/project/${PROJECT_ID}/request-access`,
      body: { privilegeLevel: 'readAndWrite' },
    })

    await grantAccessRequest(PROJECT_ID, MEMBER_ID, 'readOnly', true)
    expect(calls.at(-1)).toMatchObject({
      method: 'POST',
      url: `/project/${PROJECT_ID}/access-requests/${MEMBER_ID}/grant`,
      body: { privilegeLevel: 'readOnly', notify: true },
    })

    await declineAccessRequest(PROJECT_ID, MEMBER_ID, false)
    expect(calls.at(-1)).toMatchObject({
      method: 'DELETE',
      url: `/project/${PROJECT_ID}/access-requests/${MEMBER_ID}`,
      body: { notify: false },
    })
  })

  it('ownership + public access + lists', async () => {
    await transferProjectOwnership(PROJECT_ID, { _id: MEMBER_ID } as any)
    expect(calls.at(-1)).toMatchObject({
      method: 'POST',
      url: `/project/${PROJECT_ID}/transfer-ownership`,
      body: { user_id: MEMBER_ID },
    })

    await setPublicAccessLevel(PROJECT_ID, 'private')
    expect(calls.at(-1)).toMatchObject({
      method: 'POST',
      url: `/project/${PROJECT_ID}/settings/admin`,
      body: { publicAccessLevel: 'private' },
    })

    await listProjectMembers(PROJECT_ID)
    expect(calls.at(-1)).toMatchObject({
      method: 'GET',
      url: `/project/${PROJECT_ID}/members`,
    })

    await listProjectAccessRequests(PROJECT_ID)
    expect(calls.at(-1)).toMatchObject({
      method: 'GET',
      url: `/project/${PROJECT_ID}/access-requests`,
    })

    await listProjectInvites(PROJECT_ID)
    expect(calls.at(-1)).toMatchObject({
      method: 'GET',
      url: `/project/${PROJECT_ID}/invites`,
    })
  })
})
