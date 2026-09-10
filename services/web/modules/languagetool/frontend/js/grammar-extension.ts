/**
 * Grammar-checking CodeMirror 6 extension (LanguageTool + LLM, single extension).
 *
 * Exported as a named `extension` function matching the `sourceEditorExtensions`
 * module-import slot signature:  (options: Record<string, any>) => Extension
 *
 * Modes (per-user, fetched from GET /user/llm-settings/grammar on mount):
 *   'default'  — no grammar checking (Overleaf Hunspell spellcheck stays on)
 *   'lt'       — LanguageTool grammar check
 *   'llm'      — LLM grammar check (spans extracted from the Lezer tree)
 *   'lt+llm'   — both, in parallel, locally merged:
 *                 LLM suggestions overlapping a LanguageTool match by ≥ 60%
 *                 of the LLM range are dropped (the deterministic tool wins)
 *
 * Availability flags come from getMeta('ol-grammarSettings'); the effective
 * mode is recomputed client-side exactly as on the server (`degradeGrammarMode`
 * mirror), so a saved mode that is no longer feasible (admin force-off,
 * missing LT URL, no LLM keys) degrades instead of throwing.
 *
 * Features (ported from the languagetool branch and extended for LLM):
 *  - Own StateField + Decoration + hover tooltip (bypasses the compile-log
 *    linter's global markerFilter/tooltipFilter combine)
 *  - Debounced: checks 2 s after the last document change
 *  - LaTeX-aware: uses the Lezer syntax tree to identify plain-text regions;
 *    commands/math/comments become LanguageTool `markup`; only `Normal` text
 *    spans are sent to the LLM
 *  - Falls back to regex-based parsing when the syntax tree is unavailable
 *  - Filters the TYPOS category (LanguageTool) when the Overleaf Hunspell
 *    spellchecker is active, to avoid duplicate underlines
 *  - Cancels stale HTTP requests with AbortController
 *
 * All logic is self-contained — no imports from Overleaf core internals.
 */

import {
  StateEffect,
  StateField,
  type Extension,
} from '@codemirror/state'
import {
  Decoration,
  type DecorationSet,
  EditorView,
  ViewPlugin,
  hoverTooltip,
  type ViewUpdate,
} from '@codemirror/view'
import { syntaxTree, ensureSyntaxTree } from '@codemirror/language'
import type { SyntaxNodeRef } from '@lezer/common'
import getMeta from '@/utils/meta'
import {
  latexToAnnotations,
  type LTAnnotation,
} from './utils/latex-to-annotations'
import { degradeGrammarMode, mergeGrammarDiagnostics } from './utils/grammar-helpers'

// ═══════════════════════════════════════════════════════════════════════
// Types
// ═══════════════════════════════════════════════════════════════════════

type GrammarMode = 'default' | 'lt' | 'llm' | 'lt+llm'

/** Shape of getMeta('ol-grammarSettings') (server-computed, no user data). */
interface GrammarSettings {
  llmAdminEnabled: boolean
  llmServerConfigured: boolean
  llmAvailableForUser: boolean
  ltAvailable: boolean
}

/** Shape of GET /user/llm-settings/grammar */
interface GrammarSettingsResponse {
  mode: GrammarMode
  effectiveMode: GrammarMode
  llmModel: string
  language: string
  /** overleaf-lab (grammar port): user-blocked LanguageTool rule IDs. */
  blockedRules?: string[]
  availability: GrammarSettings
  models: GrammarModel[]
}

interface GrammarModel {
  id: string
  name: string
  isPersonal: boolean
}

/** Our own diagnostic type — independent of @codemirror/lint's Diagnostic. */
interface GrammarDiagnostic {
  from: number
  to: number
  severity: 'error' | 'warning'
  message: string
  source: string
  replacements: string[]
  engine: 'lt' | 'llm'
}

interface LTMatch {
  offset: number
  length: number
  message: string
  rule?: { id?: string; category?: { id?: string } }
  replacements?: Array<{ value: string }>
}

interface LLMSuggestion {
  spanId: string
  start: number
  end: number
  message: string
  suggestion: string
}

interface TextSpan {
  spanId: string
  text: string
  from: number
  to: number
}

// ═══════════════════════════════════════════════════════════════════════
// Mode / availability logic
// ═══════════════════════════════════════════════════════════════════════

// degradeGrammarMode is imported from ./utils/grammar-helpers (kept in sync
// with the server-side implementation and covered by unit tests).

function emptyAvailability(): GrammarSettings {
  return {
    llmAdminEnabled: false,
    llmServerConfigured: false,
    llmAvailableForUser: false,
    ltAvailable: false,
  }
}

// ═══════════════════════════════════════════════════════════════════════
// LaTeX "Normal" text spans (LanguageTool annotation + LLM spans)
// ═══════════════════════════════════════════════════════════════════════
//
// Mirrors the `noSpellCheckProp` definitions in the Overleaf LaTeX grammar
// without importing the core module.  These argument node types contain
// non-prose content (citation keys, labels, package names, etc.) and must be
// skipped so that neither engine reports false positives on them.

/** Node types whose entire subtree is always non-prose. */
const ALWAYS_SKIP = new Set([
  'BibKeyArgument',
  'BibliographyArgument',
  'BibliographyStyleArgument',
  'DocumentClassArgument',
  'LabelArgument',
  'PackageArgument',
  'RefArgument',
])

/** Node types to skip only when they appear inside certain parent contexts. */
const CONTEXTUAL_SKIP: Record<string, string[][]> = {
  OptionalArgument: [
    ['DocumentClass'],
    ['IncludeGraphics'],
    ['LineBreak'],
    ['UsePackage'],
    ['FigureEnvironment', 'BeginEnv'],
    ['ListEnvironment', 'BeginEnv'],
  ],
  ShortTextArgument: [['Date'], ['SetLengthCommand']],
  TextArgument: [['TabularEnvironment', 'BeginEnv']],
}

function shouldSkipNode(node: SyntaxNodeRef): boolean {
  if (ALWAYS_SKIP.has(node.type.name)) return true
  const contexts = CONTEXTUAL_SKIP[node.type.name]
  if (contexts) {
    return contexts.some(ctx => node.matchContext(ctx))
  }
  return false
}

/**
 * Emit a gap (everything between two Normal text spans) as whitespace → text
 * (so LanguageTool sees sentence boundaries) and non-whitespace → markup.
 */
function emitGap(annotations: LTAnnotation[], gap: string): void {
  const segments = gap.match(/\s+|\S+/g)
  if (!segments) return
  for (const segment of segments) {
    if (/^\s+$/.test(segment)) {
      annotations.push({ text: segment })
    } else {
      annotations.push({ markup: segment })
    }
  }
}

/**
 * Walk the Lezer syntax tree collecting `Normal` text nodes per line. Nodes
 * on the skip-list are pruned (their subtree is never visited).
 *
 * Returns the annotation list (covering the whole document, LT offset
 * invariant holds) plus the plain-text spans with document offsets used for
 * the LLM spans.
 */
function buildAnnotationsAndSpans(view: EditorView): {
  annotations: LTAnnotation[]
  spans: TextSpan[]
} {
  const { state } = view
  const doc = state.doc
  const annotations: LTAnnotation[] = []
  const spans: TextSpan[] = []
  let lastEnd = 0
  let spanCounter = 0

  for (let i = 1; i <= doc.lines; i++) {
    const line = doc.line(i)
    const collected: { from: number; to: number; text: string }[] = []
    const tree = syntaxTree(state)
    tree.iterate({
      from: line.from,
      to: line.to,
      enter(node: SyntaxNodeRef) {
        if (shouldSkipNode(node)) return false
        if (node.type.name === 'Normal') {
          collected.push({
            from: node.from,
            to: node.to,
            text: state.doc.sliceString(node.from, node.to),
          })
          return false
        }
        return true
      },
    })

    for (const span of collected) {
      if (span.from > lastEnd) {
        emitGap(annotations, doc.sliceString(lastEnd, span.from))
      }
      annotations.push({ text: span.text })
      if (span.text.trim().length > 0) {
        spans.push({
          spanId: `s${spanCounter++}`,
          text: span.text,
          from: span.from,
          to: span.to,
        })
      }
      lastEnd = span.to
    }
  }

  if (lastEnd < doc.length) {
    emitGap(annotations, doc.sliceString(lastEnd, doc.length))
  }

  return { annotations, spans }
}

/**
 * Fallback when the Lezer syntax tree cannot be produced (long documents,
 * parse timeout): regex-based annotation builder. Text spans are re-derived
 * from the annotations (offsets follow the same invariant), so LLM mode
 * still works.
 */
function buildAnnotationsAndSpansFromRegex(source: string): {
  annotations: LTAnnotation[]
  spans: TextSpan[]
} {
  const annotations = latexToAnnotations(source)
  const spans: TextSpan[] = []
  let offset = 0
  annotations.forEach((a, idx) => {
    if (a.text && a.text.trim().length > 0) {
      spans.push({
        spanId: `s${idx}`,
        text: a.text,
        from: offset,
        to: offset + a.text.length,
      })
    }
    offset += (a.text ?? a.markup ?? '').length
  })
  return { annotations, spans }
}

// ═══════════════════════════════════════════════════════════════════════
// State management
// ═══════════════════════════════════════════════════════════════════════
//
// Own StateField → DecorationSet pipeline + standalone hoverTooltip. We
// deliberately do NOT use @codemirror/lint's `linter()` because its
// `lintConfig` facet is a global combine: the compile-log linter's
// `markerFilter` (only severity=error) and `tooltipFilter` (returns [])
// suppress grammar-warning underlines and tooltips.

const setGrammarDiagnosticsEffect = StateEffect.define<
  readonly GrammarDiagnostic[]
>()

const grammarDiagnosticsField = StateField.define<{
  diagnostics: readonly GrammarDiagnostic[]
  decorations: DecorationSet
}>({
  create() {
    return { diagnostics: [], decorations: Decoration.none }
  },
  update({ diagnostics, decorations }, tr) {
    for (const effect of tr.effects) {
      if (effect.is(setGrammarDiagnosticsEffect)) {
        const diags = effect.value
        if (diags.length === 0) {
          return { diagnostics: [], decorations: Decoration.none }
        }
        const marks = diags
          .filter(d => d.from < d.to && d.to <= tr.state.doc.length)
          .map(d =>
            Decoration.mark({
              class: `cm-grammar-underline cm-grammar-underline-${d.engine}`,
            }).range(d.from, d.to)
          )
        return {
          diagnostics: diags,
          decorations: Decoration.set(marks, true),
        }
      }
    }
    if (tr.docChanged) {
      const mapped = diagnostics
        .map(d => ({
          ...d,
          from: tr.changes.mapPos(d.from, 1),
          to: tr.changes.mapPos(d.to, -1),
        }))
        .filter(d => d.from < d.to)
      return {
        diagnostics: mapped,
        decorations: decorations.map(tr.changes),
      }
    }
    return { diagnostics, decorations }
  },
  provide: f => EditorView.decorations.from(f, val => val.decorations),
})

// ═══════════════════════════════════════════════════════════════════════
// Hover tooltip
// ═══════════════════════════════════════════════════════════════════════

const grammarTooltip = hoverTooltip(
  (view, pos) => {
    const { diagnostics } = view.state.field(grammarDiagnosticsField)
    const matches = diagnostics.filter(d => d.from <= pos && pos <= d.to)
    if (!matches.length) return null

    const primary = matches[0]
    return {
      pos: primary.from,
      end: primary.to,
      above: true,
      create() {
        const dom = document.createElement('div')
        dom.className = 'cm-grammar-tooltip'

        for (const match of matches) {
          const entry = document.createElement('div')
          entry.className = 'cm-grammar-tooltip-entry'

          const msg = document.createElement('div')
          msg.className = 'cm-grammar-tooltip-message'
          msg.textContent = match.message
          entry.appendChild(msg)

          if (match.source) {
            const src = document.createElement('div')
            src.className = 'cm-grammar-tooltip-source'
            src.textContent = match.source
            entry.appendChild(src)
          }

          if (match.replacements.length) {
            const actions = document.createElement('div')
            actions.className = 'cm-grammar-tooltip-actions'
            for (const rep of match.replacements) {
              const btn = document.createElement('button')
              btn.className = 'cm-grammar-tooltip-action'
              btn.textContent = rep
              btn.addEventListener('click', e => {
                e.preventDefault()
                view.dispatch({
                  changes: { from: match.from, to: match.to, insert: rep },
                })
              })
              actions.appendChild(btn)
            }
            entry.appendChild(actions)
          }

          dom.appendChild(entry)
        }

        return { dom }
      },
    }
  },
  { hoverTime: 300 }
)

// ═══════════════════════════════════════════════════════════════════════
// Theme (per-engine visual distinction: LT = orange, LLM = blue)
// ═══════════════════════════════════════════════════════════════════════

const grammarTheme = EditorView.baseTheme({
  '.cm-grammar-underline': {
    backgroundImage: 'none !important',
    textDecoration: 'underline wavy',
    textUnderlineOffset: '3px',
  },
  '.cm-grammar-underline-lt': {
    textDecorationColor: '#E8A200',
  },
  '.cm-grammar-underline-llm': {
    textDecorationColor: '#2E91D1',
  },
  '.cm-grammar-tooltip': {
    backgroundColor: '#fff',
    border: '1px solid #ddd',
    borderRadius: '4px',
    padding: '8px',
    fontSize: '13px',
    maxWidth: '400px',
    boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
  },
  '.cm-grammar-tooltip-entry + .cm-grammar-tooltip-entry': {
    marginTop: '8px',
    paddingTop: '8px',
    borderTop: '1px solid #eee',
  },
  '.cm-grammar-tooltip-message': {
    marginBottom: '4px',
  },
  '.cm-grammar-tooltip-source': {
    fontSize: '11px',
    color: '#888',
    marginBottom: '4px',
  },
  '.cm-grammar-tooltip-actions': {
    display: 'flex',
    flexWrap: 'wrap',
    gap: '4px',
  },
  '.cm-grammar-tooltip-action': {
    padding: '2px 8px',
    border: '1px solid #ccc',
    borderRadius: '3px',
    backgroundColor: '#f5f5f5',
    cursor: 'pointer',
    fontSize: '12px',
    '&:hover': {
      backgroundColor: '#e0e0e0',
    },
  },
  // Owner #13 (2026-09-13 editor wave): the tooltip above hardcodes light
  // colors (white box, #888 source text, #f5f5f5 suggestion chips) — under a
  // dark code theme (overall-theme-dark) a suggestion like "recompile"
  // rendered as gray text on a gray chip, i.e. unreadable. The dark variant
  // follows the app's neutral scale (both defined in foundations/colors.scss
  // for light + dark UIs) so the popup tracks the active editor theme.
  '.overall-theme-dark .cm-grammar-tooltip': {
    backgroundColor: 'var(--neutral-80, #2f3a4c)',
    border: '1px solid var(--neutral-60, #677283)',
    color: 'var(--neutral-10, #f4f5f6)',
  },
  '.overall-theme-dark .cm-grammar-tooltip-entry + .cm-grammar-tooltip-entry': {
    borderTop: '1px solid var(--neutral-70, #495365)',
  },
  '.overall-theme-dark .cm-grammar-tooltip-source': {
    color: 'var(--neutral-40, #afb5c0)',
  },
  '.overall-theme-dark .cm-grammar-tooltip-action': {
    backgroundColor: 'var(--neutral-70, #495365)',
    border: '1px solid var(--neutral-60, #677283)',
    color: 'var(--neutral-10, #f4f5f6)',
    '&:hover': {
      backgroundColor: 'var(--neutral-60, #677283)',
    },
  },
})

// ═══════════════════════════════════════════════════════════════════════
// Core checks
// ═══════════════════════════════════════════════════════════════════════

/**
 * Run the LanguageTool check on the given document (annotations + spans
 * precomputed so the LLM check can reuse the same spans in 4c mode).
 */
async function runLanguageToolCheck(
  view: EditorView,
  annotations: LTAnnotation[],
  language: string,
  hunspellActive: boolean,
  ac: AbortController,
  picky: boolean,
  blockedRules: ReadonlySet<string>
): Promise<GrammarDiagnostic[]> {
  const csrf = getMeta('ol-csrfToken') as string | undefined
  const response = await fetch('/languagetool/check', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(csrf ? { 'X-Csrf-Token': csrf } : {}),
    },
    body: JSON.stringify({
      language,
      // overleaf-lab (grammar port): per-project pickiness (default ON on the
      // server when absent). Explicit value here so project settings win.
      picky,
      data: { annotation: annotations },
    }),
    signal: ac.signal,
  })
  if (!response.ok) return []

  const result: { matches: LTMatch[] } = await response.json()

  // overleaf-lab (grammar port): drop user-blocked rules (free-text list from
  // the user settings page; matched by rule.id, case-insensitively — the
  // tooltip shows the exact id, but user-typed entries may differ in case).
  if (blockedRules.size > 0) {
    result.matches = (
      result.matches || []
    ).filter(m => !m.rule?.id || !blockedRules.has(String(m.rule.id).toLowerCase()))
  }

  // Filter TYPOS when Hunspell is active (it handles typos itself).
  let matches = result.matches
  if (hunspellActive) {
    matches = matches.filter(m => m.rule?.category?.id !== 'TYPOS')
  }

  const docLen = view.state.doc.length
  return matches
    .filter(m => m.offset >= 0 && m.offset + m.length <= docLen)
    .map(m => ({
      from: m.offset,
      to: m.offset + m.length,
      severity: 'error' as const,
      message: m.message,
      source: `LanguageTool (${m.rule?.id ?? ''})`,
      replacements: (m.replacements ?? []).slice(0, 5).map(r => r.value),
      engine: 'lt' as const,
    }))
}

/**
 * Run the LLM grammar check on plain-text spans extracted from the Lezer
 * tree (only text is sent, never markup, so we don't pay for LaTeX).
 */
async function runLLMCheck(
  view: EditorView,
  spans: TextSpan[],
  llmModel: string,
  ac: AbortController
): Promise<GrammarDiagnostic[]> {
  if (spans.length === 0) return []

  const projectId = getMeta('ol-project_id') as string | undefined
  if (!projectId) return []
  const csrf = getMeta('ol-csrfToken') as string | undefined

  const response = await fetch(`/project/${projectId}/llm/grammar`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(csrf ? { 'X-Csrf-Token': csrf } : {}),
    },
    body: JSON.stringify({
      spans: spans.map(s => ({ spanId: s.spanId, text: s.text })),
      model: llmModel || undefined,
    }),
    signal: ac.signal,
  })
  if (!response.ok) return []

  const result: {
    success: boolean
    suggestions: LLMSuggestion[]
  } = await response.json()
  if (!result.success) return []

  const spansById = new Map(spans.map(s => [s.spanId, s]))
  const modelLabel = llmModel || 'server model'

  return result.suggestions
    .filter(s => {
      const span = spansById.get(s.spanId)
      return (
        !!span &&
        typeof s.start === 'number' &&
        typeof s.end === 'number' &&
        s.start >= 0 &&
        s.end > s.start &&
        s.end <= span.text.length
      )
    })
    .map(s => {
      const span = spansById.get(s.spanId) as TextSpan
      const suggestion = typeof s.suggestion === 'string' ? s.suggestion : ''
      return {
        from: span.from + s.start,
        to: span.from + s.end,
        severity: suggestion ? ('error' as const) : ('warning' as const),
        message: s.message || 'Grammar suggestion',
        source: `LLM (${modelLabel})`,
        replacements: suggestion ? [suggestion] : [],
        engine: 'llm' as const,
      }
    })
}

// (merged via mergeGrammarDiagnostics from ./utils/grammar-helpers — see
// the 4c merge in runCheck below)

// ═══════════════════════════════════════════════════════════════════════
// Module export
// ═══════════════════════════════════════════════════════════════════════

export const extension = (options: Record<string, any>): Extension => {
  // Static availability flags from the editor meta (always present, no user
  // data). Effective mode + user preference arrive via the hydration fetch
  // below (GET /user/llm-settings/grammar).
  const metaSettings: GrammarSettings =
    (getMeta('ol-grammarSettings') as GrammarSettings) || emptyAvailability()

  return [
    grammarDiagnosticsField,
    grammarTooltip,
    grammarTheme,
    syncPlugin(metaSettings, options),
  ]
}

function syncPlugin(
  metaSettings: GrammarSettings,
  options: Record<string, any>
) {
  return ViewPlugin.define(view => {
    let available: GrammarSettings = metaSettings
    // Initial guess from the meta (server flags, no personal key info). It
    // is only used to gate scheduling; the authoritative mode arrives via
    // the hydration fetch below and `runCheck` is gated on `hydrated`.
    let mode: GrammarMode = 'default'
    let llmModel: string = ''
    let language: string = 'auto'
    let hunspellActive = !!options?.spelling?.spellCheckLanguage
    // overleaf-lab (grammar port): per-project pickiness (default ON) and the
    // per-user blocked-rules set (empty until hydration fills it).
    let picky: boolean = options?.grammarPicky !== false
    let blockedRules: ReadonlySet<string> = new Set<string>()
    let timer: ReturnType<typeof setTimeout> | null = null
    let controller: AbortController | null = null
    let hydrated = false

    // ── Scheduling ─────────────────────────────────────────────────────

    function scheduleCheck(delay = 2000) {
      if (timer) clearTimeout(timer)
      if (mode === 'default') return
      timer = setTimeout(() => runCheck(), delay)
    }

    function clearAll() {
      if (timer) {
        clearTimeout(timer)
        timer = null
      }
      if (controller) {
        controller.abort()
        controller = null
      }
      view.dispatch({ effects: setGrammarDiagnosticsEffect.of([]) })
    }

    // ── Core check logic ───────────────────────────────────────────────

    async function runCheck() {
      if (!hydrated || mode === 'default') return

      // Abort any in-flight request.
      if (controller) controller.abort()
      const ac = new AbortController()
      controller = ac

      const source = view.state.doc.toString()
      if (source.trim().length < 20) {
        if (!ac.signal.aborted) {
          view.dispatch({ effects: setGrammarDiagnosticsEffect.of([]) })
        }
        return
      }

      // Prefer the Lezer syntax tree; fall back to the regex parser.
      let built: { annotations: LTAnnotation[]; spans: TextSpan[] }
      try {
        const tree = ensureSyntaxTree(view.state, view.state.doc.length, 500)
        if (tree) {
          built = buildAnnotationsAndSpans(view)
        } else {
          built = buildAnnotationsAndSpansFromRegex(source)
        }
      } catch {
        built = buildAnnotationsAndSpansFromRegex(source)
      }

      const spanTexts = built.spans
      if (spanTexts.length === 0) {
        if (!ac.signal.aborted) {
          view.dispatch({ effects: setGrammarDiagnosticsEffect.of([]) })
        }
        return
      }

      const wantsLT = mode === 'lt' || mode === 'lt+llm'
      const wantsLLM = mode === 'llm' || mode === 'lt+llm'

      // Parallel independent engine checks; merge locally (no backend
      // coordination in 4c).
      const [ltDiags, llmDiags] = await Promise.all([
        wantsLT
          ? runLanguageToolCheck(view, built.annotations, language, hunspellActive, ac, picky, blockedRules)
          : Promise.resolve([] as GrammarDiagnostic[]),
        wantsLLM
          ? runLLMCheck(view, built.spans, llmModel, ac)
          : Promise.resolve([] as GrammarDiagnostic[]),
      ])

      if (ac.signal.aborted) return

      const merged =
        wantsLT && wantsLLM
          ? mergeGrammarDiagnostics(ltDiags, llmDiags)
          : wantsLT
            ? ltDiags
            : llmDiags

      view.dispatch({ effects: setGrammarDiagnosticsEffect.of(merged) })
    }

    // ── Settings hydration + change listener ──────────────────────────

    async function hydrate() {
      try {
        const response = await fetch('/user/llm-settings/grammar', {
          credentials: 'same-origin',
        })
        if (!response.ok) return
        const data: GrammarSettingsResponse = await response.json()
        // overleaf-lab (grammar port): blocked rules (per-user).
        if (Array.isArray(data.blockedRules)) {
          // Store lowercase so the per-match filter can compare case-insensitively.
          blockedRules = new Set(
            data.blockedRules.map(r => String(r).trim().toLowerCase()).filter(Boolean)
          )
        }
        applySettings(
          data.mode || 'default',
          data.llmModel || '',
          data.language || 'auto',
          data.availability &&
              typeof data.availability.llmAvailableForUser === 'boolean'
              ? data.availability
              : available,
        )
        hydrated = true
      } catch {
        hydrated = true
      }
    }

    function applySettings(
      nextMode: GrammarMode,
      nextModel: string,
      nextLanguage: string,
      nextAvailable: GrammarSettings
    ) {
      available = nextAvailable
      llmModel = nextModel
      language = nextLanguage
      mode = degradeGrammarMode(nextMode, nextAvailable)
      if (mode !== 'default') {
        scheduleCheck(500)
      } else {
        clearAll()
      }
    }

    const handler = (event: Event) => {
      if (!hydrated) {
        // Hydration is in flight: the GET response is authoritative.
        return
      }
      const detail = (
        event as CustomEvent<{
          mode?: GrammarMode
          llmModel?: string
          language?: string
          blockedRules?: string[]
        }>
      ).detail

      const nextMode = detail.mode ?? mode
      const nextModel = detail.llmModel ?? llmModel
      const nextLanguage = detail.language ?? language
      const effectiveMode = degradeGrammarMode(nextMode, available)

      // overleaf-lab (grammar port): the settings UI may have changed the
      // blocked-rules list — adopt it (it is server-side state shared with
      // other tabs).
      let blockedChanged = false
      if (Array.isArray(detail.blockedRules)) {
        const next = new Set(
          detail.blockedRules.map(r => String(r).trim().toLowerCase()).filter(Boolean)
        )
        if (
          next.size !== blockedRules.size ||
          [...next].some(r => !blockedRules.has(r))
        ) {
          blockedRules = next
          blockedChanged = true
        }
      }

      if (
        effectiveMode === mode &&
        nextModel === llmModel &&
        nextLanguage === language &&
        !blockedChanged
      ) {
        return
      }
      mode = effectiveMode
      llmModel = nextModel
      language = nextLanguage
      clearAll()
      if (mode !== 'default') {
        scheduleCheck(100)
      }
    }

    window.addEventListener('grammar:settings-changed', handler)

    // overleaf-lab (grammar port): project pickiness changed (server socket
    // broadcast 'grammarPickyUpdated' → React dispatches this window event).
    const pickyHandler = (event: Event) => {
      const detail = (event as CustomEvent<{ picky?: boolean }>).detail
      if (typeof detail?.picky !== 'boolean') return
      if (detail.picky === picky) return
      picky = detail.picky
      if (hydrated && mode !== 'default') {
        if (controller) controller.abort()
        scheduleCheck(100)
      }
    }
    window.addEventListener('grammar:picky-changed', pickyHandler)

    hydrate()

    // ── ViewPlugin callbacks ───────────────────────────────────────────

    return {
      update(update: ViewUpdate) {
        if (update.docChanged && mode !== 'default') {
          // Abort stale in-flight request and schedule a fresh check.
          if (controller) controller.abort()
          scheduleCheck()
        }

        // Track Hunspell state changes: the spelling extension dispatches
        // setSpellCheckLanguageEffect with shape
        // `{ spellCheckLanguage: string | undefined }`.
        for (const tr of update.transactions) {
          for (const effect of tr.effects) {
            const val = effect.value as Record<string, unknown> | null
            if (
              val !== null &&
              typeof val === 'object' &&
              'spellCheckLanguage' in val
            ) {
              const newActive = !!(val.spellCheckLanguage as string)
              if (newActive !== hunspellActive) {
                hunspellActive = newActive
                if (mode !== 'default') scheduleCheck(100)
              }
            }
          }
        }
      },

      destroy() {
        window.removeEventListener('grammar:settings-changed', handler)
        window.removeEventListener('grammar:picky-changed', pickyHandler)
        if (timer) clearTimeout(timer)
        if (controller) {
          controller.abort()
          controller = null
        }
      },
    }
  })
}
