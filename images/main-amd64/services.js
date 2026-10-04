// node clsi was retired to the Go service (go/services/clsitex, run via
// runit/clsi-overleaf) 2026-09-29 — the clsi entry (and its pushd build
// step) is gone with the Node tree. document-updater, project-history and
// history-v1 were retired to junk/ in S5 (134afebcc0) — their compile steps
// (echo-only) and pushd targets are gone with the trees.
// 2026-10-05 (owner directive): junk/ is no longer shipped in the image —
// the webpack build chain (webpack.config.*.js, webpack-plugins/,
// app/src/infrastructure/{Views,PackageVersions}.js) was consolidated into
// frontend/, which is now the build host (CWD).
module.exports = [
  {
    name: 'web',
    dir: 'frontend', // build host (webpack chain + babel + macros + infrastructure are CWD-anchored here)
  },
]

if (require.main === module) {
  for (const service of module.exports) {
    console.log(service.name)
  }
}
