// /hub → Site settings → Compilation: Sandboxed compiles (owner #32).
// Native Mantine remake of the legacy SandboxedCompilesTab — same fields
// incl. the dynamic compile-image rows with default-image selection.

import React from 'react'
import { ActionIcon, Button, Group, Radio, Table, Text } from '@mantine/core'
import Icon from '../../../shared/icons'
import {
  Field,
  PageLoading,
  SectionShell,
  SectionTitle,
  bool0,
  num0,
  str0,
  useSyncValues,
  useSiteSettings,
} from './site-core'

type ImageRow = { image: string; name: string }

export function SandboxedSection() {
  const { data, error, flash, save, load } = useSiteSettings('sandboxed-compiles')
  const { v, up } = useSyncValues(data, d => {
    const images: ImageRow[] = Array.isArray((d as any).images)
      ? (d as any).images.map((r: any) => ({ image: String(r?.image ?? ''), name: String(r?.name ?? '') }))
      : []
    return {
      enabled: bool0((d as any).enabled, true),
      hostDir: str0((d as any).hostDir),
      socketPath: str0((d as any).socketPath),
      extraFlags: str0((d as any).extraFlags),
      imageUser: str0((d as any).imageUser),
      images,
      defaultImage: str0((d as any).defaultImage, images[0]?.image || ''),
      bodySize: String(num0((d as any).compileBodySizeLimitMb, 50)),
    }
  })
  const images: ImageRow[] = Array.isArray(v.images) ? (v.images as ImageRow[]) : []
  const defaultImage = String(v.defaultImage || images[0]?.image || '')
  if (!data && !error) return <PageLoading label="Loading sandboxed compile settings…" />
  if (error && !data) return <Group><Text size="sm" c="red">Sandboxed compiles: {error}</Text><AnchorRetry load={load} /></Group>
  const setRow = (i: number, patch: Partial<ImageRow>) =>
    up({ images: images.map((r, j) => (j === i ? { ...r, ...patch } : r)) })
  return (
    <SectionShell
      title="Sandboxed compiles"
      badge="docker"
      enabled={Boolean(v.enabled)}
      onEnabled={x => up({ enabled: x })}
      description="Docker-based sandboxed compilation (clsi) and the selectable compile images."
      footerNote="Changes apply on the next container cycle."
      flash={flash}
      onSave={() => void save({
        enabled: Boolean(v.enabled),
        dockerRunner: Boolean(v.enabled),
        hostDir: String(v.hostDir || ''),
        socketPath: String(v.socketPath || ''),
        extraFlags: String(v.extraFlags || ''),
        imageUser: String(v.imageUser || ''),
        images: images.map(r => ({ image: String(r.image || ''), name: String(r.name || '') })),
        defaultImage: defaultImage || images[0]?.image || '',
        compileBodySizeLimitMb: num0(v.bodySize, 50),
      })}
    >
      <SectionTitle top>Container host</SectionTitle>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start', width: '100%' }}>
        <Field label="Compile host dir" required value={String(v.hostDir || '')} onChange={x => up({ hostDir: x })} placeholder="/data/overleaf/compiles" hint="Host directory mounted into compile containers." width="100%" />
        <Field label="Docker socket" required value={String(v.socketPath || '')} onChange={x => up({ socketPath: x })} placeholder="/var/run/docker.sock" hint="Path to the docker socket (in the web container)." width="100%" />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Extra docker flags" value={String(v.extraFlags || '')} onChange={x => up({ extraFlags: x })} placeholder="-shell-escape" />
        <Field label="Image user" value={String(v.imageUser || '')} onChange={x => up({ imageUser: x })} placeholder="www-data" hint="User used inside compile images." />
      </Group>
      <Group wrap="wrap" gap="md" mb="xs" style={{ alignItems: 'flex-start' }}>
        <Field label="Compile body size limit (MiB)" value={String(v.bodySize || '')} onChange={x => up({ bodySize: x.replace(/[^\d]/g, '') })} placeholder="50" width="34%" type="number" />
      </Group>
      <SectionTitle top>Compile images</SectionTitle>
      <Table withTableBorder style={{ borderRadius: 10, overflow: 'hidden', marginBottom: 10 }}>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Image</Table.Th>
            <Table.Th>Name</Table.Th>
            <Table.Th style={{ width: 70 }}>Remove</Table.Th>
            <Table.Th style={{ width: 90 }}>Default</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {images.map((row, i) => (
            <Table.Tr key={i}>
              <Table.Td>
                <Field label="" value={row.image} onChange={x => setRow(i, { image: x })} placeholder="texlive/texlive:latest-full" />
              </Table.Td>
              <Table.Td>
                <Field label="" value={row.name} onChange={x => setRow(i, { name: x })} placeholder="TeXLive 2025" />
              </Table.Td>
              <Table.Td>
                <ActionIcon
                  variant="subtle"
                  color="gray"
                  disabled={images.length <= 1}
                  aria-label="Remove image row"
                  onClick={() =>
                    up({ images: images.filter((_, j) => j !== i) })
                  }
                >
                  <Icon name="delete" size={16} />
                </ActionIcon>
              </Table.Td>
              <Table.Td>
                <Radio
                  checked={defaultImage === (row.image || images[0]?.image || '')}
                  onChange={() => up({ defaultImage: row.image })}
                  aria-label={`Default image ${row.image || i + 1}`}
                />
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      <Button
        size="xs"
        variant="light"
        color="ollitex"
        leftSection={<Icon name="add" size={15} />}
        onClick={() => up({ images: [...images, { image: '', name: '' }] })}
      >
        Add image
      </Button>
    </SectionShell>
  )
}

function AnchorRetry({ load }: { load: () => void }) {
  return (
    <button
      type="button"
      onClick={e => {
        e.preventDefault()
        void load()
      }}
    >
      Retry
    </button>
  )
}
