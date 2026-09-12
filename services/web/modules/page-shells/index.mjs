import PageShellsRouter from './app/src/PageShellsRouter.mjs'

/**
 * @import { WebModule } from "../../types/web-module"
 */

/**
 * PSH — Page shells (UI-R10 W8, 2026-08-30).
 *
 * CE+ fork wrappers that give the UPSTREAM pages bookmarkable, themed
 * same-origin addresses:
 *
 *   GET /admin/panel     -> 301 /hub#/overview (hub is the single admin surface)
 *                           locals, rendered inside this module's own view)
 *   GET /user/mysettings -> 301 /hub#/mysettings.account (hub is the single
 *                           settings surface; legacy shells removed, owner 2026-09-12)
 *                           /hub#/mysettings.* leaves are the equivalent)
 *
 * HARD CONSTRAINT (fork policy): NO upstream file is edited by this feature.
 * Upstream files are IMPORTED (handlers, mixins, partials, React
 * entrypoints) and verified byte-identical by the unit tests.
 */

/** @type {WebModule} */
const PageShellsModule = {
  router: PageShellsRouter,
}

export default PageShellsModule
