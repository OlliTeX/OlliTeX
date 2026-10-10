// modules/markdown-editor (AI, 2026-10-09): ported from the texlyre
// milkdown viewer (extras/viewers/milkdown/milkdownSetup.ts) with the
// texlyre-only parts removed (language-data list, codemirror-lang-latex/
// codemirror-lang-bib, safeTypst, HighlightTheme). Everything here is
// bundled at image build time from node_modules — no runtime fetching.
import {
	Editor,
	rootCtx,
	defaultValueCtx,
	editorViewCtx,
	editorViewOptionsCtx,
	serializerCtx,
	parserCtx,
	remarkStringifyOptionsCtx,
} from '@milkdown/kit/core';
import { commonmark } from '@milkdown/kit/preset/commonmark';
import { gfm } from '@milkdown/kit/preset/gfm';
import { history } from '@milkdown/kit/plugin/history';
import { listener, listenerCtx } from '@milkdown/kit/plugin/listener';
import { tableBlock } from '@milkdown/kit/component/table-block';
import { listItemBlockComponent } from '@milkdown/kit/component/list-item-block';
import {
	linkTooltipPlugin,
	configureLinkTooltip,
} from '@milkdown/kit/component/link-tooltip';
import { codeBlockComponent } from '@milkdown/kit/component/code-block';
import { Slice } from '@milkdown/kit/prose/model';
import { TextSelection } from '@milkdown/kit/prose/state';
import type { Ctx } from '@milkdown/kit/ctx';
import type { EditorView } from '@milkdown/kit/prose/view';

import { createLinkClickHandler } from './linkClick';
import {
	mathBlockInputRule,
	mathBlockSchema,
	mathBlockView,
	mathInlineInputRule,
	mathInlineSchema,
	mathInlineView,
	remarkMathPlugin,
} from './plugins/math';
import {
	frontmatterSchema,
	remarkFrontmatterPlugin,
} from './plugins/frontmatter';

export const MILKDOWN_THEME_CLASS = 'ol-milkdown';

interface MilkdownConfigOptions {
	root: HTMLElement;
	defaultValue: string;
	editable: () => boolean;
	onMarkdownUpdated: (markdown: string) => void;
}

export function configureMilkdownEditor(options: MilkdownConfigOptions): Editor {
	const { root, defaultValue, editable, onMarkdownUpdated } = options;

	const editor = Editor.make()
		.config((ctx) => {
			ctx.set(rootCtx, root);
			ctx.set(defaultValueCtx, defaultValue);

			ctx.set(remarkStringifyOptionsCtx, {
				bullet: '-',
				rule: '-',
				ruleRepetition: 3,
				ruleSpaces: false,
				setext: false,
				listItemIndent: 'one',
				tightDefinitions: true,
			});

			ctx.update(editorViewOptionsCtx, (prev) => ({
				...prev,
				attributes: {
					class: MILKDOWN_THEME_CLASS,
					spellcheck: 'true',
				},
				editable,
				handleClick: createLinkClickHandler(),
			}));

			configureLinkTooltip(ctx);

			const l = ctx.get(listenerCtx);

			l.markdownUpdated((_ctx, markdown, prevMarkdown) => {
				if (markdown !== prevMarkdown) {
					onMarkdownUpdated(markdown);
				}
			});
		})
		.use(commonmark)
		.use(remarkFrontmatterPlugin)
		.use(frontmatterSchema)
		.use(history)
		.use(listener)
		.use(listItemBlockComponent)
		.use(codeBlockComponent)
		.use(remarkMathPlugin)
		.use(mathInlineSchema)
		.use(mathInlineView)
		.use(mathInlineInputRule)
		.use(mathBlockSchema)
		.use(mathBlockView)
		.use(mathBlockInputRule)
		.use(gfm)
		.use(tableBlock)
		.use(linkTooltipPlugin);

	return editor;
}

export function readMarkdown(ctx: Ctx): string {
	const view = ctx.get(editorViewCtx);
	const serializer = ctx.get(serializerCtx);

	return serializer(view.state.doc);
}

export function replaceMarkdown(ctx: Ctx, markdown: string): void {
	const view: EditorView = ctx.get(editorViewCtx);
	const parser = ctx.get(parserCtx);
	const doc = parser(markdown);

	if (!doc) return;

	const state = view.state;
	const tr = state.tr;

	tr.replace(0, state.doc.content.size, new Slice(doc.content, 0, 0));

	const anchor = Math.min(state.selection.anchor, tr.doc.content.size);

	tr.setSelection(TextSelection.create(tr.doc, anchor));
	view.dispatch(tr.setMeta('addToHistory', false));
}
