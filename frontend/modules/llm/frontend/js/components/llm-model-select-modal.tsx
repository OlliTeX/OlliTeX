// overleaf-lab: shared "Select LLM Model" dialog (owner request 2026-08-25).
// The single place where the deployment-wide model choice is made; the value
// drives Chat, Review, all AI Generate items and the ask-AI context menu.
import React, { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { OLModal } from '@/shared/components/ol/ol-modal'
import { useMantineSurface } from '@/features/editor-v2/mantine-surface'
import MaterialIcon from '@/shared/components/material-icon'
import { useLLMModelSelection } from '../hooks/use-llm-model-selection'
import '../../stylesheets/llm-ui.scss'

interface LLMModelSelectModalProps {
    show: boolean
    onHide: () => void
}

const LLMModelSelectModal = React.memo(function LLMModelSelectModal({
    show,
    onHide,
}: LLMModelSelectModalProps) {
    const { t } = useTranslation()
  const { Btn } = useMantineSurface() // module Mantine wave (M4)

    // overleaf-lab (owner request 2026-08-26): the "Deployment default" pseudo
    // option is gone — only concrete models (site + BYO rows) are selectable.
    const { options, loaded, selected, apply } = useLLMModelSelection()
    const [local, setLocal] = useState('')
    // overleaf-lab (owner request H, 2026-10-07): an empty model list is a real,
    // informational state (LLM not configured on this server) — surface a clear
    // notice and disable the apply button instead of a dead empty radiogroup
    // + an active "Use this model" that does nothing. The modal stays openable
    // for exactly this (the admin then knows to configure a model).
    const noModels = loaded && options.length === 0

    // Re-seed each time the modal opens (and live if another surface changed
    // the selection while it is open). When nothing is selected yet, preselect
    // the first concrete model (site default) so "Use this model" always picks
    // a real model.
    useEffect(() => {
        if (show) setLocal(selected || (loaded ? options[0]?.value || '' : ''))
    }, [show, selected, loaded, options])

    const save = () => {
        apply(local)
        onHide()
    }

    return (
        <OLModal
            show={show}
            onHide={onHide}
            aria-label={t('llm_select_model', 'Select LLM Model')}
        >
            <div className="modal-header">
                <h5 className="modal-title">
                    <MaterialIcon type="model_training" className="me-2" />
                    {t('llm_select_model', 'Select LLM Model')}
                </h5>
                <button
                    type="button"
                    className="btn-close"
                    onClick={onHide}
                    aria-label={t('close', 'Close')}
                />
            </div>
            <div className="modal-body">
                <p className="llm-model-hint">
                    {t(
                        'llm_select_model_hint',
                        'This model is used everywhere — AI Assistant chat, Review, the AI Generate menu items and ask-AI on selected text. The choice is saved to your profile and follows you across projects.',
                    )}
                </p>
                <div className="llm-model-option-list" role="radiogroup" aria-busy={!loaded}>
                    {!loaded && (
                        <div className="llm-model-option loading">
                            {t('llm_loading', 'Loading…')}
                        </div>
                    )}
                    {noModels && (
                        <div className="llm-model-option llm-model-option-notice" role="note">
                            <MaterialIcon type="notification_important" className="me-2" />
                            {t(
                                'llm_no_models',
                                'No LLM model has been configured on this server. Ask an administrator to set one up — you can pick a model here once one is available.',
                            )}
                        </div>
                    )}
                    {options.map(o => (
                        <label
                            key={o.value || 'default'}
                            className={`llm-model-option${local === o.value ? ' selected' : ''}`}
                        >
                            <input
                                type="radio"
                                name="llm-select-model"
                                value={o.value}
                                checked={local === o.value}
                                onChange={() => setLocal(o.value)}
                            />
                            <span className="llm-model-option-body">
                                <span className="llm-model-option-label">
                                    {o.label}
                                </span>
                                {o.rowName && (
                                    <span className="llm-model-option-row">
                                        {o.rowName}
                                    </span>
                                )}
                            </span>
                        </label>
                    ))}
                </div>
            </div>
            <div className="modal-footer">
                <Btn variant="tertiary" onClick={onHide}>
                    {t('cancel', 'Cancel')}
                </Btn>
                <Btn variant="primary" onClick={save} disabled={!loaded || noModels}>
                    {t('llm_apply_model', 'Use this model')}
                </Btn>
            </div>
        </OLModal>
    )
})

export default LLMModelSelectModal
