// modules/markdown-editor (AI, 2026-10-09): link click handling, ported from
// texlyre with the LinkNavigationService replaced by OlliT-safe behavior:
//   - in-document anchors (#...) scroll within the editor
//   - external links open in a new tab (rel=noopener)
//   - relative file links are a no-op (the project file tree is the
//     navigation surface; opening panes from here is out of scope)
import type { EditorView } from '@milkdown/kit/prose/view';
import type { Mark } from '@milkdown/kit/prose/model';

const isExternalScheme = (href: string): boolean =>
	/^[a-z][a-z0-9+.-]*:/i.test(href);

const slugify = (value: string): string =>
	value
		.trim()
		.toLowerCase()
		.replace(/[`*_~[\]()]/g, '')
		.replace(/[^\p{L}\p{N}\s_-]/gu, '')
		.replace(/\s+/g, '-')
		.replace(/-+/g, '-')
		.replace(/^-|-$/g, '');

const normalizeAnchor = (value: string): string =>
	decodeURIComponent(value.trim().replace(/^#/, '')).toLowerCase();

const findLinkHref = (view: EditorView, pos: number): string | null => {
	const linkType = view.state.schema.marks.link;
	if (!linkType) return null;

	const marks: readonly Mark[] = view.state.doc.resolve(pos).marks();
	const link = marks.find((mark) => mark.type === linkType);

	const href = link?.attrs.href;
	return typeof href === 'string' && href.length > 0 ? href : null;
};

const scrollToAnchor = (view: EditorView, anchor: string): void => {
	const target = normalizeAnchor(anchor);
	if (!target) return;

	let foundPos: number | null = null;

	view.state.doc.descendants((node, pos) => {
		if (foundPos !== null) return false;

		if (node.type.name === 'heading') {
			const text = node.textContent;
			const explicit = /\{#([^}]+)\}\s*$/.exec(text);
			const slug = explicit
				? normalizeAnchor(explicit[1])
				: slugify(text.replace(/\s*\{#[^}]+\}\s*$/, ''));

			if (slug === target) {
				foundPos = pos;
				return false;
			}
		}

		return true;
	});

	if (foundPos === null) return;

	const dom = view.domAtPos(foundPos);
	const el =
		dom.node.nodeType === Node.ELEMENT_NODE
			? (dom.node as HTMLElement)
			: dom.node.parentElement;

	el?.scrollIntoView({ behavior: 'smooth', block: 'start' });
};

export const navigateHref = (view: EditorView, href: string): void => {
	if (href.startsWith('#')) {
		scrollToAnchor(view, href);
		return;
	}

	if (isExternalScheme(href)) {
		window.open(href, '_blank', 'noopener,noreferrer');
		return;
	}

	// Relative project files: no-op (v1). The owner navigates via the file tree.
};

export const createLinkClickHandler =
	() =>
	(view: EditorView, pos: number, event: MouseEvent): boolean => {
		if (!event.ctrlKey && !event.metaKey) return false;

		const href = findLinkHref(view, pos);
		if (!href) return false;

		event.preventDefault();
		navigateHref(view, href);
		return true;
	};
