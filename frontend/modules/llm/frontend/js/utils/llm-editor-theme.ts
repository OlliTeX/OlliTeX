// LLM AI surfaces in the IDE follow the OVERALL theme (dark/light) —
// owner request 2026-09-03 (rail panel rendered white-on-dark in the
// previous revision). Supersedes the 2026-08-25 editor-theme-only rule:
//   1. Read the live design tokens (--wf-row-hi surface, --content-primary
//      text — both invert with the overall theme) from <body>.
//   2. Fallback to the editor colors if the tokens are missing.
//   3. llm-ui.scss maps the whole token layer onto --wf-editor-bg/-fg via
//      color-mix, so bubbles/borders/code surfaces derive automatically.
// Event-driven only (MutationObserver on body[data-theme], html
// [data-bs-theme/class] and the editor element) — no timers for the watch
// loop itself.
function readThemeTokens(): { bg?: string; fg?: string } {
    const host = (document.body || document.documentElement) as HTMLElement
    const cs = getComputedStyle(host)
    const bg = (cs.getPropertyValue('--wf-row-hi') || '').trim()
    // 2026-09-04 (R5#6, owner round-5): --content-primary stays DARK in both
    // themes (measured live: #1b222c on a dark body) — it is the un-themed
    // ink color, NOT the themed body text. The LLM surface then rendered
    // dark text on the dark panel (user: "gray/black font on a gray
    // background"). --content-primary-themed inverts with the overall theme
    // (dark body → #f4f5f6; light body → #1b222c), so it is the correct text
    // token; fall back to --content-primary if the themed var is absent.
    const fg =
        (cs.getPropertyValue('--content-primary-themed') || '').trim() ||
        (cs.getPropertyValue('--content-primary') || '').trim()
    const out: { bg?: string; fg?: string } = {}
    if (bg && bg !== 'transparent' && bg.indexOf('rgba(0, 0, 0, 0)') < 0) out.bg = bg
    if (fg && fg !== 'transparent' && fg.indexOf('rgba(0, 0, 0, 0)') < 0) out.fg = fg
    return out
}

function readEditorThemeColors(): { bg?: string; fg?: string } {
    const editor = document.querySelector<HTMLElement>('.cm-editor')
    if (!editor) return {}
    const cs = getComputedStyle(editor)
    let bg = cs.backgroundColor
    let fg = cs.color
    if (!bg || bg === 'transparent' || bg === 'rgba(0, 0, 0, 0)') {
        const scroller = editor.querySelector<HTMLElement>('.cm-scroller')
        if (scroller) {
            const scs = getComputedStyle(scroller)
            bg = bg || scs.backgroundColor
            fg = fg || scs.color
        }
    }
    const out: { bg?: string; fg?: string } = {}
    if (bg && bg !== 'transparent' && bg !== 'rgba(0, 0, 0, 0)') out.bg = bg
    if (fg) out.fg = fg
    return out
}

function luminance(color: string): number | null {
    // Supports #rgb/#rrggbb, rgb()/rgba() (comma and space syntax).
    const hex = color.match(/^#([0-9a-f]{3}|[0-9a-f]{6})$/i)
    if (hex) {
        const h = hex[1]
        const full = h.length === 3 ? h.split('').map(c => c + c).join('') : h
        const r = parseInt(full.slice(0, 2), 16)
        const g = parseInt(full.slice(2, 4), 16)
        const b = parseInt(full.slice(4, 6), 16)
        return (0.299 * r + 0.587 * g + 0.114 * b) / 255
    }
    const m = color.match(/rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)/i)
    if (m) {
        return (
            (0.299 * Number(m[1]) + 0.587 * Number(m[2]) + 0.114 * Number(m[3])) /
            255
        )
    }
    if (color === 'white' || color === 'canvas' || color === 'papayawhip') return 1
    return null
}

export function applyEditorThemeVars(scope: HTMLElement | null): void {
    if (!scope) return
    // Overall theme first (2026-09-03 owner request); editor colors are only
    // the fallback when the tokens are unavailable.
    const tokens = readThemeTokens()
    const editorColors =
        tokens.bg || tokens.fg ? {} : readEditorThemeColors()
    const bg = tokens.bg || editorColors.bg
    const fg = tokens.fg || editorColors.fg
    if (bg) scope.style.setProperty('--wf-editor-bg', bg)
    if (fg) scope.style.setProperty('--wf-editor-fg', fg)

    // Keep the accent legible on DARK surfaces: navy on near-black is
    // unreadable, so switch to the light blue accent automatically.
    const lum = bg ? luminance(bg) : null
    if (lum !== null && lum < 0.5) {
        scope.style.setProperty('--wf-accent', '#6597e0')
        scope.style.setProperty('--wf-accent-hi', '#7dabec')
        scope.style.setProperty('--wf-accent-soft', 'rgba(101, 151, 224, 0.16)')
        return
    }
    scope.style.setProperty('--wf-accent', '#28518f')
    scope.style.setProperty('--wf-accent-hi', '#214475')
    scope.style.setProperty('--wf-accent-soft', 'rgba(40, 81, 143, 0.10)')
}

export interface EditorThemeWatcher {
    stop: () => void
}

// Watches overall theme flips (body[data-theme], html [data-bs-theme/class])
// and editor element changes, then re-applies the scoped tokens.
export function watchEditorTheme(scopes: Array<HTMLElement | null>): EditorThemeWatcher {
    const update = () => {
        scopes.forEach(applyEditorThemeVars)
    }

    update()

    const docObs = new MutationObserver(() => {
        // Slight delay: theme CSS may apply after the attribute mutation.
        window.setTimeout(update, 0)
    })
    if (document.documentElement) {
        docObs.observe(document.documentElement, {
            attributes: true,
            attributeFilter: ['data-bs-theme', 'class'],
        })
    }

    // body[data-theme] is the 6.3.0 overall-theme hook (dark = "default").
    const bodyObs = new MutationObserver(() => window.setTimeout(update, 0))
    if (document.body) {
        bodyObs.observe(document.body, {
            attributes: true,
            attributeFilter: ['data-theme'],
        })
    }

    let editorObs: MutationObserver | undefined
    const attachEditorObserver = () => {
        const editor = document.querySelector<HTMLElement>('.cm-editor')
        if (!editor) return
        if (editorObs) editorObs.disconnect()
        editorObs = new MutationObserver(update)
        editorObs.observe(editor, {
            attributes: true,
            attributeFilter: ['class', 'style', 'data-theme', 'aria-theme'],
        })
    }
    attachEditorObserver()

    return {
        stop() {
            docObs.disconnect()
            bodyObs.disconnect()
            editorObs?.disconnect()
        },
    }
}
