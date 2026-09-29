module.exports = [
  {
    name: 'web',
    dir: 'frontend', // consolidated build host (services/web retired in the frontend consolidation reorg)
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
