import { describe, it, expect } from 'vitest'
import {
  detectMessageSurface,
  messageVisibleForSurface,
  normalizePlacementsChecked,
} from '../../../../frontend/js/shared/components/system-message-surface'

describe('system message placement (#17b)', () => {
  describe('detectMessageSurface', () => {
    it('classifies auth pages', () => {
      expect(detectMessageSurface('/login')).toBe('auth')
      expect(detectMessageSurface('/register')).toBe('auth')
      expect(detectMessageSurface('/user/reset-password')).toBe('auth')
      expect(detectMessageSurface('/user/reset-password/abc123')).toBe('auth')
      expect(detectMessageSurface('/user/forgot-password')).toBe('auth')
    })

    it('classifies the editor (IDE) by project id', () => {
      expect(detectMessageSurface('/editor/6a900f391f82ca1771fbc873')).toBe('editor')
      expect(detectMessageSurface('/project/6aa269b259e66f7669428de2')).toBe('editor')
      // the project LIST is an 'app' page, not the editor
      expect(detectMessageSurface('/project')).toBe('app')
    })

    it('classifies /hub (workspace + admin subroutes)', () => {
      expect(detectMessageSurface('/hub')).toBe('hub')
      expect(detectMessageSurface('/hub/admin')).toBe('hub')
      expect(detectMessageSurface('/hub/workspace/projects')).toBe('hub')
    })

    it('classifies everything else as app', () => {
      expect(detectMessageSurface('/user/settings')).toBe('app')
      expect(detectMessageSurface('/library/6aa269b259e66f7669428de2')).toBe('app')
      expect(detectMessageSurface('/')).toBe('app')
    })
  })

  describe('messageVisibleForSurface', () => {
    const all: any = { placements: [] }
    const none: any = { placements: undefined }
    const editorOnly: any = { placements: ['editor'] }
    const hubAuth: any = { placements: ['hub', 'auth'] }

    it('legacy messages (missing/empty placements) are visible everywhere', () => {
      for (const m of [all, none]) {
        for (const s of ['editor', 'hub', 'auth', 'app'] as const) {
          expect(messageVisibleForSurface(m, s)).toBe(true)
        }
      }
    })

    it('scoped messages hide on non-listed surfaces', () => {
      expect(messageVisibleForSurface(editorOnly, 'editor')).toBe(true)
      expect(messageVisibleForSurface(editorOnly, 'hub')).toBe(false)
      expect(messageVisibleForSurface(editorOnly, 'auth')).toBe(false)
      expect(messageVisibleForSurface(editorOnly, 'app')).toBe(false)
    })

    it('supports multi-surface placement', () => {
      expect(messageVisibleForSurface(hubAuth, 'hub')).toBe(true)
      expect(messageVisibleForSurface(hubAuth, 'auth')).toBe(true)
      expect(messageVisibleForSurface(hubAuth, 'editor')).toBe(false)
    })
  })

  describe('normalizePlacementsChecked (all-exclusive)', () => {
    it('all=true wins → empty placement list', () => {
      expect(normalizePlacementsChecked({ all: true, editor: true })).toEqual({
        all: true,
        placements: [],
      })
    })

    it('keeps the selected surfaces', () => {
      expect(
        normalizePlacementsChecked({ all: false, editor: true, hub: false, auth: true })
      ).toEqual({ all: false, placements: ['editor', 'auth'] })
    })
  })
})
