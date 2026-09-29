import React, { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useIdeReactContext } from '@/features/ide-react/context/ide-react-context'
import { postJSON, getJSON } from '@/infrastructure/fetch-json'
// Resolve the project's root documents (id + path), walking rootFolder
// exactly like the Go feature's entity walker.
export async function resolveEntryDocs(projectId: string): Promise<string[]> {
  try {
    const project = await getJSON<any>(`/project/${projectId}`)
    const out: string[] = []
    const walk = (folder: any): void => {
      if (!folder) {
        return
      }
      for (const d of folder.docs ?? []) {
        if (d?._id && /\.(tex|typ)$/.test(d?.name ?? '')) {
          out.push(d._id)
        }
      }
      for (const f of folder.folders ?? []) {
        walk(f)
      }
    }
    walk(project?.rootFolder?.[0])
    return out.slice(0, 50)
  } catch {
    return []
  }
}

// OlliTeX — Project inspection panel (candidate G, owner-adopted
// 2026-09-29).
//
// Rail panel (mainEditorLayoutPanels) exposing the whole-project TeX
// dependency analysis (reference: the project-inspection module of
// yu-i-i/overleaf-cep#245, AGPL-3.0 — engine vendored + oracle-pinned in
// services/web/modules/project-inspection). Calls the Go web endpoint
// POST /project/:id/project-inspection/analyze (see
// go/services/web/features/projectinspection).

type Issue = {
  id?: string
  title?: string
  target?: string
  status?: string
  type?: string
  nodeIds?: string[]
}

type CitationView = {
  missing: unknown[]
  unused: unknown[]
  duplicate: unknown[]
}

type InspectionResult = {
  schemaVersion: number
  analysisId: string
  analyzedAt: string
  entryPoints: unknown[]
  overview: {
    fileCount: number
    figureCount: number
    tableCount: number
    citationCount: number
    missing: number
    unusedUnreferenced: number
    duplicate: number
    circular: number
  }
  issues: { byId: Record<string, Issue>; truncated: boolean }
  views: {
    missing: string[]
    unused: string[]
    duplicate: string[]
    circular: string[]
    citation: CitationView
  }
  coverage: {
    parseErrors: unknown[]
    truncated: boolean
  }
}

function viewItems(ids: string[], issueBy: Record<string, Issue>): string[] {
  const out: string[] = []
  for (const id of ids ?? []) {
    const issue = issueBy[id]
    const title = issue?.title ?? id
    const target = issue?.target && issue.target !== title ? ` \u2192 ${issue.target}` : ''
    out.push(title + target)
  }
  return out
}

export default function ProjectInspectionPanel(_props: { order?: number }) {
  const { projectId } = useIdeReactContext()
  const { t } = useTranslation()
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<InspectionResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [elapsed, setElapsed] = useState<number | null>(null)

  const analyze = useCallback(async () => {
    setRunning(true)
    setError(null)
    const started = Date.now()
    try {
      // entry points: the project's root-level documents (.tex/.typ) —
      // resolved from the standard project JSON (same rootFolder model the
      // Go reader consumes); the server re-validates every id.
      const docIds: string[] = await resolveEntryDocs(projectId)
      if (docIds.length === 0) {
        throw { status: 400, error: 'INVALID_ENTRY_POINT' }
      }
      const res = await postJSON<InspectionResult>(
        `/project/${projectId}/project-inspection/analyze`,
        { body: { entryPointIds: docIds } },
      )
      if (res && res.schemaVersion === 1) {
        setResult(res)
      } else {
        setError(t('project_inspection_error_generic'))
      }
      setElapsed(Date.now() - started)
    } catch (e: unknown) {
      const status = e && typeof e === 'object' && 'status' in e
        ? (e as { status: number }).status
        : null
      const code = e && typeof e === 'object' && 'error' in e
        ? String((e as { error: unknown }).error)
        : ''
      let msg: string
      if (code === 'INVALID_ENTRY_POINT' || status === 400) {
        msg = t('project_inspection_error_invalid_entry')
      } else if (status === 413) {
        msg = t('project_inspection_error_too_large')
      } else if (status === 504) {
        msg = t('project_inspection_error_timeout')
      } else if (status === 499) {
        msg = t('project_inspection_error_cancelled')
      } else {
        msg = t('project_inspection_error_generic')
      }
      setError(msg)
      setElapsed(Date.now() - started)
    } finally {
      setRunning(false)
    }
  }, [projectId, t])

  const ov = result?.overview
  const missing = viewItems(result?.views?.missing ?? [], result?.issues?.byId ?? {})
  const unused = viewItems(result?.views?.unused ?? [], result?.issues?.byId ?? {})
  const duplicate = viewItems(result?.views?.duplicate ?? [], result?.issues?.byId ?? {})
  const circular = viewItems(result?.views?.circular ?? [], result?.issues?.byId ?? {})
  const citation = result?.views?.citation ?? { missing: [], unused: [], duplicate: [] }

  return (
    <div
      data-testid='project-inspection-panel'
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
        padding: 12,
        fontSize: 12.5,
        overflow: 'auto',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <strong style={{ fontSize: 13 }}>{t('project_inspection_title')}</strong>
        <button
          type='button'
          disabled={running}
          onClick={analyze}
          style={{
            marginLeft: 'auto',
            padding: '4px 12px',
            cursor: running ? 'progress' : 'pointer',
          }}
        >
          {running
            ? t('project_inspection_analyzing')
            : result
              ? t('project_inspection_analyze')
              : t('project_inspection_analyze')}
        </button>
      </div>

      {running && (
        <div data-testid='pi-running' style={{ opacity: 0.7 }}>
          {t('project_inspection_analyzing')}
        </div>
      )}

      {error && (
        <div
          data-testid='pi-error'
          role='alert'
          style={{ padding: 8, background: 'rgba(220,60,60,0.12)', borderRadius: 6 }}
        >
          {error}
        </div>
      )}

      {!running && result && !error && (
        <>
          <div data-testid='pi-overview' style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 4 }}>
            <span>{t('project_inspection_files')}: {ov?.fileCount}</span>
            <span>{t('project_inspection_citations')}: {ov?.citationCount}</span>
            <span>{t('project_inspection_figures')}: {ov?.figureCount}</span>
            <span>{t('project_inspection_tables')}: {ov?.tableCount}</span>
          </div>

          {missing.length > 0 && (
            <section data-testid='pi-missing'>
              <h4 style={{ margin: '0 0 4px' }}>
                {t('project_inspection_missing')} ({missing.length})
              </h4>
              <ul style={{ margin: 0, paddingLeft: 18 }}>
                {missing.map((m) => (
                  <li key={m}>{m}</li>
                ))}
              </ul>
            </section>
          )}
          {unused.length > 0 && (
            <section data-testid='pi-unused'>
              <h4 style={{ margin: '0 0 4px' }}>
                {t('project_inspection_unused')} ({unused.length})
              </h4>
              <ul style={{ margin: 0, paddingLeft: 18 }}>
                {unused.map((m) => (
                  <li key={m}>{m}</li>
                ))}
              </ul>
            </section>
          )}
          {duplicate.length > 0 && (
            <section data-testid='pi-duplicate'>
              <h4 style={{ margin: '0 0 4px' }}>
                {t('project_inspection_duplicate')} ({duplicate.length})
              </h4>
              <ul style={{ margin: 0, paddingLeft: 18 }}>
                {duplicate.map((m) => (
                  <li key={m}>{m}</li>
                ))}
              </ul>
            </section>
          )}
          {circular.length > 0 && (
            <section data-testid='pi-circular'>
              <h4 style={{ margin: '0 0 4px' }}>
                {t('project_inspection_circular')} ({circular.length})
              </h4>
              <ul style={{ margin: 0, paddingLeft: 18 }}>
                {circular.map((m) => (
                  <li key={m}>{m}</li>
                ))}
              </ul>
            </section>
          )}
          {(citation.missing?.length || citation.unused?.length || citation.duplicate?.length) ? (
            <section data-testid='pi-citation'>
              <h4 style={{ margin: '0 0 4px' }}>{t('project_inspection_citations')}</h4>
              <ul style={{ margin: 0, paddingLeft: 18 }}>
                {Array.from({ length: citation.missing.length })
                  .map((_, i) => (
                    <li key={'m' + i}>
                      {t('project_inspection_citation_missing')}: {JSON.stringify(citation.missing[i])}
                    </li>
                  ))}
              </ul>
            </section>
          ) : null}

          <div data-testid='pi-meta' style={{ opacity: 0.65, fontSize: 11 }}>
            {result.analysisId} · {result.analyzedAt}
            {elapsed != null ? ` · ${Math.round(elapsed / 1000 / 10)}s` : ''}
          </div>
        </>
      )}
    </div>
  )
}
