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

// sourceEditorExtensions contract (host: frontend/js/features/
// source-editor/extensions/index.ts) calls each provider as
// `extension(options) => Extension` (bib-editor / languagetool / llm all
// export the function form). Exporting the bare Extension object crashes
// the editor on boot with "TypeError: t is not a function" in the
// moduleExtensions.map pass — the function form is mandatory here.
export const extension = (_options: Record<string, any>): Extension =>
  EditorView.updateListener.of((update) => {
    if (update.docChanged) {
      sendHeartbeat().catch(() => undefined);
    }
  });
