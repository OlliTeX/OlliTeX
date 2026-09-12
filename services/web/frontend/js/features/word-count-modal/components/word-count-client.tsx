import { FC, useEffect, useMemo, useState } from 'react'
import { WordCountData } from '@/features/word-count-modal/components/word-count-data'
import { WordCountError } from '@/features/word-count-modal/components/word-count-error'
import { useProjectContext } from '@/shared/context/project-context'
import useAbortController from '@/shared/hooks/use-abort-controller'
import { useProjectSettingsContext } from '@/features/ide-settings/context/project-settings-context'
import { useEditorManagerContext } from '@/features/ide-react/context/editor-manager-context'
import { useEditorOpenDocContext } from '@/features/ide-react/context/editor-open-doc-context'
import { useFileTreePathContext } from '@/features/file-tree/contexts/file-tree-path'
import { debugConsole } from '@/utils/debugging'
import { signalWithTimeout } from '@/utils/abort-signal'
import { countWordsInFile } from '@/features/word-count-modal/utils/count-words-in-file'
import { createSegmenters } from '@/features/word-count-modal/utils/segmenters'
import { WordCountsClient } from './word-counts-client'
import LoadingSpinner from '@/shared/components/loading-spinner'

export const WordCountClient: FC = () => {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [data, setData] = useState<WordCountData | null>(null)
  const { projectSnapshot } = useProjectContext()
  const { spellCheckLanguage } = useProjectSettingsContext()
  const { openDocs } = useEditorManagerContext()
  const { currentDocument } = useEditorOpenDocContext()
  const { pathInFolder } = useFileTreePathContext()

  const { signal } = useAbortController()

  const segmenters = useMemo(() => {
    return createSegmenters(spellCheckLanguage?.replace(/_/, '-'))
  }, [spellCheckLanguage])

  useEffect(() => {
    if (currentDocument && segmenters) {
      const countWords = async () => {
        await openDocs.awaitBufferedOps(signalWithTimeout(signal, 5000))
        await projectSnapshot.refresh()

        if (signal.aborted) return null

        // 2026-09 (OlliTeX, File → Word count scoping): count the file the user
        // currently has OPEN in the editor — the "current file" — not the whole
        // project. The previous behaviour anchored the count on the main/root
        // document and walked every \input/\include, so it always reported the
        // project's word count regardless of which file was open. We anchor on
        // the open doc and do NOT follow its includes into the rest of the
        // project (includeIncludedFiles = false below), which is also how the
        // Selection section already counts (standalone parse, no include walk).
        const currentDocId = currentDocument.doc_id
        if (!currentDocId) return null

        const currentDocPath = pathInFolder(currentDocId)
        if (!currentDocPath) return null

        const data: WordCountData = {
          encode: 'ascii',
          textWords: 0,
          textCharacters: 0,
          headWords: 0,
          headCharacters: 0,
          abstractWords: 0,
          abstractCharacters: 0,
          captionWords: 0,
          captionCharacters: 0,
          footnoteWords: 0,
          footnoteCharacters: 0,
          outside: 0,
          otherWords: 0,
          otherCharacters: 0,
          headers: 0,
          elements: 0,
          mathInline: 0,
          mathDisplay: 0,
          errors: 0,
          messages: '',
        }

        countWordsInFile(
          data,
          projectSnapshot,
          currentDocPath,
          '/',
          segmenters,
          false,
        )

        return data
      }

      countWords()
        .then(data => {
          if (data) {
            setData(data)
          } else {
            // no resolvable current file (e.g. a non-document tab) — surface
            // the error state rather than spinning forever or rendering blank
            debugConsole.warn('word-count: no current file to count')
            setError(true)
          }
        })
        .catch(error => {
          debugConsole.error(error)
          setError(true)
        })
        .finally(() => {
          setLoading(false)
        })
    } else {
      // 2026-09 (OlliTeX, File → Word count scoping): the count is anchored on
      // the file open in the editor; when there is none, resolve the loading
      // state with the error view instead of an infinite spinner.
      debugConsole.warn('word-count: no open document available to count')
      setError(true)
      setLoading(false)
    }
  }, [
    signal,
    openDocs,
    projectSnapshot,
    segmenters,
    currentDocument,
    pathInFolder,
  ])

  return (
    <>
      {loading && !error && <LoadingSpinner />}
      {error && <WordCountError />}
      {data && <WordCountsClient data={data} />}
    </>
  )
}
