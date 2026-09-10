import fs from 'node:fs'
import path from 'node:path'
import _ from 'lodash'

// F2.5 (basic) + F3.8 (article): project-file sets for the new-project modal.
//
//   basic   (default) — the empty-ish starter: on-disk `mainbasic.typ`
//                       (F2.5), created in the project as `main.typ`
//                       (original F2.5 behavior, unchanged).
//   article           — `project_files/article/main.typ` + `references.bib`
//                       (F3.8, plan §11 "empty + one article").
//
// `project_name` is substituted via lodash `.template()` (same convention as
// the original `_buildTypstTemplate`). The first file is always the root doc
// (`main.typ`). Kept as a pure module (no @overleaf/logger, no env) so the
// mocha/vitest unit test can import it without the router's dependency chain.
const TEMPLATES = {
  basic: [{ source: 'project_files/mainbasic.typ', name: 'main.typ' }],
  article: [
    { source: 'project_files/article/main.typ', name: 'main.typ' },
    { source: 'project_files/article/references.bib', name: 'references.bib' },
  ],
}

/**
 * Render the template file set for `templateId` (unknown id → 'basic').
 * Returns `[{ name, lines }]`, `name` = project-internal filename,
 * `lines` = `project_name`-substituted content split for `addDoc`.
 */
export function buildTemplateFiles(projectName, templateId) {
  const files = TEMPLATES[templateId] ?? TEMPLATES.basic
  return files.map(({ source, name }) => {
    const outputPath = path.join(import.meta.dirname, source)
    const output = _.template(fs.readFileSync(outputPath).toString())({
      project_name: projectName || 'My project',
    })
    return { name, lines: output.split('\n') }
  })
}
