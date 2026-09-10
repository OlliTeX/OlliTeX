import { useActiveEditorTheme } from '@/shared/hooks/use-active-editor-theme'
import { EditorProviders } from '../../helpers/editor-providers'
import { SplitTestProvider } from '@/shared/context/split-test-context'

const TestComponent = ({
  userSettings,
}: {
  userSettings: Record<string, string>
}) => {
  return (
    <SplitTestProvider>
      <EditorProviders
        userSettings={{
          ...userSettings,
        }}
      >
        <TestComponentInner />
      </EditorProviders>
    </SplitTestProvider>
  )
}

const TestComponentInner = () => {
  const editorTheme = useActiveEditorTheme()
  return <div data-testid="editor-theme">{editorTheme}</div>
}

// Owner 2026-09-13 editor wave (#7/#15): the editor code theme must follow
// the active overall theme — dark UI → editorDarkTheme, light UI →
// editorLightTheme; the legacy single `editorTheme` is only a fallback
// when the paired settings are empty.
describe('useActiveEditorTheme', function () {
  describe('when overall theme is specific mode', function () {
    it('uses editorDarkTheme when in dark mode', function () {
      cy.mount(
        <TestComponent
          userSettings={{
            overallTheme: '',
            editorTheme: 'default-theme',
            editorLightTheme: 'light-theme',
            editorDarkTheme: 'dark-theme',
          }}
        />
      )
      cy.findByTestId('editor-theme').should('have.text', 'dark-theme')
    })

    it('uses editorLightTheme when in light mode', function () {
      cy.mount(
        <TestComponent
          userSettings={{
            overallTheme: 'light-',
            editorTheme: 'default-theme',
            editorLightTheme: 'light-theme',
            editorDarkTheme: 'dark-theme',
          }}
        />
      )
      cy.findByTestId('editor-theme').should('have.text', 'light-theme')
    })
  })

  describe('when overall theme is system', function () {
    function stubMediaQuery(prefersDark: boolean) {
      cy.window().then(win => {
        cy.stub(win, 'matchMedia')
          .withArgs('(prefers-color-scheme: dark)')
          .returns({
            matches: prefersDark,
            addEventListener: () => {},
            removeEventListener: () => {},
          } as any)
      })
    }

    it('uses editorDarkTheme when OS is dark', function () {
      stubMediaQuery(true)
      cy.mount(
        <TestComponent
          userSettings={{
            overallTheme: 'system',
            editorTheme: 'default-theme',
            editorLightTheme: 'light-theme',
            editorDarkTheme: 'dark-theme',
          }}
        />
      )
      cy.findByTestId('editor-theme').should('have.text', 'dark-theme')
    })

    it('uses editorLightTheme when OS is light', function () {
      stubMediaQuery(false)
      cy.mount(
        <TestComponent
          userSettings={{
            overallTheme: 'system',
            editorTheme: 'default-theme',
            editorLightTheme: 'light-theme',
            editorDarkTheme: 'dark-theme',
          }}
        />
      )
      cy.findByTestId('editor-theme').should('have.text', 'light-theme')
    })
  })

  describe('fallback to legacy editorTheme', function () {
    it('uses editorTheme when the paired settings are empty (dark)', function () {
      cy.mount(
        <TestComponent
          userSettings={{
            overallTheme: '',
            editorTheme: 'default-theme',
            editorLightTheme: '',
            editorDarkTheme: '',
          }}
        />
      )
      cy.findByTestId('editor-theme').should('have.text', 'default-theme')
    })

    it('uses editorTheme when the paired settings are empty (light)', function () {
      cy.mount(
        <TestComponent
          userSettings={{
            overallTheme: 'light-',
            editorTheme: 'default-theme',
            editorLightTheme: '',
            editorDarkTheme: '',
          }}
        />
      )
      cy.findByTestId('editor-theme').should('have.text', 'default-theme')
    })
  })
})
