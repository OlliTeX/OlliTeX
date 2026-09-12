import { useTranslation } from 'react-i18next'
import {
  OLModalBody,
  OLModalFooter,
  OLModalHeader,
  OLModalTitle,
} from '@/shared/components/ol/ol-modal'
import OLButton from '@/shared/components/ol/ol-button'
import { WordCountClient } from './word-count-client'
import SplitTestBadge from '@/shared/components/split-test-badge'
import { useEffect, useMemo } from 'react'
import { useEditorAnalytics } from '@/shared/hooks/use-editor-analytics'
import { useProjectSettingsContext } from '@/features/ide-settings/context/project-settings-context'
import {
  createSegmenters,
  type Segmenters,
} from '@/features/word-count-modal/utils/segmenters'
import { countWordsInSelection } from '@/features/word-count-modal/utils/count-words-in-selection'
import { getPendingSelection } from '@/features/word-count-modal/utils/word-count-selection'
import { debugConsole } from '@/utils/debugging'

/**
 * 2026-09 (OlliTeX, owner batch: selected-text word count): the Selection
 * section. Shown ONLY when the File → Word count action captured a non-empty
 * editor selection; the whole-document count above always remains.
 *
 * - LaTeX: the lezer-based countWordsInSelection (ported from the community
 *   selected-word-count contribution; see CREDITS.md) — words, headings,
 *   inline/display math.
 * - Typst: no lezer grammar here — plain word segmenting of the selection
 *   with `//` comments removed (documented approximation).
 */
function countTypstSelectionWords(
  text: string,
  segmenters: Segmenters | null,
): number {
  // strip full-line `//` comments before counting (documented approximation —
  // there is no Typst lezer grammar in this tree for a precise count)
  const withoutComments = text.replace(/^\s*\/\/.*$/gm, '')
  if (segmenters) {
    try {
      let n = 0
      // join hyphenated words so they segment as one word, matching the LaTeX
      // counter's treatment
      for (const value of segmenters.word.segment(
        withoutComments.replace(/\w[-_]\w/g, 'aaa'),
      )) {
        if (value.isWordLike) n++
      }
      return n
    } catch {
      // fall through to the regex count
    }
  }
  const m = withoutComments.match(/\S+/g)
  return m ? m.length : 0
}

function SelectionCountSection() {
  const { t } = useTranslation()
  const { spellCheckLanguage } = useProjectSettingsContext()
  const selection = getPendingSelection()

  const data = useMemo(() => {
    if (!selection) return null
    // createSegmenters always returns a usable object (it falls back to a
    // generic segmenter when Intl.Segmenter is unavailable), so no null handling
    const segmenters = createSegmenters(spellCheckLanguage?.replace(/_/, '-'))
    if (selection.isTypst) {
      return {
        type: 'typst' as const,
        words: countTypstSelectionWords(selection.text, segmenters),
      }
    }
    try {
      // count the selection within its own document text — the utility
      // parses full content and counts only the supplied range
      const full = selection.text
      const result = countWordsInSelection(
        full,
        { from: 0, to: full.length },
        segmenters,
      )
      return { type: 'latex' as const, ...result }
    } catch (error) {
      debugConsole.error(error)
      return null
    }
  }, [selection, spellCheckLanguage])

  if (!selection || !data) return null

  const rows =
    data.type === 'latex'
      ? [
          {
            key: 'words',
            label: t('wc_selection_words'),
            value: data.totalWords,
          },
          {
            key: 'headers',
            label: t('wc_selection_headers'),
            value: data.headers,
          },
          {
            key: 'math-inline',
            label: t('wc_selection_math_inline'),
            value: data.mathInline,
          },
          {
            key: 'math-display',
            label: t('wc_selection_math_display'),
            value: data.mathDisplay,
          },
        ]
      : [{ key: 'words', label: t('wc_selection_words'), value: data.words }]

  return (
    <div
      data-testid="word-count-selection"
      className="word-count-selection"
      style={{ marginBottom: 8 }}
    >
      <div style={{ fontWeight: 600, marginBottom: 4 }}>
        {t('wc_selection')}
      </div>
      {data.type === 'typst' && (
        <div style={{ fontSize: '0.85em', opacity: 0.75, marginBottom: 4 }}>
          {t('wc_selection_typst_hint')}
        </div>
      )}
      {rows.map(row => (
        <div
          key={row.key}
          className="float-row"
          data-testid={`word-count-selection-${row.key}`}
          style={{ display: 'flex', justifyContent: 'space-between' }}
        >
          <span>{row.label}:</span>
          <span>{new Intl.NumberFormat().format(row.value)}</span>
        </div>
      ))}
    </div>
  )
}

// NOTE: this component is only mounted when the modal is open
export default function WordCountModalContent({
  handleHide,
}: {
  handleHide: () => void
}) {
  const { t } = useTranslation()

  const { sendEvent } = useEditorAnalytics()

  useEffect(() => {
    // record when the word count modal is opened
    sendEvent('word-count-opened', {
      // 2026-09 (OlliTeX): the document count is now always the client-side,
      // current-file count (see WordCountClient), so mode is fixed.
      mode: 'client',
    })
  }, [sendEvent])

  return (
    <>
      <OLModalHeader>
        <OLModalTitle>
          {t('word_count_lower')}{' '}
          <SplitTestBadge
            splitTestName="word-count-client"
            displayOnVariants={['enabled']}
          />
        </OLModalTitle>
      </OLModalHeader>

      <OLModalBody>
        {/*
          2026-09 (OlliTeX, File → Word count scoping): the document count is
          always the CURRENT file (the one open in the editor), computed
          client-side. The old default rendered the server texcount figure,
          which counted the whole project (main doc + every \input) regardless
          of which file was open — the user wanted the current file instead.
        */}
        <WordCountClient />
        {getPendingSelection() ? (
          <div
            style={{
              marginTop: 8,
              borderTop: '1px solid var(--border-divider-themed, #d0d5dd)',
            }}
          >
            <SelectionCountSection />
          </div>
        ) : null}
      </OLModalBody>

      <OLModalFooter>
        <OLButton variant="secondary" onClick={handleHide}>
          {t('close')}
        </OLButton>
      </OLModalFooter>
    </>
  )
}
