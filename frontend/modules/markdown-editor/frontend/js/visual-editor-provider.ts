// modules/markdown-editor (AI, owner 2026-10-09): visual-editor provider for
// .md/.markdown files. Same contract as the tikz/diagram/drawio providers:
// claims the file, exposes the Milkdown component, defaults to visual mode
// (the user keeps the raw-source view one click away via the switch — which
// lives in the editor toolbar itself, see markdown-visual-editor.tsx).
import MarkdownVisualEditor from './components/markdown-visual-editor'

/**
 * .md / .markdown / .markdown files open in the Milkdown visual editor.
 */
const isMdFile = (filename: string | null | undefined): boolean =>
	/\.(md|markdown|mdown)$/i.test(filename || '')


export const id = 'markdown'

export const defaultVisual = true

export function isVisualEditorAvailable (filename) {
	return isMdFile(filename)
}

export function getVisualEditorComponent (filename) {
	return isMdFile(filename) ? MarkdownVisualEditor : null
}

export default {
	id,
	defaultVisual,
	isVisualEditorAvailable,
	getVisualEditorComponent,
}
