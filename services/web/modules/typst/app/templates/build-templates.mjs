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
  // Owner 2026-09-13: translation of the TeX example project
  // (app/templates/project_files/example-project-sp) — the rich showcase:
  // title/abstract, sections, frog figure, widgets table, lists, math,
  // citations. `frog.jpg` is a binary asset added via addFile (same
  // mechanism as the TeX example project).
  example: [
    { source: 'project_files/example/main.typ', name: 'main.typ' },
    { source: 'project_files/example/sample.bib', name: 'sample.bib' },
    { source: 'project_files/example/frog.jpg', name: 'frog.jpg', binary: true },
  ],
}

/**
 * Render the template file set for `templateId` (unknown id → 'basic').
 * Doc entries return `[{ name, lines }]`; binary assets return
 * `[{ name, filePath }]` (caller uses `addFile`, not `addDoc`). The first
 * entry is always the root doc (`main.typ`). Kept as a pure module (no
 * @overleaf/logger, no env) so the mocha/vitest unit test can import it
 * without the router's dependency chain.
 */
export function buildTemplateFiles(projectName, templateId) {
  const files = TEMPLATES[templateId] ?? TEMPLATES.basic
  return files.map(({ source, name, binary }) => {
    const outputPath = path.join(import.meta.dirname, source)
    if (binary) {
      return { name, filePath: outputPath }
    }
    const output = _.template(fs.readFileSync(outputPath).toString())({
      project_name: projectName || 'My project',
    })
    return { name, lines: output.split('\n') }
  })
}
