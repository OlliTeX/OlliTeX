import {
  createContext,
  FC,
  useCallback,
  useContext,
  useMemo,
  useState,
} from 'react'
import { useLayoutContext } from '@/shared/context/layout-context'
import AutoCloseBracketsSetting from '@/features/ide-settings/components/editor-settings/auto-close-brackets-setting'
import AutoCompleteSetting from '@/features/ide-settings/components/editor-settings/auto-complete-setting'
import CodeCheckSetting from '@/features/ide-settings/components/editor-settings/code-check-setting'
import PreviewTabsSetting from '@/features/ide-settings/components/editor-settings/preview-tabs-setting'
import KeybindingSetting from '@/features/ide-settings/components/editor-settings/keybinding-setting'
import PDFViewerSetting from '@/features/ide-settings/components/editor-settings/pdf-viewer-setting'
import importOverleafModules from '../../../../macros/import-overleaf-module.macro'
import SpellCheckSetting from '@/features/ide-settings/components/editor-settings/spell-check-setting'
import DictionarySetting from '@/features/ide-settings/components/editor-settings/dictionary-setting'
// overleaf-lab (grammar port): per-project LanguageTool pickiness toggle
import GrammarPickySetting from '@/features/settings/components/editor-settings/grammar-picky-setting'
import { useTranslation } from 'react-i18next'
import BreadcrumbsSetting from '@/features/ide-settings/components/editor-settings/breadcrumbs-setting'
import NonBlinkingCursorSetting from '@/features/ide-settings/components/editor-settings/non-blinking-cursor-setting'
import MathPreviewSetting from '@/features/ide-settings/components/editor-settings/math-preview-setting'
import RootDocumentSetting from '@/features/ide-settings/components/compiler-settings/root-document-setting'
import CompilerSetting from '@/features/ide-settings/components/compiler-settings/compiler-setting'
import ImageNameSetting from '@/features/ide-settings/components/compiler-settings/image-name-setting'
import DraftSetting from '@/features/ide-settings/components/compiler-settings/draft-setting'
import StopOnFirstErrorSetting from '@/features/ide-settings/components/compiler-settings/stop-on-first-error-setting'
import AutoCompileSetting from '@/features/ide-settings/components/compiler-settings/auto-compile-setting'
import OverallThemeSetting from '@/features/ide-settings/components/appearance-settings/overall-theme-setting'
import EditorThemeSetting from '@/features/ide-settings/components/appearance-settings/editor-theme-setting'
import FontSizeSetting from '@/features/ide-settings/components/appearance-settings/font-size-setting'
import LineHeightSetting from '@/features/ide-settings/components/appearance-settings/line-height-setting'
import FontFamilySetting from '@/features/ide-settings/components/appearance-settings/font-family-setting'
import DarkModePdfSetting from '@/features/ide-settings/components/appearance-settings/dark-mode-pdf-setting'
import RootBibDocumentSetting from '../components/reference-settings/root-bib-document-setting'
import ReferenceFormatSetting from '../components/reference-settings/reference-format-setting'

import { useProjectSettingsContext } from '@/features/ide-settings/context/project-settings-context'
import { useFeatureFlag } from '@/shared/context/split-test-context'
import ProjectNotificationsSetting from '@/features/ide-settings/components/editor-settings/project-notifications-setting'
import getMeta from '@/utils/meta'
import type {
  SettingsEntry,
  SettingsSection,
  SettingsSectionHook,
} from '@/features/ide-settings/context/types'
import EditorTabsSetting from '../components/editor-settings/editor-tabs-setting'
import FloatingMenuSetting from '../components/editor-settings/floating-menu-setting'
import useEventListener from '@/shared/hooks/use-event-listener'
import OptimizeCompileSetting from '../components/compiler-settings/optimize-compile-setting'

const [referenceSearchSettingModule] = importOverleafModules(
  'referenceSearchSetting'
)
const ReferenceSearchSetting = referenceSearchSettingModule?.import.default

const editorTabExtraSectionHooks: SettingsSectionHook[] = importOverleafModules(
  'settingsModalEditorTabSections'
)
  .map((m: any) => m?.import?.default)
  .filter((h: unknown): h is SettingsSectionHook => typeof h === 'function')

const spellcheckExtraSectionHooks: SettingsSectionHook[] =
  importOverleafModules('settingsModalSpellcheckSections')
    .map((m: any) => m?.import?.default)
    .filter((h: unknown): h is SettingsSectionHook => typeof h === 'function')

const useSlotSections = (hooks: SettingsSectionHook[]): SettingsSection[] =>
  hooks.map(hook => hook()).filter((s): s is SettingsSection => s != null)

type SettingsModalState = {
  show: boolean
  setShow: (shown: boolean) => void
  activeTab: string | null | undefined
  setActiveTab: (tab: string | null | undefined) => void
  settingsTabs: SettingsEntry[]
  settingToTabMap: Map<string, string>
  settingToFocus?: string
}

export const SettingsModalContext = createContext<
  SettingsModalState | undefined
>(undefined)

export const SettingsModalProvider: FC<React.PropsWithChildren> = ({
  children,
}) => {
  const { t } = useTranslation()
  const { isOverleaf } = getMeta('ol-ExposedSettings')
  const { overallTheme, floatingMenu, compiler } = useProjectSettingsContext()
  // Typst projects: hide TeX Live-only compile settings (fork parity F2.11)
  const isTypst = compiler === 'typst'
  const [settingToFocus, setSettingToFocus] = useState<string | undefined>(
    undefined
  )

  useEventListener(
    'ui.focus-setting',
    useCallback((event: CustomEvent<string | undefined>) => {
      setSettingToFocus(event.detail)
    }, [])
  )

  const { settingsShown, setSettingsShown } = useLayoutContext()

  const hasEmailNotifications = useFeatureFlag('email-notifications')
  const hasToolbarMigration = useFeatureFlag('writefull-toolbar-migration')

  const editorTabExtraSections = useSlotSections(editorTabExtraSectionHooks)
  const spellcheckExtraSections = useSlotSections(spellcheckExtraSectionHooks)

  const allSettingsTabs: SettingsEntry[] = useMemo(
    () => [
      {
        key: 'editor',
        title: t('editor'),
        icon: 'code',
        sections: [
          {
            key: 'general',
            settings: [
              {
                key: 'autoComplete',
                component: <AutoCompleteSetting />,
              },
              {
                key: 'autoPairDelimiters',
                component: <AutoCloseBracketsSetting />,
              },
              {
                key: 'nonBlinkingCursor',
                component: <NonBlinkingCursorSetting />,
              },
              {
                key: 'syntaxValidation',
                component: <CodeCheckSetting />,
              },
              {
                key: 'editorTabs',
                component: <EditorTabsSetting />,
              },
              {
                key: 'previewTabs',
                component: <PreviewTabsSetting />,
              },
              {
                key: 'mode',
                component: <KeybindingSetting />,
              },
              {
                key: 'pdfViewer',
                component: <PDFViewerSetting />,
              },
              {
                key: 'floating-menu',
                component: <FloatingMenuSetting />,
                hidden: !hasToolbarMigration && floatingMenu,
              },
            ],
          },
          {
            key: 'tools',
            title: t('tools'),
            settings: [
              {
                key: 'breadcrumbs-setting',
                component: <BreadcrumbsSetting />,
              },
              {
                key: 'mathPreview',
                component: <MathPreviewSetting />,
              },
            ],
          },
          ...editorTabExtraSections,
        ],
      },
      {
        key: 'spelling_and_language',
        title: t('spelling_and_language'),
        icon: 'spellcheck',
        sections: [
          {
            key: 'spellcheck',
            title: t('spellcheck'),
            settings: [
              {
                key: 'spellCheckLanguage',
                component: <SpellCheckSetting />,
              },
              // overleaf-lab (grammar port): per-project LanguageTool pickiness
              // (ON by default; turns off the aggressive style rules).
              {
                key: 'grammarPicky',
                component: <GrammarPickySetting />,
              },
              {
                key: 'dictionary-settings',
                component: <DictionarySetting />,
              },
            ],
          },
          ...spellcheckExtraSections,
        ],
      },
      {
        key: 'compiler',
        title: t('compiler'),
        icon: 'picture_as_pdf',
        sections: [
          {
            key: 'general',
            settings: [
              {
                key: 'rootDocId',
                component: <RootDocumentSetting />,
              },
              {
                key: 'compiler',
                component: <CompilerSetting />,
              },
              {
                key: 'imageName',
                component: <ImageNameSetting />,
                hidden: isTypst,
              },
              {
                key: 'draft',
                component: <DraftSetting />,
                hidden: isTypst,
              },
              {
                key: 'stopOnFirstError',
                component: <StopOnFirstErrorSetting />,
                hidden: isTypst,
              },
              {
                key: 'autoCompile',
                component: <AutoCompileSetting />,
              },
              {
                key: 'optimizeCompiles',
                component: <OptimizeCompileSetting />,
                hidden: isTypst,
              },
            ],
          },
        ],
      },
      {
        key: 'references',
        title: t('references'),
        icon: 'book_5',
        sections: [
          {
            key: 'general',
            settings: [
              {
                key: 'mainBibliographyDocId',
                component: <RootBibDocumentSetting />,
              },
              {
                key: 'referenceFormat',
                component: <ReferenceFormatSetting />,
                hidden: true, // Hide this setting for now, as it is not yet implemented its usage
              },
              {
                key: 'write-and-cite-settings',
                component: <ReferenceSearchSetting />,
                hidden: !ReferenceSearchSetting,
              },
            ],
          },
        ],
      },
      {
        key: 'appearance',
        title: t('appearance'),
        icon: 'brush',
        sections: [
          {
            key: 'general',
            settings: [
              {
                key: 'overallTheme',
                component: <OverallThemeSetting />,
              },
              {
                key: 'editorTheme',
                component: <EditorThemeSetting />,
              },
              {
                key: 'pdfDarkMode',
                component: <DarkModePdfSetting />,
                hidden: overallTheme === 'light-',
              },
              {
                key: 'fontSize',
                component: <FontSizeSetting />,
              },
              {
                key: 'fontFamily',
                component: <FontFamilySetting />,
              },
              {
                key: 'lineHeight',
                component: <LineHeightSetting />,
              },
            ],
          },
        ],
      },

      {
        key: 'project_notifications',
        title: t('project_notifications'),
        icon: 'notifications' as const,
        sections: [
          {
            key: 'general',
            settings: [
              {
                key: 'projectNotifications',
                component: <ProjectNotificationsSetting />,
              },
            ],
          },
        ],
        hidden: false // [IV] CE: un-gated (upstream: hidden: !hasEmailNotifications, i.e. the saas 'email-notifications' split-test flag); the CE backend exists, so the tab is always available,
      },

      {
        key: 'account_settings',
        title: t('account_settings'),
        icon: 'settings',
        // 2026-09 owner decision: the canonical account-settings URL on this
        // deployment is /user/mysettings (our own shell page).
        href: '/user/mysettings',
      },
    ],
    [
      t,
      overallTheme,
      hasEmailNotifications,
      isOverleaf,
      editorTabExtraSections,
      spellcheckExtraSections,
      hasToolbarMigration,
      floatingMenu,
      isTypst,
    ]
  )

  const settingsTabs = useMemo(
    () => allSettingsTabs.filter(tab => !tab.hidden),
    [allSettingsTabs]
  )

  const settingToTabMap = useMemo(() => {
    const map = new Map<string, string>()
    settingsTabs
      .filter(t => 'sections' in t)
      .forEach(tab => {
        tab.sections.forEach(section => {
          section.settings.forEach(setting => {
            map.set(setting.key, tab.key)
          })
        })
      })
    return map
  }, [settingsTabs])

  const [activeTab, setActiveTab] = useState<string | null | undefined>(
    settingsTabs[0]?.key
  )

  const value = useMemo(
    () => ({
      show: settingsShown,
      setShow: setSettingsShown,
      activeTab,
      setActiveTab,
      settingsTabs,
      settingToTabMap,
      settingToFocus,
    }),
    [
      settingsShown,
      setSettingsShown,
      activeTab,
      setActiveTab,
      settingsTabs,
      settingToTabMap,
      settingToFocus,
    ]
  )

  return (
    <SettingsModalContext.Provider value={value}>
      {children}
    </SettingsModalContext.Provider>
  )
}

export const useSettingsModalContext = () => {
  const value = useContext(SettingsModalContext)

  if (!value) {
    throw new Error(
      `useSettingsModalContext is only available inside SettingsModalProvider`
    )
  }

  return value
}
