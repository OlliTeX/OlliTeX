import { screen, within, render } from '@testing-library/react'
import { expect } from 'chai'
import fetchMock from 'fetch-mock'
import { SettingsModalProvider } from '@/features/ide-settings/context/settings-modal-context'
import {
  EditorProviders,
  projectDefaults,
} from '../../../helpers/editor-providers'
import CompilerSetting from '@/features/ide-settings/components/compiler-settings/compiler-setting'
import userEvent from '@testing-library/user-event'
import getMeta from '@/utils/meta'

const OPTIONS = [
  {
    label: 'pdfLaTeX',
    value: 'pdflatex',
  },
  {
    label: 'LaTeX',
    value: 'latex',
  },
  {
    label: 'XeLaTeX',
    value: 'xelatex',
  },
  {
    label: 'LuaLaTeX',
    value: 'lualatex',
  },
]

const exposedSettings = () => getMeta('ol-ExposedSettings')

describe('<CompilerSetting />', function () {
  afterEach(function () {
    fetchMock.removeRoutes().clearHistory()
    exposedSettings().typstEnabled = undefined
  })

  it('each option is shown and can be selected', async function () {
    render(
      <EditorProviders>
        <SettingsModalProvider>
          <CompilerSetting />
        </SettingsModalProvider>
      </EditorProviders>
    )

    const saveSettingsMock = fetchMock.post(
      `express:/project/:projectId/settings`,
      {
        status: 200,
      },
      { delay: 0 }
    )

    const select = screen.getByLabelText('Compiler')

    // Reverse order so we test changing to each option
    for (const option of OPTIONS.reverse()) {
      const optionElement = within(select).getByText(option.label)
      expect(optionElement.getAttribute('value')).to.equal(option.value)
      await userEvent.selectOptions(select, [optionElement])

      expect(
        saveSettingsMock.callHistory.called(
          `/project/${projectDefaults._id}/settings`,
          {
            body: { compiler: option.value },
          }
        )
      ).to.be.true
    }
  })

  describe('Typst option (gated by ExposedSettings.typstEnabled)', function () {
    it('hides the Typst option when the flag is off (default behaviour)', function () {
      exposedSettings().typstEnabled = false
      const testRender = render(
        <EditorProviders>
          <SettingsModalProvider>
            <CompilerSetting />
          </SettingsModalProvider>
        </EditorProviders>
      )

      const select = screen.getByLabelText('Compiler')
      const values = within(select)
        .queryAllByRole('option')
        .map((el) => el.getAttribute('value'))
      expect(values).to.deep.equal(['pdflatex', 'latex', 'xelatex', 'lualatex'])
      testRender.unmount()
    })

    it('shows the Typst option when the flag is on', function () {
      exposedSettings().typstEnabled = true
      const testRender = render(
        <EditorProviders>
          <SettingsModalProvider>
            <CompilerSetting />
          </SettingsModalProvider>
        </EditorProviders>
      )

      const select = screen.getByLabelText('Compiler')
      const values = within(select)
        .queryAllByRole('option')
        .map((el) => el.getAttribute('value'))
      expect(values).to.deep.equal([
        'pdflatex',
        'latex',
        'xelatex',
        'lualatex',
        'typst',
      ])
      testRender.unmount()
    })
  })
})
