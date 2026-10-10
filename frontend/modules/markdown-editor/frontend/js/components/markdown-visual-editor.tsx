// modules/markdown-editor (AI, owner 2026-10-09): the .md visual editor for
// OlliT /editor — Milkdown (ProseMirror) ported from the texlyre reference
// implementation, bridged to the OlliT document store with the house visual-
// editor pattern (same as drawio/tikz/svgedit): the CodeMirror-backed
// document is the single source of truth; Milkdown writes into it on every
// update (Yjs persists it exactly like a code edit); Code mode re-mounts
// this component booting from the current document.
//
// VENDORING (owner hard requirement): milkdown + remark + katex come from
// node_modules and are bundled at image build time; styles.css + the crepe
// base theme CSS are imported here (bundled). No CDN, no runtime fetching.
import {
	useCallback,
	useEffect,
	useRef,
	useState,
} from 'react'
import {
	Milkdown,
	MilkdownProvider,
	useEditor,
	useInstance,
} from '@milkdown/react'
import '@milkdown/crepe/theme/common/style.css'
import 'katex/dist/katex.min.css'
import { useTranslation } from 'react-i18next'
import { useCodeMirrorViewContext } from '@/features/source-editor/components/codemirror-context'
import EditorSwitch from '@/features/source-editor/components/editor-switch'
import {
	configureMilkdownEditor,
	replaceMarkdown,
} from '../milkdown/milkdown-setup'
import MdToolbar from './md-toolbar'
import '../milkdown/styles.css'

function MarkdownSurface ({ cmView }: { cmView: ReturnType<typeof useCodeMirrorViewContext> | null }) {
	const { t } = useTranslation()
	const [loading, getInstance] = useInstance()
	const scrollRef = useRef<HTMLDivElement | null>(null)
	const lastSentRef = useRef<string>('')
	const cmRef = useRef(cmView)
	cmRef.current = cmView

	// The OlliT document (CodeMirror 6 state) is the source of truth.
	const [markdown] = useState<string>(() =>
		cmView ? cmView.state.doc.toString() : '')
	lastSentRef.current = markdown

	useEditor(
		root =>
			configureMilkdownEditor({
				root,
				defaultValue: markdown,
				editable: () => true,
				onMarkdownUpdated: (md: string) => {
					// Loop guard: skip echoes of replaceMarkdown() we did
					// ourselves (see code-mode remount path).
					if (md === lastSentRef.current) return
					lastSentRef.current = md
					const cm = cmRef.current
					if (!cm) return
					if (cm.state.doc.toString() === md) return
					cm.dispatch({
						changes: { from: 0, to: cm.state.doc.length, insert: md },
					})
				},
			}),
		[]
	)

	// Ctrl/Cmd-modified toolbar styling (ported from the reference):
	useEffect(() => {
		const node = scrollRef.current
		if (!node) return
		const onKey = (e: KeyboardEvent) => {
			node.classList.toggle('mod-held', e.ctrlKey || e.metaKey)
		}
		const onBlur = () => node.classList.remove('mod-held')
		document.addEventListener('keydown', onKey)
		document.addEventListener('keyup', onKey)
		window.addEventListener('blur', onBlur)
		return () => {
			document.removeEventListener('keydown', onKey)
			document.removeEventListener('keyup', onKey)
			window.removeEventListener('blur', onBlur)
		}
	}, [])

	return (
		<div ref={scrollRef} className="milkdown-editor-scroll">
			<Milkdown />
		</div>
	)
}

export default function MarkdownVisualEditor () {
	const cmView = useCodeMirrorViewContext()
	const { t } = useTranslation()

	if (!cmView) {
		return (
			<div className="ol-md-editor-shell">
				<div className="ol-md-editor-note">
					{t('loading', { defaultValue: 'Loading…' })}
				</div>
			</div>
		)
	}

	return (
		<MilkdownProvider>
			<div className="ol-md-editor-shell">
				<div className="ol-md-editor-toolbar-row">
					<MdToolbar />
					{/* AB-pattern (2026-10-09): the Code|Visual switch MUST be
					    reachable while the visual editor is up — in this
					    architecture the CM toolbar (and its switch) is hidden
					    together with the code pane. The drawio viewer made the
					    same mistake and it was the root of the "stuck in
					    canvas" bug. */}
					<div className="ol-md-editor-switch">
						<EditorSwitch />
					</div>
				</div>
				<MarkdownSurface cmView={cmView} />
			</div>
		</MilkdownProvider>
	)
}
