module.exports = [
  {
    name: 'web',
  },
  {
    name: 'clsi',
  },
]
// document-updater, project-history and history-v1 were retired to junk/ in
// S5 (134afebcc0) — their compile steps (echo-only) and pushd targets are
// gone with the trees.

if (require.main === module) {
  for (const service of module.exports) {
    console.log(service.name)
  }
}
