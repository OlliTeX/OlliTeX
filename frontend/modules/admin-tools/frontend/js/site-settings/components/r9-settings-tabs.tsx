/**
 * R9 §7.2 (2026-08-29): six admin-managed runtime tabs for the Manage
 * Site console — Sandboxed Compiles, Git Integration, GitHub Sync,
 * E-mail, Linked File Types, Pandoc.
 *
 * Restyled 2026-08-30 in the CE+ admin vocabulary (user request — mirror
 * davrot/overleaf-cep@fe4ceb6 email-admin.pug / sso-admin.pug):
 * card + enable switch, h6.text-primary section headers, row/col-md grids,
 * label.form-label (strong) + input.form-control, form-text hints,
 * no-autofill password wrappers, big "Save Configuration" footer.
 *
 * Each tab saves its own site-settings section (stored values WIN over
 * compose env from the next container cycle — the boot hydrator applies
 * them to every service, see modules/server-ce-scripts/scripts/
 * hydrate-site-settings-env.mjs and app/src/Features/SiteSettings/
 * EnvHydrator.mjs). Secrets are encrypted + masked exactly like the SSO
 * tabs: empty on save keeps the stored value.
 */
import React, { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { postJSON } from '@/infrastructure/fetch-json'
import {
  Card,
  Field,
  Hint,
  PasswordField,
  Row,
  SaveFooter,
  SectionTitle,
  Switch
} from './ce-admin-ui'
import { useSave } from './sso-settings-tab'

type SectionValue = {
  enabled?: boolean
  [key: string]: unknown
}


function Two ({ a, b, cols = 'col-md-6' }: {
  a: React.ReactNode
  b?: React.ReactNode
  cols?: string
}) {
  return (
    <div className="row mb-3">
      <div className={cols}>{a}</div>
      {b && <div className={cols}>{b}</div>}
    </div>
  )
}

// ------------------------------------------------------------------------
// Sandboxed Compiles
// ------------------------------------------------------------------------
type ImageRow = { image: string; name: string }

export function SandboxedCompilesTab (
  { initial }: { initial: SectionValue }
) {
  const { t } = useTranslation()
  const { flash, save } = useSave('sandboxed-compiles')
  const [enabled, setEnabled] = useState(Boolean(initial.enabled ?? true))
  const [hostDir, setHostDir] = useState(String(initial.hostDir ?? ''))
  const [socketPath, setSocketPath] = useState(String(initial.socketPath ?? ''))
  const [extraFlags, setExtraFlags] = useState(String(initial.extraFlags ?? ''))
  const [imageUser, setImageUser] = useState(String(initial.imageUser ?? ''))
  const [images, setImages] = useState<ImageRow[]>(
    Array.isArray(initial.images)
      ? initial.images.map(r => ({
        image: String(r?.image ?? ''),
        name: String(r?.name ?? '')
      }))
      : []
  )
  const [defaultImage, setDefaultImage] = useState(
    String(initial.defaultImage ?? '')
  )
  // 2026-09-09 (owner R10 #3): compile body size limit (was compose env)
  const [bodySizeLimitMb, setBodySizeLimitMb] = useState(
    String(initial.compileBodySizeLimitMb ?? 50)
  )

  const setRow = (i: number, patch: Partial<ImageRow>) => {
    setImages(rows =>
      rows.map((r, j) => (j === i ? { ...r, ...patch } : r))
    )
  }

  const submit = () => {
    void save({
      enabled,
      dockerRunner: enabled,
      hostDir,
      socketPath,
      extraFlags,
      imageUser,
      images,
      defaultImage: defaultImage || (images[0] && images[0].image) || '',
      compileBodySizeLimitMb: parseInt(bodySizeLimitMb, 10) || 50
    })
  }

  return (
    <Card
      title={t('adminSite.sandboxedCompiles')}
      enabled={enabled}
      onEnabled={setEnabled}
      badge="docker"
    >
      <p className="text-muted">
        {t('adminSite.scDesc')}
        {t('adminSite.scFixed')}
      </p>
      <SectionTitle>Container Host</SectionTitle>
      {/* 2026-09-09 (owner R11 #12): full-width rows — these are long paths */}
      <Row cols={12}><Field id="sc-hostdir" label={t('adminSite.scHostDir')} required value={hostDir} onChange={setHostDir} placeholder="/data/overleaf/compiles" hint={t('adminSite.scHostDirHint')} /></Row>
      <Row cols={12}><Field id="sc-socket" label={t('adminSite.scSocket')} required value={socketPath} onChange={setSocketPath} placeholder="/var/run/docker.sock" hint={t('adminSite.scSocketHint')} /></Row>
      <Two
        a={<Field id="sc-flags" label={t('adminSite.scFlags')} value={extraFlags} onChange={setExtraFlags} placeholder="-shell-escape" />}
        b={<Field id="sc-user" label={t('adminSite.scImageUser')} value={imageUser} onChange={setImageUser} placeholder="www-data" hint={t('adminSite.scImageUserHint')} />}
      />
      <Two
        a={<Field id="sc-bodysize" label={t('adminSite.scBodySizeLimit')} type="number" value={bodySizeLimitMb} onChange={setBodySizeLimitMb} placeholder="50" hint={t('adminSite.scBodySizeHint')} />}
      />
      <SectionTitle top>{t('adminSite.scImages')}</SectionTitle>
      <table className="table table-sm mb-2">
        <thead>
          <tr>
            <th>{t('adminSite.scImageCol')}</th>
            <th>{t('adminSite.scNameCol')}</th>
            <th style={{ width: '90px' }}>{t('remove')}</th>
            <th style={{ width: '100px' }}>{t('adminSite.scDefaultCol')}</th>
          </tr>
        </thead>
        <tbody>
          {images.map((row, i) => (
            <tr key={i}>
              <td>
                <input
                  className="form-control"
                  value={row.image}
                  placeholder="texlive/texlive:latest-full"
                  onChange={e => setRow(i, { image: e.currentTarget.value })}
                />
              </td>
              <td>
                <input
                  className="form-control"
                  value={row.name}
                  placeholder="TeXLive 2025"
                  onChange={e => setRow(i, { name: e.currentTarget.value })}
                />
              </td>
              <td>
                <button
                  type="button"
                  className="btn btn-sm btn-outline-secondary"
                  disabled={images.length <= 1}
                  onClick={() =>
                    setImages(rows => rows.filter((_, j) => j !== i))
                  }
                >
                  {t('remove')}
                </button>
              </td>
              <td>
                <div className="form-check">
                  <input
                    className="form-check-input"
                    type="radio"
                    name="sc-default-image"
                    checked={
                      (defaultImage || (images[0] && images[0].image)) === row.image
                    }
                    onChange={() => setDefaultImage(row.image)}
                    aria-label={t('adminSite.scDefaultCol')}
                  />
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {/* R12-14 (2026-08-31): btn-outline-secondary was invisible on the white
          admin card (faint border + faint text on white) — outline-primary is
          clearly visible on both themes. */}
      <button
        type="button"
        className="btn btn-sm btn-outline-primary"
        onClick={() => setImages(rows => [...rows, { image: '', name: '' }])}
      >
        + {t('adminSite.scAddRow')}
      </button>
      <SaveFooter
        flash={flash}
        onSave={submit}
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// ------------------------------------------------------------------------
// Git Integration
// ------------------------------------------------------------------------
export function GitIntegrationTab (
  { initial }: { initial: SectionValue }
) {
  const { t } = useTranslation()
  const { flash, save } = useSave('git-integration')
  const [enabled, setEnabled] = useState(Boolean(initial.enabled))
  const [host, setHost] = useState(String(initial.host ?? 'git-bridge'))
  const [port, setPort] = useState(String(initial.port ?? 8000))

  return (
    <Card
      title={t('adminSite.gitIntegration')}
      enabled={enabled}
      onEnabled={setEnabled}
      badge="git"
    >
      <p className="text-muted">{t('adminSite.gitDesc')}</p>
      <Two
        a={<Field id="git-host" label={t('adminSite.gitHost')} required value={host} onChange={setHost} placeholder="git-bridge" />}
        b={<Field id="git-port" label={t('adminSite.gitPort')} required value={port} onChange={v => setPort(v.replace(/[^\d]/g, ''))} placeholder="8000" />}
      />
      <p className="text-muted">{t('adminSite.gitContainerNote')}</p>
      <SaveFooter
        flash={flash}
        onSave={() => void save({ enabled, host, port: Number(port) || 8000 })}
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// ------------------------------------------------------------------------
// GitHub Sync
// ------------------------------------------------------------------------
export function GithubSyncTab (
  { initial }: { initial: SectionValue }
) {
  const { t } = useTranslation()
  const { flash, save } = useSave('github-sync')
  const [enabled, setEnabled] = useState(Boolean(initial.enabled))
  const [clientID, setClientID] = useState(
    String(initial.clientId ?? initial.clientID ?? '')
  )
  const [clientSecret, setClientSecret] = useState('')
  const [cipherFile, setCipherFile] = useState(String(initial.cipherFile ?? ''))
  const [cipherLabel, setCipherLabel] = useState(String(initial.cipherLabel ?? ''))
  const [advanced, setAdvanced] = useState(false)
  const secretSet = Boolean(initial.clientSecretSet)

  return (
    <Card
      title={t('adminSite.githubSync')}
      enabled={enabled}
      onEnabled={setEnabled}
      badge="github"
    >
      <p className="text-muted">{t('adminSite.ghDesc')}</p>
      <SectionTitle>OAuth App</SectionTitle>
      <Two
        a={<Field id="gh-id" label={t('adminSite.ghClientId')} required value={clientID} onChange={setClientID} />}
        b={<PasswordField id="gh-secret" label={t('adminSite.ghClientSecret')} value={clientSecret} onChange={setClientSecret} set={secretSet} hint={t('adminSite.ssoSecretNote')} />}
      />
      <button
        type="button"
        className="btn btn-sm btn-outline-secondary mb-3"
        onClick={() => setAdvanced(a => !a)}
      >
        {advanced ? '−' : '+'} {t('adminSite.ghAdvanced')}
      </button>
      {advanced && (
        <div className="mb-3">
          <Two
            a={<Field id="gh-cipherfile" label={t('adminSite.ghCipherFile')} value={cipherFile} onChange={setCipherFile} />}
            b={<Field id="gh-cipherlabel" label={t('adminSite.ghCipherLabel')} value={cipherLabel} onChange={setCipherLabel} hint={t('adminSite.ghCipherHint')} />}
          />
        </div>
      )}
      <p className="text-muted">
        {t('adminSite.ghCallback')}{' '}
        <code>https://psintern.neuro.uni-bremen.de/user/github-sync/oauth2/callback</code>
        <br />
        {t('adminSite.ghLimits')}
      </p>
      <SaveFooter
        flash={flash}
        onSave={() =>
          void save({
            enabled,
            clientId: clientID,
            clientSecret,
            cipherFile,
            cipherLabel
          })
        }
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// ------------------------------------------------------------------------
// E-mail — CE+ email-admin.pug layout (2026-08-30)
// ------------------------------------------------------------------------
export function EmailTab ({ initial }: { initial: SectionValue }) {
  const { t } = useTranslation()
  const { flash, save } = useSave('email')
  const [driver, setDriver] = useState(String(initial.driver ?? 'smtp'))
  const [fromAddress, setFromAddress] = useState(String(initial.fromAddress ?? ''))
  const [replyTo, setReplyTo] = useState(String(initial.replyTo ?? ''))
  const [host, setHost] = useState(String(initial.host ?? ''))
  const [port, setPort] = useState(String(initial.port ?? 587))
  const [secure, setSecure] = useState(Boolean(initial.secure))
  const [ignoreTLS, setIgnoreTLS] = useState(Boolean(initial.ignoreTLS))
  const [name, setName] = useState(String(initial.name ?? ''))
  const [user, setUser] = useState(String(initial.user ?? ''))
  const [pass, setPass] = useState('')
  const [tlsRejectUnauth, setTlsRejectUnauth] = useState(
    Boolean(initial.tlsRejectUnauth ?? true)
  )
  const [skipConfirmation, setSkipConfirmation] = useState(
    Boolean(initial.skipConfirmation)
  )
  const [accessKeyId, setAccessKeyId] = useState(String(initial.accessKeyId ?? ''))
  const [sesSecret, setSesSecret] = useState('')
  const [sesRegion, setSesRegion] = useState(String(initial.sesRegion ?? ''))
  // 2026-09-09 (owner R10 #3): admin contact + custom footer (was compose env)
  const [adminEmail, setAdminEmail] = useState(String(initial.adminEmail ?? ''))
  const [customFooter, setCustomFooter] = useState(String(initial.customFooter ?? ''))
  // UI round 10 item 6: one-off test e-mail (sent through the STORED config).
  const [testTo, setTestTo] = useState('')
  const [testSending, setTestSending] = useState(false)
  const [testStatus, setTestStatus] = useState<{ ok: boolean; msg: string } | null>(null)

  const runTest = async () => {
    const to = testTo.trim()
    if (!to || testSending) return
    setTestSending(true)
    setTestStatus(null)
    try {
      await postJSON('/admin/site-settings/email/test', { body: { to } })
      setTestStatus({ ok: true, msg: to })
    } catch (err: unknown) {
      const message =
        (err as { data?: { message?: string } })?.data?.message ||
        (err instanceof Error ? err.message : String(err))
      setTestStatus({ ok: false, msg: message })
    }
    setTestSending(false)
  }
  const passSet = Boolean(initial.passSet)
  const sesSecretSet = Boolean(initial.sesSecretSet)

  const submit = () => {
    void save({
      driver,
      fromAddress,
      replyTo,
      host,
      port: Number(port) || 587,
      secure: driver === 'smtp' ? secure : false,
      ignoreTLS: driver === 'smtp' ? ignoreTLS : false,
      name,
      user,
      pass,
      tlsRejectUnauth,
      accessKeyId: driver === 'ses' ? accessKeyId : '',
      sesSecret: driver === 'ses' ? sesSecret : '',
      sesRegion: driver === 'ses' ? sesRegion : '',
      skipConfirmation,
      adminEmail,
      customFooter
    })
  }

  return (
    <Card
      title={t('adminSite.email')}
      badge="smtp/ses"
    >
      <p className="text-muted">{t('adminSite.emailDesc')}</p>
      <div className="row mb-3">
        <div className="col-md-6">
          <Switch
            id="em-skipconfirm"
            checked={skipConfirmation}
            onChange={setSkipConfirmation}
            label={<><strong>{t('adminSite.emailSkipConfirm')}</strong> <span className="text-muted">{t('adminSite.emailSkipConfirmHint') || ''}</span></>}
          />
        </div>
      </div>
      <SectionTitle>General</SectionTitle>
      <Two
        a={<Field id="em-from" label={t('adminSite.emailFrom')} required value={fromAddress} onChange={setFromAddress} placeholder="noreply@example.com" hint={t('adminSite.emailFromHint')} />}
        b={<Field id="em-reply" label={t('adminSite.emailReplyTo')} value={replyTo} onChange={setReplyTo} placeholder="support@example.com" hint={t('adminSite.emailReplyToHint')} />}
      />
      <div className="row mb-3">
        <div className="col-md-6">
          <Field id="em-admin" label={t('adminSite.emailAdmin')} value={adminEmail} onChange={setAdminEmail} placeholder="admin@example.com" hint={t('adminSite.emailAdminHint')} />
        </div>
        <div className="col-md-6">
          <Field id="em-customfooter" label={t('adminSite.emailCustomFooter')} value={customFooter} onChange={setCustomFooter} placeholder="\u00a9 Example University" />
        </div>
      </div>
      <SectionTitle top>Email Driver</SectionTitle>
      <div className="row mb-3">
        <div className="col-md-6">
          <label className="form-label" htmlFor="em-driver"><strong>{t('adminSite.emailDriver')}</strong></label>
          <select
            id="em-driver"
            className="form-select"
            value={driver}
            onChange={e => setDriver(e.currentTarget.value)}
          >
            <option value="smtp">SMTP</option>
            <option value="ses">AWS SES</option>
          </select>
          <Hint>{t('adminSite.emailDriverHint')}</Hint>
        </div>
      </div>
      {driver === 'smtp' ? (
        <>
          <SectionTitle top>SMTP Configuration</SectionTitle>
          <div className="row mb-3">
            <div className="col-md-6">
              <Field id="em-host" label={t('adminSite.emailHost')} required value={host} onChange={setHost} placeholder="smtp.example.com" />
            </div>
            <div className="col-md-3">
              <Field id="em-port" label={t('adminSite.emailPort')} value={port} onChange={v => setPort(v.replace(/[^\d]/g, ''))} placeholder="587" />
            </div>
            <div className="col-md-3">
              <Field id="em-name" label={t('adminSite.emailName')} value={name} onChange={setName} placeholder="" hint={t('adminSite.emailNameHint')} />
            </div>
          </div>
          <div className="row mb-3">
            <div className="col-md-4">
              <Switch id="em-secure" checked={secure} onChange={setSecure} label={t('adminSite.emailSecure')} />
              <Hint>{t('adminSite.emailSecureHint')}</Hint>
            </div>
            <div className="col-md-4">
              <Switch id="em-ignoretls" checked={ignoreTLS} onChange={setIgnoreTLS} label={t('adminSite.emailIgnoreTLS')} />
              <Hint>{t('adminSite.emailIgnoreTLSHint')}</Hint>
            </div>
            <div className="col-md-4">
              <Switch id="em-tlsreject" checked={tlsRejectUnauth} onChange={setTlsRejectUnauth} label={t('adminSite.emailTlsReject')} />
              <Hint>{t('adminSite.emailTlsRejectHint')}</Hint>
            </div>
          </div>
          <div className="row mb-3">
            <div className="col-md-6">
              <Field id="em-user" label={t('adminSite.emailUser')} value={user} onChange={setUser} placeholder="" />
            </div>
            <div className="col-md-6">
              <PasswordField id="em-pass" label={t('adminSite.emailPass')} value={pass} onChange={setPass} set={passSet} hint={t('adminSite.ssoSecretNote')} />
            </div>
          </div>
        </>
      ) : (
        <>
          <SectionTitle top>AWS SES Configuration</SectionTitle>
          <div className="row mb-3">
            <div className="col-md-6">
              <Field id="em-ak" label={t('adminSite.emailSesAkId')} required value={accessKeyId} onChange={setAccessKeyId} placeholder="" />
            </div>
            <div className="col-md-6">
              <PasswordField id="em-sk" label={t('adminSite.emailSesSecret')} value={sesSecret} onChange={setSesSecret} set={sesSecretSet} hint={t('adminSite.ssoSecretNote')} />
            </div>
          </div>
          <div className="row mb-3">
            <div className="col-md-6">
              <Field id="em-region" label={t('adminSite.emailSesRegion')} value={sesRegion} onChange={setSesRegion} placeholder="us-east-1" />
            </div>
          </div>
        </>
      )}
      <SectionTitle top>Send a test e-mail</SectionTitle>
      <div className="row mb-3 align-items-end">
        <div className="col-md-6">
          <Field
            id="em-test-to"
            label={t('adminSite.emailTestTo')}
            value={testTo}
            onChange={setTestTo}
            placeholder="admin@example.com"
            hint={t('adminSite.emailTestToHint')}
          />
        </div>
        <div className="col-md-6">
          <button
            type="button"
            className="btn btn-primary d-block"
            disabled={testSending || testTo.trim() === ''}
            onClick={() => void runTest()}
          >
            {testSending ? t('adminSite.emailTestSending') : t('adminSite.emailTestSend')}
          </button>
        </div>
      </div>
      {testStatus ? (
        testStatus.ok ? (
          <div role="status" className="alert alert-success mb-3">
            {t('adminSite.emailTestOk')} — {testStatus.msg}
          </div>
        ) : (
          <div role="alert" className="alert alert-danger mb-3">
            {testStatus.msg}
          </div>
        )
      ) : null}
      <SaveFooter flash={flash} onSave={submit} note={t('adminSite.restartHint')} />
    </Card>
  )
}

// ------------------------------------------------------------------------
// Linked File Types
// ------------------------------------------------------------------------
const LINKED_TYPES = [
  { key: 'project_file', locked: true, labelKey: 'adminSite.lftProjectFile' },
  { key: 'project_output_file', locked: true, labelKey: 'adminSite.lftProjectOutput' },
  { key: 'url', locked: false, labelKey: 'adminSite.lftUrl' },
  { key: 'zotero', locked: false, labelKey: 'adminSite.lftZotero' }
]

export function LinkedFileTypesTab (
  { initial }: { initial: SectionValue }
) {
  const { t } = useTranslation()
  const { flash, save } = useSave('linked-file-types')
  const initialTypes: string[] = Array.isArray(initial.enabledTypes)
    ? initial.enabledTypes
    : []
  const [types, setTypes] = useState<string[]>(
    ['project_file', 'project_output_file']
      .concat(
        initialTypes.filter(
          k => k !== 'project_file' && k !== 'project_output_file'
        )
      )
  )

  return (
    <Card title={t('adminSite.linkedFileTypes')} badge="files">
      <p className="text-muted">{t('adminSite.lftDesc')}</p>
      {LINKED_TYPES.map(row => {
        const checked = types.includes(row.key) || row.locked
        return (
          <div key={row.key} className="mb-2">
            <div className="form-check">
              <input
                className="form-check-input"
                type="checkbox"
                id={`lft-${row.key}`}
                checked={checked}
                disabled={row.locked}
                onChange={e => {
                  const on = e.currentTarget.checked
                  setTypes(cur =>
                    on
                      ? Array.from(new Set([...cur, row.key]))
                      : cur.filter(k => k !== row.key)
                  )
                }}
              />
              <label className="form-check-label" htmlFor={`lft-${row.key}`}>
                {t(row.labelKey)}
                {row.locked ? ` (${t('adminSite.lftLocked')})` : ''}
              </label>
            </div>
          </div>
        )
      })}
      <SaveFooter
        flash={flash}
        onSave={() =>
          void save({
            enabledTypes: [
              'project_file',
              'project_output_file',
              ...types.filter(
                k => k !== 'project_file' && k !== 'project_output_file'
              )
            ]
          })
        }
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// ------------------------------------------------------------------------
// Pandoc
// ------------------------------------------------------------------------
export function PandocTab ({ initial }: { initial: SectionValue }) {
  const { t } = useTranslation()
  const { flash, save } = useSave('pandoc')
  const [enabled, setEnabled] = useState(Boolean(initial.enabled))
  const [image, setImage] = useState(
    String(initial.image ?? 'pandoc-ol:3.10.0.0')
  )

  return (
    <Card
      title={t('adminSite.pandoc')}
      enabled={enabled}
      onEnabled={setEnabled}
      badge="pandoc"
    >
      <p className="text-muted">{t('adminSite.pandocDesc')}</p>
      <Two
        a={<Field id="pd-image" label={t('adminSite.pandocImage')} required value={image} onChange={setImage} placeholder="pandoc-ol:3.10.0.0" hint={t('adminSite.restartHint')} />}
      />
      <p className="text-muted">{t('adminSite.pandocBuildNote')}</p>
      <SaveFooter
        flash={flash}
        onSave={() => void save({ enabled, image })}
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// ------------------------------------------------------------------------
// WebDAV (2026-09-04, owner #10) — the WebDAV service configuration
// (compose env WEBDAV_*) is admin-managed here; stored values hydrate the
// env at web boot (applies on the next container cycle). API section id:
// "webdav".
// ------------------------------------------------------------------------
export function WebdavTab ({ initial }: { initial: SectionValue }) {
  const { t } = useTranslation()
  const { flash, save } = useSave('webdav')
  const [enabled, setEnabled] = useState(Boolean(initial.enabled))
  const [rootPath, setRootPath] = useState(String(initial.rootPath ?? '/Overleaf'))
  const [timeoutMs, setTimeoutMs] = useState(String(initial.requestTimeoutMs ?? 60000))
  const [retryCount, setRetryCount] = useState(String(initial.retryCount ?? 2))
  const [retryDelayMs, setRetryDelayMs] = useState(String(initial.retryDelayMs ?? 500))
  const [cipherLabel, setCipherLabel] = useState(String(initial.cipherLabel ?? ''))
  const [cipherPassword, setCipherPassword] = useState('')
  const advanced = Boolean(initial.cipherLabel)
  const [showAdvanced, setShowAdvanced] = useState(advanced)

  const ints = {
    requestTimeoutMs: parseInt(timeoutMs, 10),
    retryCount: parseInt(retryCount, 10),
    retryDelayMs: parseInt(retryDelayMs, 10),
  }

  return (
    <Card
      title={t('adminSite.webdav')}
      enabled={enabled}
      onEnabled={setEnabled}
      badge="webdav"
    >
      <p className="text-muted">{t('adminSite.webdavDesc')}</p>
      <Two
        a={<Field id="wd-root" label={t('adminSite.webdavRootPath')} required value={rootPath} onChange={setRootPath} placeholder="/Overleaf" />}
        b={<Field id="wd-timeout" label={t('adminSite.webdavTimeout')} type="number" value={timeoutMs} onChange={setTimeoutMs} placeholder="60000" />}
      />
      <Two
        a={<Field id="wd-retries" label={t('adminSite.webdavRetries')} type="number" value={retryCount} onChange={setRetryCount} placeholder="2" />}
        b={<Field id="wd-delay" label={t('adminSite.webdavRetryDelay')} type="number" value={retryDelayMs} onChange={setRetryDelayMs} placeholder="500" />}
      />
      <button
        type="button"
        className="btn btn-sm btn-outline-secondary mb-3"
        onClick={() => setShowAdvanced(a => !a)}
      >
        {showAdvanced ? '−' : '+'} {t('adminSite.webdavAdvanced', 'Advanced')}
      </button>
      {showAdvanced && (
        <div className="mb-3">
          <Two
            a={<Field id="wd-cipherlabel" label={t('adminSite.webdavCipherLabel')} value={cipherLabel} onChange={setCipherLabel} placeholder="OL_WEBDAV2-v3" />}
            b={<PasswordField id="wd-cipherpass" label={t('adminSite.webdavCipherPassword')} value={cipherPassword} onChange={setCipherPassword} set={Boolean(initial.cipherPasswordSet)} hint={t('adminSite.webdavCipherHint')} />}
          />
        </div>
      )}
      <SaveFooter
        flash={flash}
        onSave={() =>
          void save({
            enabled,
            rootPath,
            ...ints,
            cipherLabel,
            cipherPassword,
          })}
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// ------------------------------------------------------------------------
// Dropbox (2026-09-04, owner #10) — app credentials + enable flag, managed
// here instead of compose env (DROPBOX_*). API section id: "dropbox".
// ------------------------------------------------------------------------
export function DropboxTab ({ initial }: { initial: SectionValue }) {
  const { t } = useTranslation()
  const { flash, save } = useSave('dropbox')
  const [enabled, setEnabled] = useState(Boolean(initial.enabled))
  const [appKey, setAppKey] = useState(String(initial.appKey ?? ''))
  const [appSecret, setAppSecret] = useState('')

  return (
    <Card
      title={t('adminSite.dropbox')}
      enabled={enabled}
      onEnabled={setEnabled}
      badge="dropbox"
    >
      <p className="text-muted">{t('adminSite.dropboxDesc')}</p>
      <Two
        a={<Field id="db-key" label={t('adminSite.dropboxAppKey')} required value={appKey} onChange={setAppKey} placeholder="xs8q2ebd8qrmhuu" />}
        b={<PasswordField id="db-secret" label={t('adminSite.dropboxAppSecret')} value={appSecret} onChange={setAppSecret} set={Boolean(initial.appSecretSet)} hint={t('adminSite.dropboxSecretHint')} />}
      />
      <SaveFooter
        flash={flash}
        onSave={() => void save({ enabled, appKey, appSecret })}
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// ------------------------------------------------------------------------
// Miscellaneous (2026-09-01) — the remaining toolkit/env differences
// (branding / access / lifecycle / limits / compile) consolidated into one
// tab. See TOOLKIT_ENV_GAP.md. API section id: "misc".
// ------------------------------------------------------------------------
export function MiscTab ({ initial }: { initial: SectionValue }) {
  const { t } = useTranslation()
  const { flash, save } = useSave('misc')

  // Branding
  const [appName, setAppName] = useState(String(initial.appName ?? ''))
  const [hidePoweredBy, setHidePoweredBy] = useState(Boolean(initial.navHidePoweredBy))
  const [noindex, setNoindex] = useState(Boolean(initial.robotsNoindex))
  // Access & sharing
  const [allowPublic, setAllowPublic] = useState(Boolean(initial.allowPublicAccess))
  const [anonRW, setAnonRW] = useState(Boolean(initial.allowAnonymousReadWriteSharing))
  const [disableLink, setDisableLink] = useState(Boolean(initial.disableLinkSharing))
  const [disableChat, setDisableChat] = useState(Boolean(initial.disableChat))
  // Lifecycle
  const [projDelay, setProjDelay] = useState(String(initial.projectHardDeletionDelayDays ?? 90))
  const [userDelay, setUserDelay] = useState(String(initial.userHardDeletionDelayDays ?? 90))
  const [historyRestore, setHistoryRestore] = useState(Boolean(initial.historyRestore))
  const [pdfCaching, setPdfCaching] = useState(initial.enablePdfCaching !== false)
  // python-runner (browser-side Python split editor) — enabled by default
  // only where the operator flipped it on in admin/site (D2: applies on
  // the next container cycle; ENABLE_PYTHON_RUNNER seeds the first boot).
  const [pythonRunner, setPythonRunner] = useState(Boolean(initial.pythonRunner))
  // Limits
  const [maxUpload, setMaxUpload] = useState(String(initial.maxUploadSizeMiB ?? 50))
  const [maxEntities, setMaxEntities] = useState(String(initial.maxEntitiesPerProject ?? 2000))
  // Compile
  const [compiler, setCompiler] = useState(String(initial.defaultLatexCompiler ?? 'pdflatex'))
  // 2026-09-09 (owner R10 #3): project-change notification delay (was compose env)
  const [notifDelay, setNotifDelay] = useState(
    String(initial.projectChangeNotificationDelayMs ?? 30000)
  )

  const submit = (): void => {
    void save({
      appName,
      navHidePoweredBy: hidePoweredBy,
      robotsNoindex: noindex,
      allowPublicAccess: allowPublic,
      allowAnonymousReadWriteSharing: anonRW,
      disableLinkSharing: disableLink,
      disableChat,
      projectHardDeletionDelayDays: parseInt(projDelay, 10),
      userHardDeletionDelayDays: parseInt(userDelay, 10),
      historyRestore,
      enablePdfCaching: pdfCaching,
      pythonRunner,
      maxUploadSizeMiB: parseInt(maxUpload, 10),
      maxEntitiesPerProject: parseInt(maxEntities, 10),
      defaultLatexCompiler: compiler,
      projectChangeNotificationDelayMs: parseInt(notifDelay, 10) || 0,
    })
  }

  return (
    <Card title={t('adminSite.miscellaneous')} badge="misc">
      <p className="text-muted">{t('adminSite.miscDesc')}</p>

      <SectionTitle top>{t('adminSite.miscBranding')}</SectionTitle>
      <Two a={<Field id="misc-app-name" label={t('adminSite.miscAppName')} value={appName} onChange={setAppName} placeholder="Overleaf (Community Edition)" hint={t('adminSite.restartHint')} />} />
      <Two
        a={<Switch id="misc-hide-pb" checked={hidePoweredBy} onChange={setHidePoweredBy} label={t('adminSite.miscHidePoweredBy')} />}
        b={<Switch id="misc-noindex" checked={noindex} onChange={setNoindex} label={t('adminSite.miscNoindex')} />}
      />

      <SectionTitle>{t('adminSite.miscAccess')}</SectionTitle>
      <Two
        a={<Switch id="misc-public" checked={allowPublic} onChange={setAllowPublic} label={t('adminSite.miscAllowPublic')} />}
        b={<Switch id="misc-anonrw" checked={anonRW} onChange={setAnonRW} label={t('adminSite.miscAnonRW')} />}
      />
      <Two
        a={<Switch id="misc-nolink" checked={disableLink} onChange={setDisableLink} label={t('adminSite.miscDisableLinkSharing')} />}
        b={<Switch id="misc-nochat" checked={disableChat} onChange={setDisableChat} label={t('adminSite.miscDisableChat')} />}
      />

      <SectionTitle>{t('adminSite.miscLifecycle')}</SectionTitle>
      <Two
        a={<Field id="misc-proj-delay" label={t('adminSite.miscProjDelay')} type="number" value={projDelay} onChange={setProjDelay} placeholder="90" hint={t('adminSite.miscDaysHint')} />}
        b={<Field id="misc-user-delay" label={t('adminSite.miscUserDelay')} type="number" value={userDelay} onChange={setUserDelay} placeholder="90" hint={t('adminSite.miscDaysHint')} />}
      />
      <Two
        a={<Switch id="misc-hist" checked={historyRestore} onChange={setHistoryRestore} label={t('adminSite.miscHistoryRestore')} />}
        b={<Switch id="misc-pdfcache" checked={pdfCaching} onChange={setPdfCaching} label={t('adminSite.miscPdfCaching')} />}
      />

      <SectionTitle>{t('adminSite.miscLimits')}</SectionTitle>
      <Two
        a={<Field id="misc-maxupload" label={t('adminSite.miscMaxUpload')} type="number" value={maxUpload} onChange={setMaxUpload} placeholder="50" hint={t('adminSite.miscMibHint')} />}
        b={<Field id="misc-maxentities" label={t('adminSite.miscMaxEntities')} type="number" value={maxEntities} onChange={setMaxEntities} placeholder="2000" />}
      />

      <SectionTitle>{t('adminSite.miscCompile')}</SectionTitle>
      <Two a={<Field id="misc-compiler" label={t('adminSite.miscCompiler')} value={compiler} onChange={setCompiler} placeholder="pdflatex" hint={t('adminSite.restartHint')} />} />

      <SectionTitle>{t('adminSite.miscPythonRunner', 'Python runner')}</SectionTitle>
      <Two a={<Switch id="misc-python-runner" checked={pythonRunner} onChange={setPythonRunner} label={t('adminSite.miscPythonRunnerToggle', 'Enable the browser-based Python split editor for .py files (python-runner module).')} hint={t('adminSite.restartHint')} />} />

      <SectionTitle>{t('adminSite.miscNotifications')}</SectionTitle>
      <Two a={<Field id="misc-notif-delay" label={t('adminSite.miscNotifDelay')} type="number" value={notifDelay} onChange={setNotifDelay} placeholder="30000" hint={t('adminSite.miscNotifDelayHint', 'Delay between project changes before the e-mail notification is sent (ms). Applies on the next container cycle.')} />} />

      <SaveFooter flash={flash} onSave={submit} note={t('adminSite.restartHint')} />
    </Card>
  )
}

// ------------------------------------------------------------------------
// 2026-09-09 (owner R10 #3): the remaining compose env vars move into
// admin/site — stored values hydrate the matching env at web boot (boot
// hydrator, EnvHydrator.mjs), so the container no longer carries them.
// ------------------------------------------------------------------------

// LanguageTool server (was compose LANGUAGETOOL_URL / LANGUAGETOOL_ENABLED)
export function LanguagetoolTab ({ initial }: { initial: SectionValue }) {
  const { t } = useTranslation()
  const { flash, save } = useSave('languagetool')
  const [enabled, setEnabled] = useState(initial.enabled !== false)
  const [url, setUrl] = useState(String(initial.url ?? ''))

  return (
    <Card
      title={t('adminSite.languagetool')}
      enabled={enabled}
      onEnabled={setEnabled}
      badge="grammar"
    >
      <p className="text-muted">{t('adminSite.languagetoolDesc')}</p>
      <Two
        a={<Field id="lt-url" label={t('adminSite.languagetoolUrl')} value={url} onChange={setUrl} placeholder="http://languagetool:8010" hint={t('adminSite.languagetoolUrlHint')} />}
      />
      <SaveFooter
        flash={flash}
        onSave={() => void save({ enabled, url })}
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// LLM instance flags (was compose LLM_ENABLED / LLM_ALLOW_USER_SETTINGS /
// LLM_USER_RATE_PER_MINUTE…). Model selection stays on /admin/llm/settings.
export function LlmInstanceTab ({ initial }: { initial: SectionValue }) {
  const { t } = useTranslation()
  const { flash, save } = useSave('llm')
  const [enabled, setEnabled] = useState(initial.enabled !== false)
  const [allowUser, setAllowUser] = useState(
    initial.allowUserSettings !== false
  )
  const [userRate, setUserRate] = useState(String(initial.userRatePerMinute ?? 10))
  const [adminRate, setAdminRate] = useState(String(initial.adminRatePerMinute ?? 0))
  const [userDaily, setUserDaily] = useState(String(initial.userDailyTokens ?? 0))

  return (
    <Card
      title={t('adminSite.llmInstance')}
      enabled={enabled}
      onEnabled={setEnabled}
      badge="ai"
    >
      <p className="text-muted">{t('adminSite.llmInstanceDesc')}</p>
      <Two a={<Switch id="llm-allow-user" checked={allowUser} onChange={setAllowUser} label={t('adminSite.llmAllowUserSettings')} />} />
      <Two
        a={<Field id="llm-user-rate" label={t('adminSite.llmUserRate')} type="number" value={userRate} onChange={setUserRate} placeholder="10" hint={t('adminSite.llmRateHint')} />}
        b={<Field id="llm-admin-rate" label={t('adminSite.llmAdminRate')} type="number" value={adminRate} onChange={setAdminRate} placeholder="0 (unlimited)" />}
      />
      <Two a={<Field id="llm-user-daily" label={t('adminSite.llmUserDailyTokens')} type="number" value={userDaily} onChange={setUserDaily} placeholder="0 (unlimited)" hint={t('adminSite.llmRateHint')} />} />
      <SaveFooter
        flash={flash}
        onSave={() =>
          void save({
            enabled,
            allowUserSettings: allowUser,
            userRatePerMinute: parseInt(userRate, 10) || 0,
            adminRatePerMinute: parseInt(adminRate, 10) || 0,
            userDailyTokens: parseInt(userDaily, 10) || 0,
          })}
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// Branding: navbar title + footers (was compose OVERLEAF_NAV_TITLE /
// OVERLEAF_LEFT_FOOTER / OVERLEAF_RIGHT_FOOTER).
export function BrandingTab ({ initial }: { initial: SectionValue }) {
  const { t } = useTranslation()
  const { flash, save } = useSave('branding')
  const [navTitle, setNavTitle] = useState(String(initial.navTitle ?? ''))
  const [leftFooter, setLeftFooter] = useState(String(initial.leftFooter ?? ''))
  const [rightFooter, setRightFooter] = useState(String(initial.rightFooter ?? ''))

  return (
    <Card title={t('adminSite.branding')} badge="branding">
      <p className="text-muted">{t('adminSite.brandingDesc')}</p>
      <Two a={<Field id="br-nav-title" label={t('adminSite.brandingNavTitle')} value={navTitle} onChange={setNavTitle} placeholder="Overleaf" hint={t('adminSite.restartHint')} />} />
      <div className="mb-3">
        <label htmlFor="br-left-footer" className="form-label"><strong>{t('adminSite.brandingLeftFooter')}</strong></label>
        <textarea
          id="br-left-footer"
          className="form-control"
          rows={3}
          value={leftFooter}
          placeholder='[{"text": "…", "url": "https://…"}]'
          onChange={e => setLeftFooter(e.currentTarget.value)}
        />
        <p className="form-text">{t('adminSite.brandingFooterHint')}</p>
      </div>
      <div className="mb-3">
        <label htmlFor="br-right-footer" className="form-label"><strong>{t('adminSite.brandingRightFooter')}</strong></label>
        <textarea
          id="br-right-footer"
          className="form-control"
          rows={2}
          value={rightFooter}
          placeholder='[{"text": "Powered by Overleaf", "url": "https://github.com/yu-i-i/overleaf-cep"}]'
          onChange={e => setRightFooter(e.currentTarget.value)}
        />
      </div>
      <SaveFooter
        flash={flash}
        onSave={() => void save({ navTitle, leftFooter, rightFooter })}
        note={t('adminSite.restartHint')}
      />
    </Card>
  )
}

// Internal service URLs (was compose V1_HISTORY_URL / GITHUBINTERFACE_* /
// WEBDAVINTERFACE_API_URL / DROPBOXINTERFACE_API_URL /
// DATAMANIPULATOR_API_URL). These are the web process → companion
// service addresses; stored values replace the env at boot.
export function ServicesTab ({ initial }: { initial: SectionValue }) {
  const { t } = useTranslation()
  const { flash, save } = useSave('services')
  const [v1HistoryUrl, setV1HistoryUrl] = useState(String(initial.v1HistoryUrl ?? ''))
  const [githubUrl, setGithubUrl] = useState(String(initial.githubInterfaceUrl ?? ''))
  const [webdavUrl, setWebdavUrl] = useState(String(initial.webdavInterfaceUrl ?? ''))
  const [dropboxUrl, setDropboxUrl] = useState(String(initial.dropboxInterfaceUrl ?? ''))
  const [datamanipUrl, setDatamanipUrl] = useState(String(initial.dataManipulatorUrl ?? ''))

  const submit = () => {
    void save({
      v1HistoryUrl,
      githubInterfaceUrl: githubUrl,
      webdavInterfaceUrl: webdavUrl,
      dropboxInterfaceUrl: dropboxUrl,
      dataManipulatorUrl: datamanipUrl,
    })
  }

  return (
    <Card title={t('adminSite.services')} badge="internal">
      <p className="text-muted">{t('adminSite.servicesDesc')}</p>
      <Two a={<Field id="svc-v1history" label={t('adminSite.svcV1History')} value={v1HistoryUrl} onChange={setV1HistoryUrl} placeholder="http://overleafserver:3100/api" hint={t('adminSite.restartHint')} />} />
      {/* 2026-09-09 (owner R11 #10): the githubinterface workdir root was
          removed from here — it is a fixed container-level constant
          (/var/lib/overleaf/ghif; shared by the in-container web +
          githubinterface processes and persisted on the data volume). An
          admin editing only this field would split web/interface views. */}
      <Two a={<Field id="svc-gh" label={t('adminSite.svcGithub')} value={githubUrl} onChange={setGithubUrl} placeholder="http://localhost:4013" hint={t('adminSite.restartHint')} />} />
      <Two
        a={<Field id="svc-webdav" label={t('adminSite.svcWebdav')} value={webdavUrl} onChange={setWebdavUrl} placeholder="http://localhost:4002" hint={t('adminSite.restartHint')} />}
        b={<Field id="svc-dropbox" label={t('adminSite.svcDropbox')} value={dropboxUrl} onChange={setDropboxUrl} placeholder="http://localhost:4003" hint={t('adminSite.restartHint')} />}
      />
      <Two a={<Field id="svc-datamanipulator" label={t('adminSite.svcDatamanipulator')} value={datamanipUrl} onChange={setDatamanipUrl} placeholder="http://localhost:4001" hint={t('adminSite.restartHint')} />} />
      <SaveFooter flash={flash} onSave={submit} note={t('adminSite.restartHint')} />
    </Card>
  )
}
