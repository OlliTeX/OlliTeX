// overleaf-lab (owner request H/J/Q/Z, 2026-10-07): ONE shared source of truth
// for "is the LLM surface usable right now?" consumed by every AI entry point:
//   J — toolbar "Ask AI" button
//   Q — rail "AI Assistant" tab
//   Z — Insert → "AI Generate" group
//   H — the "Select LLM Model" modal (kept openable, just shows a notice)
//
// A surface is AVAILABLE only when BOTH hold:
//   1. the super-admin enabled at least one LLM feature for this project
//      (live per-project flags: chat / inline completion / compliance review), AND
//   2. at least one selectable model exists (site backend or user BYO rows).
//
// Fail CLOSED: until the flags load, or on any fetch failure, availability is
// false. A deployment with LLM off (or with LLM on but no model configured)
// therefore never shows a broken/empty AI entry point — the owner's exact ask.
// The "Select LLM Model" entry point (H) stays reachable regardless so the
// admin/user can still configure a model; its modal renders a "no model" notice.
import { useLLMFeatures } from './use-llm-features'
import { useLLMModelSelection } from './use-llm-model-selection'

export interface LLMAvailability {
    /** true only once flags + model list are loaded AND a model is selectable */
    available: boolean
    /** admin enabled at least one LLM feature for this project (live flags) */
    adminEnabled: boolean
    /** at least one selectable model (site + BYO rows) */
    hasModel: boolean
    /** both the feature flags and the model list have finished loading */
    loaded: boolean
}

export function useLLMAvailability(): LLMAvailability {
    const features = useLLMFeatures()
    const { options, loaded: modelsLoaded } = useLLMModelSelection()

    const adminEnabled =
        features.chatEnabled || features.completionEnabled || features.reviewEnabled
    const hasModel = options.length > 0
    const loaded = features.loaded && modelsLoaded
    const available = loaded && adminEnabled && hasModel

    return { available, adminEnabled, hasModel, loaded }
}
