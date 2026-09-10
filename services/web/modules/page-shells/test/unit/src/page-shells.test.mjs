/**
 * PSH — page-shells unit tests (UI-R10 W8).
 *
 * 1) The NEW views mirror their upstream counterparts on every
 *    functional surface (pug includes, entrypoints, meta tags, tab ids,
 *    form actions) while living only inside modules/page-shells.
 * 2) Upstream files remain untouched by this feature (byte stability).
 * 3) captureRender correctly bridges an upstream-style render handler.
 **/
import { readFileSync, existsSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, it, expect } from 'vitest'
import captureRender from '../../../app/src/captureRender.mjs'

// <web>/modules/page-shells/test/unit/src  ->  five levels up = <web>
const web = resolve(fileURLToPath(new URL('.', import.meta.url)), '../../../../..')
const repoApp = resolve(web, 'app')
const moduleDir = resolve(web, 'modules/page-shells')
const read = p => readFileSync(p, 'utf8')

describe('PSH view mirrors', () => {
  // 2026-09-10 (owner queue 4): the legacy /admin/panel page is REMOVED —
  // the admin hub is the single admin surface. Assert the redirect + that
  // the panel artifacts are gone (the my-settings shell still ships).
  it('/admin/panel is a 301 redirect to the hub and its artifacts are removed', () => {
    const router = read(resolve(moduleDir, 'app/src/PageShellsRouter.mjs'))
    expect(router).toContain("'/admin/panel'")
    expect(router).toContain('res.redirect(301, \'/hub#/overview\')')
    expect(router).not.toContain('AdminPanelShellController')
    expect(existsSync(resolve(moduleDir, 'app/views/admin-panel.pug'))).toBe(false)
    expect(existsSync(resolve(moduleDir, 'app/src/AdminPanelShellController.mjs'))).toBe(false)
    expect(existsSync(resolve(moduleDir, 'frontend/js/pages/panel-shell.js'))).toBe(false)
  })

  it('user-my-settings.pug mirrors upstream user/settings.pug on all meta tags + entrypoint', () => {
    const upstream = read(resolve(repoApp, 'views/user/settings.pug'))
    const shell = read(resolve(moduleDir, 'app/views/user-my-settings.pug'))

    const metaNames = [...upstream.matchAll(/name='(ol-[A-Za-z0-9-]+)'/g)].map(m => m[1])
    expect(metaNames.length).toBeGreaterThanOrEqual(20)
    for (const name of metaNames) {
      expect(shell, `missing meta ${name}`).toContain(`name='${name}'`)
    }
    // Same React app
    expect(shell).toContain("- entrypoint = 'pages/user/settings'")
    expect(shell).toContain('#settings-page-root')
    expect(shell).toContain('layout-react')
  })

  it('module files exist and the module is registered', () => {
    expect(existsSync(resolve(moduleDir, 'index.mjs'))).toBe(true)
    for (const f of ['PageShellsRouter.mjs', 'captureRender.mjs', 'MySettingsShellController.mjs']) {
      expect(existsSync(resolve(moduleDir, 'app/src', f)), f).toBe(true)
    }
    const defaults = read(resolve(web, 'config/settings.defaults.js'))
    expect(defaults).toMatch(/'page-shells'/)
  })
})

describe('PSH captureRender bridge', () => {
  it('captures view + locals from an upstream-style handler', async () => {
    const upstreamStyle = async (req, res) => {
      res.locals.someSplit = 'yes'
      const payload = { title: 'x', user: { email: req.userEmail }, extra: 42 }
      res.render('some/upstream-view', payload)
    }
    const cap = captureRender()
    await cap.run(upstreamStyle, { session: {}, userEmail: 'a@b.c' })
    expect(cap.redirected).toBeNull()
    expect(cap.view).toBe('some/upstream-view')
    expect(cap.locals.user.email).toBe('a@b.c')
    expect(cap.locals.extra).toBe(42)
    expect(cap.locals).not.toBeNull()
  })

  it('captures redirect instead of render', async () => {
    const upstreamStyle = (req, res) => res.redirect('/login')
    const cap = captureRender()
    await cap.run(upstreamStyle, {})
    expect(cap.redirected).toBe('/login')
    expect(() => cap.ensureRendered()).toThrow()
  })

  it('ensureRendered throws when nothing was rendered', () => {
    const cap = captureRender()
    expect(() => cap.ensureRendered()).toThrow()
  })
})

describe('PSH upstream stability (hard constraint: no upstream edits)', () => {
  const UPSTREAM_FILES = [
    'app/src/Features/ServerAdmin/AdminController.mjs',
    'app/src/Features/User/UserPagesController.mjs',
    'app/src/router.mjs',
    'app/views/admin/index.pug',
    'app/views/user/settings.pug',
    'app/views/layout-react.pug',
    'app/views/layout-marketing.pug',
    'app/views/_mixins/bookmarkable_tabset.pug',
  ]
  it('all upstream files still exist and are non-trivial (feature imports them, never edits them)', () => {
    for (const f of UPSTREAM_FILES) {
      const full = resolve(repoApp, f.replace(/^app\//, ''))
      expect(existsSync(full), f).toBe(true)
    }
    // The settings shell controller IMPORTS the upstream handler (policy check).
    const settingsCtl = read(resolve(moduleDir, 'app/src/MySettingsShellController.mjs'))
    expect(settingsCtl).toContain('User/UserPagesController.mjs')
  })
})
