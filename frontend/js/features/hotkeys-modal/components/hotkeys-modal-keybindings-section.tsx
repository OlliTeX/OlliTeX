import { useTranslation } from 'react-i18next'
import { useUserSettingsContext } from '@/shared/context/user-settings-context'
import { Hotkey } from './hotkey'
import OLRow from '@/shared/components/ol/ol-row'
import OLCol from '@/shared/components/ol/ol-col'

/**
 * live-07 #5 (owner): the Keyboard-shortcuts modal now shows WHICH keymap the
 * user actually has active (Default / Vim / Emacs, from the per-user
 * `userSettings.mode` that setKeybindings() applies in the editor) and lists
 * that keymap's most useful combinations, with a link managing the choice in
 * /user/mysettings#key-bindings (bottom text links there too).
 */
export default function HotkeysModalKeybindingsSection({
  isMac = false,
}: {
  isMac?: boolean
}) {
  const { userSettings } = useUserSettingsContext()
  const { t } = useTranslation()
  const mode = userSettings.mode || 'default'

  if (mode === 'vim') {
    return (
      <div className="mb-4 kb-active">
        <h3>
          {t('keybindings')} — <em>{t('keybindings_active_vim')}</em>
        </h3>
        <OLRow>
          <OLCol xs={4}>
            <Hotkey combination="i" description={t('kb_vim_insert')} />
            <Hotkey combination="Esc" description={t('kb_vim_normal')} />
            <Hotkey combination="h / j / k / l" description={t('kb_vim_move')} />
          </OLCol>
          <OLCol xs={4}>
            <Hotkey combination="dd" description={t('kb_vim_delete_line')} />
            <Hotkey combination="dw" description={t('kb_vim_delete_word')} />
            <Hotkey combination="gg / G" description={t('kb_vim_top_bottom')} />
            <Hotkey combination="/" description={t('kb_vim_search')} />
          </OLCol>
          <OLCol xs={4}>
            <Hotkey combination="o / O" description={t('kb_vim_newline')} />
            <Hotkey combination="p" description={t('kb_vim_paste')} />
            <a className="kb-manage" href="/user/mysettings#key-bindings">
              {t('kb_manage')}
            </a>
          </OLCol>
        </OLRow>
      </div>
    )
  }
  if (mode === 'emacs') {
    return (
      <div className="mb-4 kb-active">
        <h3>
          {t('keybindings')} — <em>{t('keybindings_active_emacs')}</em>
        </h3>
        <OLRow>
          <OLCol xs={4}>
            <Hotkey combination="C-a / C-e" description={t('kb_emacs_line_ends')} />
            <Hotkey combination="C-f / C-b" description={t('kb_emacs_chars')} />
            <Hotkey combination="M-f / M-b" description={t('kb_emacs_words')} />
          </OLCol>
          <OLCol xs={4}>
            <Hotkey combination="C-k" description={t('kb_emacs_kill_line')} />
            <Hotkey combination="C-w" description={t('kb_emacs_kill_word')} />
            <Hotkey combination="C-y" description={t('kb_emacs_yank')} />
            <Hotkey combination={isMac ? 'C-_ / C-_' : 'C-/'} description={t('kb_emacs_undo')} />
          </OLCol>
          <OLCol xs={4}>
            <Hotkey combination="C-s / C-r" description={t('kb_emacs_search')} />
            <Hotkey combination="M-/" description={t('kb_emacs_complete')} />
            <a className="kb-manage" href="/user/mysettings#key-bindings">
              {t('kb_manage')}
            </a>
          </OLCol>
        </OLRow>
      </div>
    )
  }
  // 'default' — the standard editor keymap (current behaviour)
  return (
    <div className="mb-4 kb-active">
      <h3>
        {t('keybindings')} — <em>{t('keybindings_active_default')}</em>
      </h3>
      <p className="mb-2">{t('kb_default_desc')}</p>
      <p className="mb-0">
        <a className="kb-manage" href="/user/mysettings#key-bindings">
          {t('kb_manage')}
        </a>{' '}
        <span className="form-text">{t('kb_switch_hint')}</span>
      </p>
    </div>
  )
}
