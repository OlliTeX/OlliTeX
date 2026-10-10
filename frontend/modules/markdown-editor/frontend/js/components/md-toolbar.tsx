// modules/markdown-editor (AI, 2026-10-09): compact self-contained toolbar
// for the .md Milkdown editor. Command wiring ported from the texlyre
// helpers.ts (all @milkdown/kit commands), with the two texlyre popover
// widgets (ImagePicker, TableGridSelector) replaced by plain controls:
// image via URL field (paste-upload is a follow-up) and a fixed-size table
// insert. Text labels keep the bundle asset-free (no icon font/CDN).
import { useCallback, useRef, useState } from 'react'
import { useInstance } from '@milkdown/react'
import { editorViewCtx } from '@milkdown/kit/core'
import {
	setBlockType,
	toggleMark,
	wrapIn,
} from '@milkdown/kit/prose/commands'
import { redo, undo } from '@milkdown/kit/prose/history'
import { wrapInList } from '@milkdown/kit/prose/schema-list'
import { insertTableCommand } from '@milkdown/kit/preset/gfm'
import type { MarkType, NodeType, Schema } from '@milkdown/kit/prose/model'
import type { EditorView } from '@milkdown/kit/prose/view'

const getMark = (schema: Schema, names: string[]): MarkType | null => {
	for (const name of names) {
		const mark = schema.marks[name]
		if (mark) return mark
	}
	return null
}

const getNode = (schema: Schema, names: string[]): NodeType | null => {
	for (const name of names) {
		const node = schema.nodes[name]
		if (node) return node
	}
	return null
}

const run = (
	view: EditorView,
	command: (
		state: EditorView['state'],
		dispatch?: EditorView['dispatch']
	) => boolean
): boolean => {
	const didRun = command(view.state, view.dispatch)
	if (didRun) view.focus()
	return didRun
}

interface Btn {
	key: string
	label: string
	title: string
	fn: (view: EditorView) => boolean
}

const BUTTONS: Btn[] = [
	{
		key: 'undo',
		label: '⟲',
		title: 'Undo',
		fn: view => run(view, undo),
	},
	{
		key: 'redo',
		label: '⟳',
		title: 'Redo',
		fn: view => run(view, redo),
	},
	{
		key: 'h1',
		label: 'H1',
		title: 'Heading 1',
		fn: view =>
			!!getNode(view.state.schema, ['heading_1', 'heading']) &&
			run(view, setBlockType(getNode(view.state.schema, ['heading_1', 'heading'])!, { level: 1 })),
	},
	{
		key: 'h2',
		label: 'H2',
		title: 'Heading 2',
		fn: view =>
			!!getNode(view.state.schema, ['heading_1', 'heading']) &&
			run(view, setBlockType(getNode(view.state.schema, ['heading_1', 'heading'])!, { level: 2 })),
	},
	{
		key: 'h3',
		label: 'H3',
		title: 'Heading 3',
		fn: view =>
			!!getNode(view.state.schema, ['heading_1', 'heading']) &&
			run(view, setBlockType(getNode(view.state.schema, ['heading_1', 'heading'])!, { level: 3 })),
	},
	{
		key: 'bold',
		label: 'B',
		title: 'Bold',
		fn: view => {
			const mark = getMark(view.state.schema, ['strong', 'bold'])
			return !!mark && run(view, toggleMark(mark))
		},
	},
	{
		key: 'italic',
		label: 'I',
		title: 'Italic',
		fn: view => {
			const mark = getMark(view.state.schema, ['emphasis', 'italic'])
			return !!mark && run(view, toggleMark(mark))
		},
	},
	{
		key: 'strike',
		label: 'S̶',
		title: 'Strikethrough',
		fn: view => {
			const mark = getMark(view.state.schema, ['strikethrough'])
			return !!mark && run(view, toggleMark(mark))
		},
	},
	{
		key: 'code',
		label: '</>',
		title: 'Inline code',
		fn: view => {
			const mark = getMark(view.state.schema, ['inline_code', 'code'])
			return !!mark && run(view, toggleMark(mark))
		},
	},
	{
		key: 'quote',
		label: '❝',
		title: 'Blockquote',
		fn: view => {
			const node = getNode(view.state.schema, ['blockquote'])
			return !!node && run(view, wrapIn(node))
		},
	},
	{
		key: 'ul',
		label: '•',
		title: 'Bullet list',
		fn: view => {
			const node = getNode(view.state.schema, ['bullet_list', 'bulletList'])
			return !!node && run(view, wrapInList(node))
		},
	},
	{
		key: 'ol',
		label: '1.',
		title: 'Numbered list',
		fn: view => {
			const node = getNode(view.state.schema, ['ordered_list', 'orderedList'])
			return !!node && run(view, wrapInList(node, { order: 1 }))
		},
	},
	{
		key: 'table',
		label: '⊞',
		title: 'Insert table (3×3)',
		fn: view => {
			insertTableCommand.run({ row: 3, col: 3 })
			view.focus()
			return true
		},
	},
	{
		key: 'hr',
		label: '—',
		title: 'Horizontal rule',
		fn: view => {
			const node = getNode(view.state.schema, [
				'hr',
				'horizontal_rule',
				'horizontalRule',
			])
			if (!node) return false
			view.dispatch(view.state.tr.replaceSelectionWith(node.create()).scrollIntoView())
			view.focus()
			return true
		},
	},
]

export default function MdToolbar () {
	const [, getInstance] = useInstance()
	const [imageUrl, setImageUrl] = useState('')
	const imgInputRef = useRef<HTMLInputElement | null>(null)

	const withView = useCallback(
		(fn: (view: EditorView) => boolean): boolean => {
			const editor = getInstance()
			if (!editor) return false
			let ok = false
			editor.action(ctx => {
				ok = fn(ctx.get(editorViewCtx))
			})
			return ok
		},
		[getInstance]
	)

	const doLink = useCallback(() => {
		const url = window.prompt('Link URL')
		if (!url) return
		withView(view => {
			const mark = getMark(view.state.schema, ['link'])
			if (!mark) return false
			return run(view, toggleMark(mark, { href: url }))
		})
	}, [withView])

	const doImage = useCallback(() => {
		if (!imageUrl.trim()) return
		const src = imageUrl.trim()
		withView(view => {
			const type = view.state.schema.nodes.image
			if (!type) return false
			view.dispatch(
				view.state.tr.replaceSelectionWith(type.create({ src }), false).scrollIntoView()
			)
			view.focus()
			return true
		})
		setImageUrl('')
		if (imgInputRef.current) imgInputRef.current.value = ''
	}, [imageUrl, withView])

	return (
		<div className="ol-md-toolbar" role="toolbar" aria-label="Markdown editor toolbar">
			{BUTTONS.map(b => (
				<button
					key={b.key}
					type="button"
					className="ol-md-toolbar-btn"
					title={b.title}
					onClick={e => {
						e.preventDefault()
						withView(b.fn)
					}}>
					{b.label}
				</button>
			))}
			<button
				type="button"
				className="ol-md-toolbar-btn"
				title="Insert link"
				onClick={e => {
					e.preventDefault()
					doLink()
				}}>
				🔗
			</button>
			<input
				ref={imgInputRef}
				className="ol-md-toolbar-img"
				type="text"
				placeholder="image url…"
				value={imageUrl}
				onChange={e => setImageUrl(e.target.value)}
				onKeyDown={e => {
					if (e.key === 'Enter') {
						e.preventDefault()
						doImage()
					}
				}}
			/>
			<button
				type="button"
				className="ol-md-toolbar-btn"
				title="Insert image (URL)"
				onClick={e => {
					e.preventDefault()
					doImage()
				}}>
				🖼
			</button>
		</div>
	)
}
