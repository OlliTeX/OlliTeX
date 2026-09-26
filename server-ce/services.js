module.exports = [
  {
    name: 'web',
  },
  {
    name: 'document-updater',
  },
  {
    name: 'clsi',
  },
  {
    name: 'project-history',
  },
  {
    name: 'history-v1',
  },
]

if (require.main === module) {
  for (const service of module.exports) {
    console.log(service.name)
  }
}
