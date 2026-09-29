/**
 * Pure grammar-mode helpers shared by the editor extension (and unit
 * tests). No DOM / CodeMirror dependencies.
 *
 * Keep `degradeGrammarMode` in sync with the server-side implementation in
 * modules/llm/app/src/LLMSettingsController.mjs.
 */

export type GrammarMode = 'default' | 'lt' | 'llm' | 'lt+llm'

export interface GrammarAvailability {
    llmAdminEnabled: boolean
    llmServerConfigured: boolean
    llmAvailableForUser: boolean
    ltAvailable: boolean
}

export interface GrammarDiagnostic {
    from: number
    to: number
    severity: 'error' | 'warning'
    message: string
    source: string
    replacements: string[]
    engine: 'lt' | 'llm'
}

export const GRAMMAR_MODES: GrammarMode[] = ['default', 'lt', 'llm', 'lt+llm']

/**
 * Validate a stored mode against availability and degrade to the next best
 * feasible mode (never auto-upgrades; mirrors the server logic).
 */
export function degradeGrammarMode(
    mode: GrammarMode,
    a: GrammarAvailability
): GrammarMode {
    if (!GRAMMAR_MODES.includes(mode)) return 'default'
    if (mode === 'lt+llm') {
        if (a.ltAvailable && a.llmAvailableForUser) return 'lt+llm'
        if (a.ltAvailable) return 'lt'
        if (a.llmAvailableForUser) return 'llm'
        return 'default'
    }
    if (mode === 'lt') return a.ltAvailable ? 'lt' : 'default'
    if (mode === 'llm') return a.llmAvailableForUser ? 'llm' : 'default'
    return 'default'
}

/**
 * Combined-mode (4c) merge, run on the client.
 *  - LanguageTool is the deterministic, cheap layer — it wins conflicts.
 *  - LLM suggestions overlapping an LT match by ≥ 60% of the larger range
 *    are dropped (the LLM suggestion was mostly redundant with LT).
 *  - Hunspell underlines are handled by the existing spell extension and
 *    are not part of this merge.
 */
export function mergeGrammarDiagnostics(
    ltDiags: GrammarDiagnostic[],
    llmDiags: GrammarDiagnostic[]
): GrammarDiagnostic[] {
    const merged = llmDiags.filter(llmDiag => {
        for (const ltDiag of ltDiags) {
            const overlapStart = Math.max(ltDiag.from, llmDiag.from)
            const overlapEnd = Math.min(ltDiag.to, llmDiag.to)
            const overlap = Math.max(0, overlapEnd - overlapStart)
            if (overlap <= 0) continue
            const llmRange = llmDiag.to - llmDiag.from
            const ltRange = ltDiag.to - ltDiag.from
            if (overlap >= 0.6 * Math.max(llmRange, ltRange)) return false
        }
        return true
    })

    return [...ltDiags, ...merged]
}
