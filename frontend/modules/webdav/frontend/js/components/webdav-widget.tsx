import { useCallback, useEffect, useState } from 'react'
import type { CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import OLButton from '@/shared/components/ol/ol-button'
import OLNotification from '@/shared/components/notification' // 6.3.0 legacy Notification provides the OLNotification surface
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import { debugConsole } from '@/utils/debugging'
import {
  Alert,
  Button,
  PasswordInput,
  TextInput,
} from '@mantine/core'
import {
  canUseMantineSurface,
  useEditorUiVariant,
} from '@/features/editor-v2/variant'

type WebdavStatus = {
  connected: boolean
  baseUrl?: string
  rootPath?: string
  lastSyncAt?: string | null
  lastSyncError?: string | null
  lastConflict?: {
    projectId: string
    path?: string | null
    detectedAt: string
  } | null
}

type WebdavForm = {
  baseUrl: string
  username: string
  password: string
  rootPath: string
}

function logWebdavError(operation: string, error: any) {
  const serverMessage =
    typeof error?.data?.message === 'string'
      ? error.data.message
      : error?.data?.message?.text
  debugConsole.error(`[WebDAV] ${operation} failed`, {
    message: serverMessage || error?.message,
    status: error?.response?.status,
    statusText: error?.response?.statusText,
    method: error?.options?.method,
    url: error?.url,
  })
}

export default function WebdavWidget() {
  const { t } = useTranslation()
  // M1 module Mantine wave: variant gate (hook — must run before any
  // early return): /editor renders the Mantine card body once the
  // editor's OlliT shell is ready; /Project keeps the legacy markup.
  const uiCtx = useEditorUiVariant()
  const mantine = canUseMantineSurface(uiCtx)
  const [status, setStatus] = useState<WebdavStatus>()
  const [form, setForm] = useState<WebdavForm>({
    baseUrl: '',
    username: '',
    password: '',
    rootPath: '/Overleaf',
  })
  const [loading, setLoading] = useState(true)
  const [working, setWorking] = useState(false)
  const [error, setError] = useState<string>()

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const nextStatus = await getJSON<WebdavStatus>('/user/webdav/status')
      setStatus(nextStatus)
      if (nextStatus.lastSyncError) {
        debugConsole.error('[WebDAV] server reported a sync error', {
          message: nextStatus.lastSyncError,
          conflict: nextStatus.lastConflict || undefined,
          lastSyncAt: nextStatus.lastSyncAt || undefined,
        })
      }
      if (nextStatus.connected) {
        setForm(current => ({
          ...current,
          baseUrl: nextStatus.baseUrl || current.baseUrl,
          rootPath: nextStatus.rootPath || current.rootPath,
        }))
      }
    } catch (refreshError) {
      logWebdavError('status refresh', refreshError)
      setError(t('generic_something_went_wrong'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    refresh()
  }, [refresh])

  const updateField = (field: keyof WebdavForm, value: string) => {
    setForm(current => ({ ...current, [field]: value }))
  }

  // Poll is no longer available in user settings - sync via project pages
  // Note: resolve conflict is handled directly from the webdav-sync-modal

  const disconnect = async () => {
    setWorking(true)
    setError(undefined)
    try {
      await postJSON('/user/webdav/disconnect')
      setStatus({ connected: false })
      setForm(current => ({
        ...current,
        baseUrl: '',
        username: '',
        password: '',
        rootPath: '/Overleaf',
      }))
    } catch (disconnectError: any) {
      logWebdavError('disconnect', disconnectError)
      setError(disconnectError?.data?.message || disconnectError?.message || t('generic_something_went_wrong'))
    } finally {
      setWorking(false)
    }
  }

  const connect = async () => {
    setWorking(true)
    setError(undefined)
    try {
      await postJSON('/user/webdav/connect', { body: form })
      await refresh()
    } catch (connectError: any) {
      logWebdavError('connect', connectError)
      setError(connectError?.data?.message || connectError?.message || t('generic_something_went_wrong'))
    } finally {
      setWorking(false)
    }
  }

  if (loading) {
    return (
      <div className="settings-widget-container">
        <div className="d-none d-md-block" aria-hidden="true" />
        <div className="description-container">
          <h4>{t('webdav')}</h4>
          <p className="small">{t('loading')}...</p>
        </div>
      </div>
    )
  }

  // M1 module Mantine wave: the outer DOM (container, heading, ids) is
  // identical in both branches so the e2e matrix and the a11y tree do
  // not move.
  const legacyBody = (
    <>
      {error && <OLNotification type="error" content={error} />}
      {status?.lastSyncError && (
        <OLNotification type="error" content={status.lastSyncError} />
      )}
      {status?.connected ? (
        <>
          <p className="small">
            {t('connected_to')} <strong>{status.baseUrl}</strong>.
          </p>
          {/* Sync buttons moved to project pages */}
          <p className="small text-muted mb-2">
            {t('webdav_sync_hint')}
          </p>
          <OLButton
            variant="danger-ghost"
            onClick={disconnect}
            disabled={working}
          >
            {t('webdav_disconnect')}
          </OLButton>
          <p className="small text-muted">{t('webdav_unlink_note')}</p>
        </>
      ) : (
        // a11y (Chromium heuristic): a password field must live inside a
        // real <form>; submit is the existing connect handler.
        <form
          onSubmit={event => {
            event.preventDefault()
            if (!working && form.baseUrl && form.username && form.password) {
              void connect()
            }
          }}
        >
          <label className="form-label" htmlFor="webdav-base-url">{t('webdav_base_url_label')}</label>
          <p className="small form-text">{t('webdav_base_url_description')}</p>
          <input
            id="webdav-base-url"
            className="form-control mb-2"
            value={form.baseUrl}
            onChange={event => updateField('baseUrl', event.target.value)}
            placeholder={t('webdav_server_url_placeholder')}
            type="url"
            required
          />
          <label className="form-label" htmlFor="webdav-username">{t('webdav_username_label')}</label>
          <input
            id="webdav-username"
            className="form-control mb-2"
            value={form.username}
            onChange={event => updateField('username', event.target.value)}
            autoComplete="username"
            required
          />
          <label className="form-label" htmlFor="webdav-password">{t('webdav_password_label')}</label>
          <input
            id="webdav-password"
            className="form-control mb-2"
            value={form.password}
            onChange={event => updateField('password', event.target.value)}
            type="password"
            autoComplete="current-password"
            required
          />
          <label className="form-label" htmlFor="webdav-root-path">{t('webdav_remote_root_label')}</label>
          <input
            id="webdav-root-path"
            className="form-control mb-2"
            value={form.rootPath}
            onChange={event => updateField('rootPath', event.target.value)}
            required
          />
          <OLButton
            variant="secondary"
            onClick={connect}
            disabled={working || !form.baseUrl || !form.username || !form.password}
          >
            {working ? t('loading') : t('webdav_connect')}
          </OLButton>
        </form>
      )}
    </>
  )

  const formStyle: CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    gap: '10px',
    width: '100%',
  }

  const mantineBody = (
    <uiCtx.Provider>
      {error && <Alert color="red" variant="light">{error}</Alert>}
      {status?.lastSyncError && (
        <Alert color="red" variant="light">
          {status.lastSyncError}
        </Alert>
      )}
      {status?.connected ? (
        <>
          <p className="small">
            {t('connected_to')} <strong>{status.baseUrl}</strong>.
          </p>
          <p className="small text-muted mb-2">{t('webdav_sync_hint')}</p>
          <Button
            color="red"
            variant="subtle"
            size="xs"
            disabled={working}
            onClick={() => void disconnect()}
          >
            {t('webdav_disconnect')}
          </Button>
          <p className="small text-muted">{t('webdav_unlink_note')}</p>
        </>
      ) : (
        // keep the real <form> (password-field a11y heuristic) in both branches
        <form
          style={formStyle}
          onSubmit={event => {
            event.preventDefault()
            if (!working && form.baseUrl && form.username && form.password) {
              void connect()
            }
          }}
        >
          <TextInput
            id="webdav-base-url"
            size="sm"
            type="url"
            required
            label={t('webdav_base_url_label')}
            description={t('webdav_base_url_description')}
            placeholder={t('webdav_server_url_placeholder')}
            value={form.baseUrl}
            onChange={value => updateField('baseUrl', value)}
          />
          <TextInput
            id="webdav-username"
            size="sm"
            required
            aria-label={t('webdav_username_label')}
            label={t('webdav_username_label')}
            autoComplete="username"
            value={form.username}
            onChange={value => updateField('username', value)}
          />
          <PasswordInput
            id="webdav-password"
            size="sm"
            required
            label={t('webdav_password_label')}
            autoComplete="current-password"
            value={form.password}
            onChange={value => updateField('password', value)}
          />
          <TextInput
            id="webdav-root-path"
            size="sm"
            required
            label={t('webdav_remote_root_label')}
            value={form.rootPath}
            onChange={value => updateField('rootPath', value)}
          />
          <Button
            variant="light"
            size="xs"
            disabled={
              working || !form.baseUrl || !form.username || !form.password
            }
            onClick={() => void connect()}
          >
            {working ? t('loading') : t('webdav_connect')}
          </Button>
        </form>
      )}
    </uiCtx.Provider>
  )

  return (
    <div className="settings-widget-container">
      <div className="d-none d-md-block" aria-hidden="true" />
      <div className="description-container">
        <div className="title-row">
          <h4 id="webdav">{t('webdav')}</h4>
        </div>
        <p className="small">{t('webdav_description')}</p>
        {mantine ? mantineBody : legacyBody}
      </div>
    </div>
  )
}