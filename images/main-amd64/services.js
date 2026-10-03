module.exports = [
  {
    name: 'web',
    // Consolidated build host (frontend consolidation reorg): the webpack
    // chain (webpack.config.*.js + webpack-plugins + babel + macros + tsconfig)
    // is self-contained under frontend/; 2026-09-29 bake completed the reorg
    // by landing the missing config files there (they were symlinked from
    // services/web, which docker's build context cannot follow).
    // P7 step 4 (2026-10-03): the Node web app retired to junk/services-web (owner RETIRE
    // decision; see junk/services-web/RETIRED.md). It REMAINS the webpack build host — the
    // webpack config chain (webpack.config.*.js) lives with its package.json there.
    dir: 'junk/services-web', // build host (webpack chain + Settings.js + build macro are CWD-anchored here)
  },
]
// node clsi was retired to the Go service (go/services/clsitex, run via
// runit/clsi-overleaf) 2026-09-29 — the clsi entry (and its pushd build
// step) is gone with the Node tree. document-updater, project-history and
// history-v1 were retired to junk/ in S5 (134afebcc0) — their compile steps
// (echo-only) and pushd targets are gone with the trees.

if (require.main === module) {
  for (const service of module.exports) {
    console.log(service.name)
  }
}
