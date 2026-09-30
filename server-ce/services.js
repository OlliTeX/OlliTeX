module.exports = [
  {
    name: 'web',
    // Consolidated build host (frontend consolidation reorg): the webpack
    // chain (webpack.config.*.js + webpack-plugins + babel + macros + tsconfig)
    // is self-contained under frontend/; 2026-09-29 bake completed the reorg
    // by landing the missing config files there (they were symlinked from
    // services/web, which docker's build context cannot follow).
    dir: 'services/web', // build host (Settings.js + build macro are CWD-anchored to services/web)
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
