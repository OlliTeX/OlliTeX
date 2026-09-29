// Word count — server (texcount) renderer, tested in isolation.
//
// 2026-09 (OlliTeX, File → Word count scoping): the File → Word count modal
// now ALWAYS shows the CURRENT file (the one open in the editor), computed
// client-side (see WordCountClient + countWordsInFile with includeIncludedFiles
// = false). The previous default rendered the project-wide texcount figure,
// which counted the whole project regardless of which file was open — the
// owner wanted the current file instead.
//
// WordCountServer (and its WordCounts renderer) is no longer wired into the
// modal, but it remains a usable component. This spec keeps it covered by
// mounting it directly with the endpoint intercepted (same provider tree as
// before — EditorProviders/ReactContextRoot supplies LocalCompileProvider).
import { WordCountServer } from '@/features/word-count-modal/components/word-count-server'
import { EditorProviders } from '../../../helpers/editor-providers'

describe('<WordCountServer />', function () {
  beforeEach(function () {
    cy.interceptCompile()
  })

  it('renders the translated modal title', function () {
    cy.intercept('/project/*/wordcount*', {
      body: { texcount: { messages: 'This is a test' } },
    })

    cy.mount(
      <EditorProviders projectId="project-1">
        <WordCountServer />
      </EditorProviders>,
    )

    cy.findByText(/something went wrong/).should('not.exist')
  })

  it('renders a loading message when loading', function () {
    const { promise, resolve } = Promise.withResolvers<void>()

    cy.intercept('/project/*/wordcount*', async req => {
      await promise
      req.reply({ texcount: { messages: 'This is a test' } })
    })

    cy.mount(
      <EditorProviders projectId="project-1">
        <WordCountServer />
      </EditorProviders>,
    )

    cy.findByText('Loading…').then(() => {
      resolve()
    })

    cy.findByText('This is a test')
  })

  it('renders an error message and hides loading message on error', function () {
    cy.intercept('/project/*/wordcount?*', {
      statusCode: 500,
    })

    cy.mount(
      <EditorProviders projectId="project-1">
        <WordCountServer />
      </EditorProviders>,
    )

    cy.findByText('Sorry, something went wrong')

    cy.findByText('Loading').should('not.exist')
  })

  it('displays messages', function () {
    cy.intercept('/project/*/wordcount*', {
      body: { texcount: { messages: 'This is a test' } },
    })

    cy.mount(
      <EditorProviders projectId="project-1">
        <WordCountServer />
      </EditorProviders>,
    )

    cy.findByText('This is a test')
  })

  it('displays counts data', function () {
    cy.intercept('/project/*/wordcount*', {
      body: {
        texcount: {
          textWords: 500,
          headWords: 100,
          outside: 200,
          mathDisplay: 2,
          mathInline: 3,
          headers: 4,
        },
      },
    })

    cy.mount(
      <EditorProviders projectId="project-1">
        <WordCountServer />
      </EditorProviders>,
    )

    cy.findByText((content, element) => {
      return /^Total Words\s*:\s*500$/.test(element!.textContent!.trim())
    })

    cy.findByText((content, element) => {
      return /^Math Display\s*:\s*2$/.test(element!.textContent!.trim())
    })

    cy.findByText((content, element) => {
      return /^Math Inline\s*:\s*3$/.test(element!.textContent!.trim())
    })

    cy.findByText((content, element) => {
      return /^Headers\s*:\s*4$/.test(element!.textContent!.trim())
    })
  })
})
