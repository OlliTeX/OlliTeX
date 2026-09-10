// `buildTemplateFiles` (app/templates/build-templates.mjs) is the one piece of
// the F3.8 router delta that is unit-testable without the session chain; the
// router itself is exercised by the flag-on e2e journey (F5.7) and the
// clsi_typst acceptance suite (F1.19 pattern).
import { buildTemplateFiles } from '../../../app/templates/build-templates.mjs'
import { describe, expect, it } from 'vitest'

describe('modules/typst: new-project templates (F3.8)', function () {
  it('renders the basic template (F2.5) as one main.typ doc (project_name substituted)', function () {
    const result = buildTemplateFiles('My Paper', 'basic')
    expect(result).toEqual([
      { name: 'main.typ', lines: expect.any(Array) },
    ])
    const [mainFile] = result
    // `= <%= project_name %>` → `= My Paper` (lodash `.template` substitution)
    expect(mainFile.lines.some(line => line.includes('= My Paper'))).toBe(true)
    // no leftover template placeholders
    expect(mainFile.lines.join('\n')).not.toContain('<%=')
  })

  it('renders the article template (F3.8 / plan §11) as main.typ + references.bib', function () {
    const result = buildTemplateFiles('My Paper', 'article')
    expect(result.map(f => f.name)).toEqual(['main.typ', 'references.bib'])
    const [mainFile, bibFile] = result
    // F3.8 article is project_name-substituted and the root doc is main.typ
    expect(mainFile.lines.some(line => line.includes('= My Paper'))).toBe(true)
    // references.bib is the `#bibliography` + `~@key` citation source
    expect(bibFile.lines.some(line => line.includes('@article'))).toBe(true)
    // Owner decision (2026-09-10): typst 0.15.1 (digest-pinned image) — the
    // modern citation route works out of the box (live-verified:
    // `#bibliography("references.bib")` + `~@example:2025` → compiled PDF with
    // a References section; the 0.14-era citation-free workaround is retired).
    const mainSrc = mainFile.lines.join('\n')
    expect(mainSrc).toContain('#bibliography("references.bib")')
    expect(mainSrc).toContain('~@example:2025')
  })

  it('falls back to the basic template for an unrecognized id', function () {
    expect(buildTemplateFiles('My Paper', 'bogus')).toEqual(
      buildTemplateFiles('My Paper', 'basic')
    )
  })
})
