import Settings from '@overleaf/settings'
import logger from '@overleaf/logger'
import TypstRouter from './app/src/TypstRouter.mjs'

/**
 * @import { WebModule } from "../../types/web-module"
 */

/**
 * Typst module (TYPST_INTEGRATION_PLAN §5.2 / §8-P2).
 *
 * Gated by the top-level `Settings.typst.enabled` section (NOT
 * `Settings.features.typst` — Settings.features is a plan->features map
 * iterated by subscription code and must not gain a flat `{enabled}` key).
 *
 * The "Typst project" launchpad menu entry + modal are wired through
 * `Settings.overleafModuleImports` (a build-time babel macro consumed by
 * NewProjectButton.tsx) and are only present when enabled — a module may
 * NOT export `viewIncludes` (Modules.mjs throws).
 *
 * When off, the router is not mounted, so `POST /project/new/typst` returns
 * 404 and no new typst projects can be created (clsi_typst also 501s).
 */
let TypstModule = {}

if (!(Settings.typst && Settings.typst.enabled)) {
  logger.info({}, 'Typst module disabled (Settings.typst.enabled false)')
} else {
  logger.debug({}, 'Enabling Typst module')
  TypstModule = {
    router: TypstRouter,
  }
}

export default TypstModule
