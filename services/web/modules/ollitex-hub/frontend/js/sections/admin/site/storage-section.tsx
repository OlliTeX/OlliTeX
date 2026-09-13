// /hub → Site settings → Storage (owner 2026-09-14).
//
// Chooses the local object storage for the in-container filestore +
// docstore services:
//   • fs — the classic flat local files under /var/lib/overleaf (default)
//   • s3 — the SeaweedFS object store (default target; any S3 gateway works)
//
// Values persist as the `storage` site-settings section AND are written to
// a managed env fragment (/etc/overleaf/env.d/ollitex-storage.sh) that every
// runit service sources at start — so the change applies on the next
// container cycle to filestore AND docstore (Node or Go). ${VAR:-value} form
// means explicit compose env always wins. Rollback is the same control set
// back to `fs` + cycle; both stores keep their data — move with
// `seaweed-migrate` whenever (tools live in the repo, cmd/seaweed-migrate).

import React from 'react'
import React from 'react'
import { Anchor, Group, Text } from '@mantine/core'
import {
  Field,
  NativeSelectField,
  PageLoading,
  SectionShell,
  str0,
  useSyncValues,
  useSiteSettings,
} from './site-core'

export function StorageSection() {
  const { data, error, flash, save, load } = useSiteSettings('storage')
  const { v, up } = useSyncValues(data, d => ({
    backend: (d as any).backend === 's3' ? 's3' : 'fs',
    s3Endpoint: str0((d as any).s3Endpoint),
    s3AccessKeyId: str0((d as any).s3AccessKeyId),
    s3Secret: str0((d as any).s3Secret),
    templateFilesBucket: str0((d as any).templateFilesBucket),
    projectBlobsBucket: str0((d as any).projectBlobsBucket),
    globalBlobsBucket: str0((d as any).globalBlobsBucket),
    docstoreArchiveBucket: str0((d as any).docstoreArchiveBucket),
  }))
  const s3Set = Boolean((data as any)?.s3SecretSet)

  if (!data && !error) return <PageLoading label="Loading storage settings…" />
  if (error && !data) {
    return (
      <Group>
        <Text size="sm" c="red">Storage: {error}</Text>
        <Anchor href="#" onClick={e => { e.preventDefault(); void load() }}>Retry</Anchor>
      </Group>
    )
  }

  const managed = Boolean((data as any)?.envManaged)
  return (
    <SectionShell
      title="Storage (local object storage)"
      badge="storage"
      description={
        managed
          ? 'Managed env fragment is in place — filestore + docstore pick these values up on the next container cycle.'
          : 'No stored value yet — the services use their defaults (fs: flat files under /var/lib/overleaf). Saving a backend writes it for the next cycle.'
      }
      footerNote="Applies on the next container restart (filestore + docstore). Compose env always wins over this page. Rollback = choose “Flat files (fs)” and restart; move data with seaweed-migrate."
      flash={flash}
      onSave={() => void save({
        backend: String(v.backend || 'fs'),
        s3Endpoint: String(v.s3Endpoint || '').trim(),
        s3AccessKeyId: String(v.s3AccessKeyId || '').trim(),
        s3Secret: String(v.s3Secret || ''),
        templateFilesBucket: String(v.templateFilesBucket || '').trim(),
        projectBlobsBucket: String(v.projectBlobsBucket || '').trim(),
        globalBlobsBucket: String(v.globalBlobsBucket || '').trim(),
        docstoreArchiveBucket: String(v.docstoreArchiveBucket || '').trim(),
      })}
    >
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <NativeSelectField
          label="Local storage backend"
          value={String(v.backend || 'fs')}
          onChange={x => up({ backend: x })}
          options={[
            { value: 'fs', label: 'Flat files (fs) — default' },
            { value: 's3', label: 'SeaweedFS / S3 gateway' },
          ]}
          hint="What filestore (project/template files) and docstore (deleted-doc archives) store their blobs in. Both services follow the same switch, so a storage migration is one save + one restart."
          width="100%"
        />
      </Group>

      <Text size="sm" fw={700} c="blue" mt="md" mb="xs" pt="xs">
        SeaweedFS / S3 gateway
      </Text>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field
          label="Gateway endpoint"
          value={String(v.s3Endpoint || '')}
          onChange={x => up({ s3Endpoint: x })}
          placeholder="http://seaweedfs-s3:8333"
          hint="S3 endpoint the services talk to. In this stack: http://127.0.0.1:8333 from inside overleafserver, http://seaweedfs-s3:8333 between containers. Empty = service default (127.0.0.1:8333)."
          width="100%"
        />
        <Field
          label="Access key ID"
          value={String(v.s3AccessKeyId || '')}
          onChange={x => up({ s3AccessKeyId: x })}
          placeholder="anonymous (local SeaweedFS)"
          hint="Only if the gateway has auth enabled."
        />
        <Field
          label="Secret access key"
          value={String(v.s3Secret || '')}
          onChange={x => up({ s3Secret: x })}
          placeholder={s3Set ? '•••• (configured — leave empty to keep)' : 'anonymous (local SeaweedFS)'}
          hint={s3Set ? 'A value is stored — leave empty to keep it.' : 'Only if the gateway has auth enabled.'}
        />
      </Group>

      <Text size="sm" fw={700} c="blue" mt="md" mb="xs" pt="xs">
        Buckets
      </Text>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field
          label="Template files bucket"
          value={String(v.templateFilesBucket || '')}
          onChange={x => up({ templateFilesBucket: x })}
          placeholder="filestore-template"
          hint="filestore templates (OVERLEAF_FILESTORE_TEMPLATE_FILES_BUCKET_NAME)."
        />
        <Field
          label="Project blobs bucket"
          value={String(v.projectBlobsBucket || '')}
          onChange={x => up({ projectBlobsBucket: x })}
          placeholder="filestore-blobs"
          hint="filestore history project blobs (OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET)."
        />
        <Field
          label="Global blobs bucket"
          value={String(v.globalBlobsBucket || '')}
          onChange={x => up({ globalBlobsBucket: x })}
          placeholder="filestore-global-blobs"
          hint="filestore hash-global blobs (OVERLEAF_HISTORY_BLOBS_BUCKET)."
        />
        <Field
          label="Docstore archive bucket"
          value={String(v.docstoreArchiveBucket || '')}
          onChange={x => up({ docstoreArchiveBucket: x })}
          placeholder="docstore-archive"
          hint="docstore deleted-document archives (BUCKET_NAME)."
        />
      </Group>

      {String(v.backend || 'fs') === 's3' ? (
        <Text size="xs" c="dimmed" mt="xs">
          Buckets are created idempotently by the services at startup when missing (verified).
          Existing projects keep working: keys in the s3 store are exactly
          {' '}<code>project/…</code> (filestore) or <code>project/doc</code> (docstore) —
          no migration is needed for new data, and old flat files can be lifted over
          any time with <code>seaweed-migrate to-seaweed</code> (or back, lossless).
        </Text>
      ) : null}
    </SectionShell>
  )
}
