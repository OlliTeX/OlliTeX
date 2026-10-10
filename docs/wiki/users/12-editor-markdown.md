# Markdown files & visual editor (users)

OlliTeX projects can mix LaTeX, Typst, and **Markdown** documents. Markdown
files (`.md`) get a dedicated **visual editor** (block-based, WYSIWYG) with a
classical code mode next to it — the document stays real Markdown on disk,
so anything that reads `.md` (pandoc export, imports, your editor) keeps
working.

## Opening a Markdown file

Add a `.md` file (File → New File → `name.md`, or upload one), then click it
in the file tree. The editor opens with two modes in the mode switch:

- **Visual** — the block editor: headings (H1–H3), bold/italic/strikethrough,
  inline code, blockquote, ordered/unordered lists, tables, horizontal rule,
  links, images, and code blocks from the toolbar.
- **Code** — the same text in a CodeMirror 6 editor with Markdown
  highlighting, for precise control or bulk edits.

Both modes edit the **same underlying Markdown text**; switch freely. What
you type in Visual mode serialises back to clean Markdown; what you write in
Code mode renders in Visual mode.

> New files start **empty** — OlliTeX does not pre-fill `.md` files with
> LaTeX boilerplate. (LaTeX projects seed `main.tex`; Markdown files do not.)

## Theme

The Markdown editor follows your editor theme (light/dark and your chosen
accent) — same rules as the LaTeX/Typst panes. If a theme change doesn't
take effect until you reopen the file, close and reopen the tab.

## Diagram files (TikZ, SVG, Draw.io)

Besides text formats, projects support **diagram documents**:

| Type | Edit with | Renders in the project |
| --- | --- | --- |
| **TikZ** (`.tikz` / `\tikzset` snippets) | LaTeX source (TikZ syntax) | LaTeX compile output |
| **SVG diagram** (`.svg`) | Plain SVG source | Inline in viewers/export |
| **Draw.io** (`.drawio`) | The built-in Draw.io surface | Export/compile as raster or vector |

Create them from File → New File (diagram choices) or upload existing files.
Opening a Draw.io file mounts the embedded Draw.io editor directly in the
editor pane; the viewer surface is used for read-only preview elsewhere in
the UI.

## Exporting Markdown

From the File menu (or Project → Export), a Markdown document can be
exported alongside the project-wide exports (`.docx`, `.md`, `.html`
conversions run through the instance's conversion pipeline).

## Verified against

OlliTeX v26 surface (2026-10-11): Visual editor surface (Milkdown-based
block editor inside the `ol-md-editor-shell`), Code mode (CodeMirror 6),
mode switch, toolbar set (H1–H3, B/I/strikethrough, inline code, blockquote,
lists, table, rule, link, image), theme bridge (editor light/dark + accent
follow-through), diagram file types (TikZ / SVG / Draw.io) as first-class
project files.
