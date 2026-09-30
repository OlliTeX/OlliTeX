import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { getJSON, putJSON, deleteJSON } from '@/infrastructure/fetch-json'
import useAsync from '@/shared/hooks/use-async'
import OLButton from '@/shared/components/ol/ol-button'
import { TextInput } from '@mantine/core'
import {
  OLModal,
  OLModalBody,
  OLModalFooter,
} from '@/shared/components/ol/ol-modal'
import getMeta from '@/utils/meta'

// WakaTime / Wakapi account card (candidate F — owner-adopted 2026-09-29;
// port of the reference `wakatime-integration-card`): connect with the
// user's OWN API key, show recent project time, disconnect.
//
// Security path: the browser talks only to the OlliTeX Go web routes
// (/user/wakatime, /project/:id/wakatime/*); the Go web holds the encrypted
// key and relays to the user's own WakaTime/Wakapi endpoint. The key never
// reaches wakatime.com from the browser and never appears in logs.

function getMetaValue(name: string): string | null {
  return (
    document.head.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
      ?.getAttribute('content') ?? null
  )
}

function timeAgo(seconds: number): string {
  if (seconds < 60) {
    return `${seconds}s`
  }
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)}m`
  }
  return `${Math.floor(seconds / 3600)}h ${Math.floor(seconds % 3600 / 60)}m`
}

export function WakatimeCard() {
  const { t } = useTranslation()
  const projectId = getMetaValue('ol-project_id')
  const [showLinkModal, setShowLinkModal] = useState(false)
  const [apiKey, setApiKey] = useState('')
  const [apiUrl, setApiUrl] = useState('https://wakatime.com/api/v1')
  const [error, setError] = useState<string | null>(null)
  const [summary, setSummary] = useState<{
    connected?: boolean
    totalSeconds?: number
    rangeDays?: number
  }>()

  const {
    isLoading: statusLoading,
    runAsync,
    data: statusData,
  } = useAsync<{ connected?: boolean; apiUrl?: string; error?: boolean }>()

  const refresh = (silent = false) =>
    runAsync(getJSON('/user/wakatime/status'))
      .then(res => {
        if (res?.connected && projectId) {
          getJSON(`/project/${projectId}/wakatime/summary`)
            .then(s => setSummary(s))
            .catch(() => setSummary(undefined))
        }
        return res
      })
      .catch((err: unknown) => {
        if (!silent) {
          const msg =
            (err as { data?: { message?: string } })?.data?.message ??
            (err as Error)?.message
          if (msg) {
            setError(msg)
          }
        }
        throw err
      })

  useEffect(() => {
    refresh(true).catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId])

  const { wakaTimeEnabled } = getMeta('ol-ExposedSettings') as {
    wakaTimeEnabled?: boolean
  }
  if (!wakaTimeEnabled) {
    // instance-level opt-out (owner directive: off-by-default)
    return null
  }

  const connected = !!statusData?.connected

  const onLink = () => {
    setError(null)
    if (!apiKey.trim()) {
      setError(t('wakatime_error_api_key_required'))
      return
    }
    runAsync(
      putJSON('/user/wakatime', {
        body: {
          apiUrl: apiUrl.trim() || 'https://wakatime.com/api/v1',
          apiKey: apiKey.trim(),
        },
      }),
    )
      .then(() => {
        setShowLinkModal(false)
        setApiKey('')
        return refresh(true)
      })
      .catch((err: unknown) => {
        const msg =
          (err as { data?: { message?: string } })?.data?.message ??
          t('wakatime_error_link')
        setError(msg)
      })
  }

  const onUnlink = () => {
    runAsync(deleteJSON('/user/wakatime'))
      .then(() => {
        setSummary(undefined)
        return runAsync(getJSON('/user/wakatime/status'))
      })
      .catch(() => undefined)
  }

  const totalSeconds = summary?.connected ? (summary?.totalSeconds ?? 0) : null

  return (
    <div style={{ border: '1px solid #d7dce0', borderRadius: 4, padding: 16, background: '#fff' }}>
      <h4 style={{ margin: 0, fontSize: 16, fontWeight: 600 }}>{t('wakatime_title')}</h4>
      <p style={{ marginTop: 8, marginBottom: 12, color: '#525659', fontSize: 13, lineHeight: 1.5 }}>
        {t('wakatime_description')}
      </p>

      {statusLoading ? (
        <p style={{ fontSize: 13 }}>{t('wakatime_status_loading')}</p>
      ) : connected ? (
        <>
          <p style={{ fontSize: 13, margin: '0 0 8px 0' }}>
            {t('wakatime_connected', { url: statusData?.apiUrl ?? '' })}
          </p>
          {totalSeconds !== null ? (
            <p style={{ fontSize: 13, margin: '0 0 12px 0' }}>
              {t('wakatime_project_time', {
                time: timeAgo(totalSeconds),
                days: summary?.rangeDays ?? 7,
              })}
            </p>
          ) : (
            <p style={{ fontSize: 13, margin: '0 0 12px 0', color: '#787d80' }}>
              {t('wakatime_no_data')}
            </p>
          )}
          <OLButton variant="danger" onClick={onUnlink}>
            {t('wakatime_disconnect')}
          </OLButton>
        </>
      ) : (
        <>
          <p style={{ fontSize: 13, margin: '0 0 12px 0', color: '#787d80' }}>
            {t('wakatime_not_connected')}
          </p>
          <OLButton variant="primary" onClick={() => setShowLinkModal(true)}>
            {t('wakatime_connect')}
          </OLButton>
        </>
      )}

      {showLinkModal && (
        <OLModal
          show={showLinkModal}
          header={t('wakatime_connect_title')}
          onHide={() => setShowLinkModal(false)}
        >
          <OLModalBody>
            <p style={{ fontSize: 13, margin: '0 0 12px 0', color: '#525659' }}>
              {t('wakatime_connect_help')}
            </p>
            <label htmlFor="waka-apiurl" style={{ display: 'block', marginBottom: 12 }}>
              <span style={{ fontSize: 13, fontWeight: 600, display: 'block', marginBottom: 4 }}>
                {t('wakatime_field_api_url')}
              </span>
              <TextInput
                id="waka-apiurl"
                value={apiUrl}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => setApiUrl(e.target.value)}
                placeholder="https://wakatime.com/api/v1"
              />
            </label>
            <label htmlFor="waka-apikey" style={{ display: 'block', marginBottom: 4 }}>
              <span style={{ fontSize: 13, fontWeight: 600, display: 'block', marginBottom: 4 }}>
                {t('wakatime_field_api_key')}
              </span>
              <TextInput
                id="waka-apikey"
                value={apiKey}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => setApiKey(e.target.value)}
                placeholder={t('wakatime_field_api_key_placeholder')}
                type="password"
              />
            </label>
            {error && (
              <p style={{ color: '#ba265e', fontSize: 13, marginTop: 8 }}>{error}</p>
            )}
            <p style={{ fontSize: 12, marginTop: 10, color: '#787d80' }}>
              {t('wakatime_privacy_note')}
            </p>
          </OLModalBody>
          <OLModalFooter>
            <OLButton variant="secondary" onClick={() => setShowLinkModal(false)}>
              {t('wakatime_cancel')}
            </OLButton>
            <OLButton variant="primary" onClick={onLink}>
              {t('wakatime_connect')}
            </OLButton>
          </OLModalFooter>
        </OLModal>
      )}
    </div>
  )
}

export default WakatimeCard
