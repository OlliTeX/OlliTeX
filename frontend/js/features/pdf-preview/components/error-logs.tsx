import { useTranslation } from 'react-i18next'
import { memo, useCallback, useMemo, useState } from 'react'
import { usePdfPreviewContext } from '@/features/pdf-preview/components/pdf-preview-provider'
import StopOnFirstErrorPrompt from '@/features/pdf-preview/components/stop-on-first-error-prompt'
import PdfPreviewError from '@/features/pdf-preview/components/pdf-preview-error'
import PdfValidationIssue from '@/features/pdf-preview/components/pdf-validation-issue'
import PdfLogsEntries from '@/features/pdf-preview/components/pdf-logs-entries'
import PdfPreviewErrorBoundaryFallback from '@/features/pdf-preview/components/pdf-preview-error-boundary-fallback'
import withErrorBoundary from '@/infrastructure/error-boundary'
import { useDetachCompileContext as useCompileContext } from '@/shared/context/detach-compile-context'
import { LogEntry as LogEntryData } from '@/features/pdf-preview/util/types'
import LogEntry from './log-entry'
import PdfClearCacheButton from '@/features/pdf-preview/components/pdf-clear-cache-button'
import PdfDownloadFilesButton from '@/features/pdf-preview/components/pdf-download-files-button'
import RollingBuildSelectedReminder from './rolling-build-selected-reminder'
import CheckpointCompilesEnabledReminder from './checkpoint-compiles-enabled-reminder'

type ErrorLogTab = {
  key: string
  label: string
  entries: LogEntryData[] | undefined
}

function ErrorLogs({
  includeActionButtons,
}: {
  includeActionButtons?: boolean
}) {
  const { error, logEntries, rawLog, validationIssues, stoppedOnFirstError } =
    useCompileContext()
  const { t } = useTranslation()

  const tabs = useMemo(() => {
    return [
      {
        key: 'all',
        label: t('all_logs'),
        entries: logEntries?.all,
      },
      { key: 'errors', label: t('errors'), entries: logEntries?.errors },
      { key: 'warnings', label: t('warnings'), entries: logEntries?.warnings },
      { key: 'info', label: t('info'), entries: logEntries?.typesetting },
    ]
  }, [logEntries, t])

  const { loadingError } = usePdfPreviewContext()

  const [activeTab, setActiveTab] = useState<string | null>('all')

  const changeTab = useCallback(
    (key: string | null) => {
      if (tabs.some(tab => tab.key === key)) {
        setActiveTab(key)
      }
    },
    [tabs]
  )

  const entries = useMemo(() => {
    return tabs.find(tab => tab.key === activeTab)?.entries || []
  }, [activeTab, tabs])

  const includeErrors = activeTab === 'all' || activeTab === 'errors'
  const includeWarnings = activeTab === 'all' || activeTab === 'warnings'

  return (
    // 2026-09-12 (a11y fix, green gate): replaced the react-bootstrap
    // TabContainer + Nav + TabContent combo with explicit tab ARIA. RB 2.x's
    // Nav/NavItem emits role="tab" + aria-controls pointing at pane ids that
    // its (v1-era) TabContent never renders — 42 dangling aria-controls
    // idrefs (axe aria-valid-attr-value, critical) on every editor page.
    // Same classes as before (logs.scss keeps styling it); one real
    // tabpanel that always renders and follows the selected tab.
    <div className="error-logs">
      <div role="tablist" className="error-logs-tabs">
        {tabs.map(tab => {
          const active = activeTab === tab.key
          return (
            <button
              key={tab.key}
              type="button"
              id={'error-logs-tab-' + tab.key}
              role="tab"
              aria-selected={active}
              aria-controls="error-logs-tabpanel"
              className={'error-logs-tab-header' + (active ? ' active' : '')}
              onClick={() => changeTab(tab.key)}
            >
              {tab.label}
              <div className="error-logs-tab-count">
                {/* TODO: it would be nice if this number included custom errors */}
                {formatErrorNumber(tab.entries?.length)}
              </div>
            </button>
          )
        })}
      </div>
      <div
        role="tabpanel"
        id="error-logs-tabpanel"
        aria-labelledby={'error-logs-tab-' + (activeTab ?? 'all')}
        tabIndex={0}
        className="error-logs new-error-logs tab-content"
      >
        <div className="logs-pane-content">
          <RollingBuildSelectedReminder />
          <CheckpointCompilesEnabledReminder />
          {stoppedOnFirstError && includeErrors && <StopOnFirstErrorPrompt />}

          {loadingError && (
            <PdfPreviewError
              error="pdf-viewer-loading-error"
              includeErrors={includeErrors}
              includeWarnings={includeWarnings}
            />
          )}

          {error && <PdfPreviewError error={error} />}

          {includeErrors &&
            validationIssues &&
            Object.entries(validationIssues).map(([name, issue]) => (
              <PdfValidationIssue key={name} name={name} issue={issue} />
            ))}

          {entries && (
            <PdfLogsEntries
              entries={entries}
              hasErrors={
                includeErrors &&
                logEntries?.errors &&
                logEntries?.errors.length > 0
              }
            />
          )}

          {rawLog && activeTab === 'all' && (
            <LogEntry
              headerTitle={t('raw_logs')}
              rawContent={rawLog}
              entryAriaLabel={t('raw_logs_description')}
              level="raw"
              alwaysExpandRawContent
              showSourceLocationLink={false}
            />
          )}

          {includeActionButtons && (
            <div className="logs-pane-actions">
              <PdfClearCacheButton />
              <PdfDownloadFilesButton />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function formatErrorNumber(num: number | undefined) {
  if (num === undefined) {
    return undefined
  }

  if (num > 99) {
    return '99+'
  }

  return Math.floor(num).toString()
}

// (2026-09-12) TabHeader helper removed with the react-bootstrap tabs:
// the tab buttons are now rendered inline above (proper tab ARIA, no
// dangling aria-controls).

export default withErrorBoundary(memo(ErrorLogs), () => (
  <PdfPreviewErrorBoundaryFallback type="logs" />
))
