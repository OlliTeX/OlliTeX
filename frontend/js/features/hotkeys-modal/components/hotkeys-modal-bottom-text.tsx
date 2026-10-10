import { Trans } from 'react-i18next'
import * as eventTracking from '../../../infrastructure/event-tracking'

export default function HotkeysModalBottomText() {
  return (
    <div className="hotkeys-modal-bottom-text">
      <Trans
        i18nKey="kb_bottom_manage"
        components={[
          // eslint-disable-next-line jsx-a11y/anchor-has-content, react/jsx-key
          // live-07 #5 (owner): this link now points at the user's own
          // key-bindings settings (Default / Vim / Emacs).
          <a
            onClick={() => eventTracking.sendMB('left-menu-hotkeys-template')}
            /* owner 2026-10-10 (item M): the old /user/mysettings#key-bindings
               hash is a retired pre-reorg route — the key-bindings page is
               now the /user-settings leaf mysettings.keybindings. */
            href="/user-settings/mysettings.keybindings"
          />,
        ]}
      />
    </div>
  )
}
