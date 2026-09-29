import { describe, it, expect } from 'vitest'
import {
    degradeGrammarMode,
    mergeGrammarDiagnostics,
} from '../../frontend/js/utils/grammar-helpers.ts'

const diag = (from, to, engine) => ({
    from,
    to,
    severity: 'warning',
    message: 'msg',
    source: engine,
    replacements: [],
    engine,
})

describe('degradeGrammarMode', () => {
    const bothOn = {
        llmAdminEnabled: true,
        llmServerConfigured: true,
        llmAvailableForUser: true,
        ltAvailable: true,
    }
    const ltOnly = { ...bothOn, llmAvailableForUser: false }
    const llmOnly = { ...bothOn, ltAvailable: false }
    const neither = {
        llmAdminEnabled: false,
        llmServerConfigured: false,
        llmAvailableForUser: false,
        ltAvailable: false,
    }

    it('keeps the mode when it is feasible', () => {
        expect(degradeGrammarMode('default', bothOn)).toBe('default')
        expect(degradeGrammarMode('lt', bothOn)).toBe('lt')
        expect(degradeGrammarMode('llm', bothOn)).toBe('llm')
        expect(degradeGrammarMode('lt+llm', bothOn)).toBe('lt+llm')
    })

    it('degrades when an engine is unavailable', () => {
        expect(degradeGrammarMode('lt', ltOnly)).toBe('lt')
        expect(degradeGrammarMode('lt', neither)).toBe('default')
        expect(degradeGrammarMode('llm', llmOnly)).toBe('llm')
        expect(degradeGrammarMode('llm', neither)).toBe('default')
    })

    it('degrades combined mode to the single-available engine', () => {
        expect(degradeGrammarMode('lt+llm', ltOnly)).toBe('lt')
        expect(degradeGrammarMode('lt+llm', llmOnly)).toBe('llm')
        expect(degradeGrammarMode('lt+llm', neither)).toBe('default')
    })

    it('never auto-upgrades the mode', () => {
        expect(degradeGrammarMode('default', bothOn)).toBe('default')
    })

    it('treats unknown modes as default', () => {
        expect(degradeGrammarMode('lt-llm' /* malformed */, bothOn)).toBe(
            'default'
        )
    })
})

describe('mergeGrammarDiagnostics (4c dedup)', () => {
    it('drops LLM diagnostics that fully overlap an LT match', () => {
        const result = mergeGrammarDiagnostics(
            [diag(0, 10, 'lt')],
            [
                diag(1, 9, 'llm'),
                diag(50, 60, 'llm'),
            ]
        )
        expect(
            result.filter(d => d.engine === 'llm').length
        ).toBe(1)
        expect(result[1].engine).toBe('llm')
    })

    it('keeps unrelated LLM diagnostics', () => {
        const result = mergeGrammarDiagnostics(
            [diag(0, 10, 'lt')],
            [diag(20, 25, 'llm')]
        )
        expect(result.length).toBe(2)
    })

    it('keeps LLM diagnostics whose overlap is < 60% of the larger range', () => {
        const result = mergeGrammarDiagnostics(
            [diag(0, 100, 'lt')],
            [diag(95, 150, 'llm')] // overlap 5 of 55 = 9% → keep
        )
        expect(result.length).toBe(2)
    })

    it('LT always survives the merge', () => {
        const result = mergeGrammarDiagnostics(
            [diag(0, 10, 'lt')],
            [diag(0, 10, 'llm')]
        )
        expect(result.filter(d => d.engine === 'lt').length).toBe(1)
        expect(result.filter(d => d.engine === 'llm').length).toBe(0)
    })

    it('preserves LLM order among survivors', () => {
        const result = mergeGrammarDiagnostics(
            [diag(0, 10, 'lt')],
            [diag(20, 25, 'llm'), diag(40, 45, 'llm')]
        )
        const llmDiags = result.filter(d => d.engine === 'llm')
        expect(llmDiags.map(d => d.from)).toEqual([20, 40])
        // LT list comes first in the merged result
        expect(result[0].engine).toBe('lt')
    })
})
