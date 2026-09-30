// WakaTime source-editor extension (candidate F).
// Captures document changes per the CodeMirror `update` stream and feeds
// the throttled tracker (2-minute per-file, reference parity). Registered
// via overleafModuleImports.sourceEditorExtensions (settings.defaults.js)
// so it ships on every source editor — the per-request relay is a no-op
// unless the user has linked a WakaTime account (the Go route 404s when
// the instance flag is off).

import { EditorView } from '@codemirror/view';
import type { Extension } from '@codemirror/state';
import { sendHeartbeat } from './tracker';

export const extension: Extension = EditorView.updateListener.of((update) => {
  if (update.docChanged) {
    sendHeartbeat().catch(() => undefined);
  }
});
