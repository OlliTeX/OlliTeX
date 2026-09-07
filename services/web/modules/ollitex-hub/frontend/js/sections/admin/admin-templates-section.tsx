import React, { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Anchor,
  Badge,
  Button,
  Card,
  FileInput,
  Group,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Tooltip,
  ActionIcon,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { getJSON, postJSON, putJSON, deleteJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import ConfirmModal from '../../shared/confirm-modal'
import { PageError, PageLoading } from '../../shared/page-state'

type Category = { key: string; name?: string; enabled?: boolean }
type GallerySection = { enabled?: boolean; categories?: Category[] }
type GalleryTemplate = {
  template_id?: string
  id?: string
  name?: string
  title?: string
  category?: string
  version?: string
  owner?: string
}

function tid(t: any): string {
  return t?.template_id || t?.id || ''
}
function tname(t: any): string {
  return t?.name || t?.title || 'Untitled'
}

export default function AdminTemplatesSection() {
  const [sectionCfg, setSectionCfg] = useState<GallerySection | null>(null)
  const [cfgError, setCfgError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [list, setList] = useState<GalleryTemplate[] | null>(null)
  const [importFile, setImportFile] = useState<File | null>(null)
  const [importUrl, setImportUrl] = useState('')
  const [override, setOverride] = useState(false)
  const [busy, setBusy] = useState(false)
  const [confirmDel, setConfirmDel] = useState<GalleryTemplate | null>(null)
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setCfgError(null)
    try {
      const all = await getJSON('/admin/site-settings')
      const tpl = (all && (all.templates || all.sections?.templates)) || {}
      setSectionCfg({
        enabled: tpl.enabled !== false,
        categories: Array.isArray(tpl.categories) ? tpl.categories : [],
      })
    } catch (err: any) {
      setCfgError((err?.data?.message as string) || String(err?.message || err))
    }
    try {
      const data = await getJSON('/api/templates/admin-list')
      const l = Array.isArray(data?.templates) ? data.templates : Array.isArray(data) ? data : []
      setList(l)
    } catch {
      setList([])
    }
    setLoading(false)
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const saveSection = async (patch: Partial<GallerySection>) => {
    if (!sectionCfg) return
    const next = { ...sectionCfg, ...patch }
    try {
      await putJSON('/admin/site-settings/templates', { body: next })
      setSectionCfg(next)
      notifications.show({ message: 'Template gallery settings saved.', color: 'teal' })
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || 'Could not save settings.',
        color: 'red',
      })
    }
  }

  const doImport = async () => {
    setBusy(true)
    try {
      if (importFile) {
        const b64 = await new Promise<string>((resolve, reject) => {
          const fr = new FileReader()
          fr.onload = () => {
            const s = String(fr.result || '')
            const i = s.indexOf(',')
            resolve(i >= 0 ? s.slice(i + 1) : s)
          }
          fr.onerror = () => reject(new Error('Could not read the file'))
          fr.readAsDataURL(importFile)
        })
        await postJSON('/template/bundle/import', {
          body: { data: b64, override },
        })
        setImportFile(null)
        setOverride(false)
      } else if (importUrl.trim()) {
        await postJSON('/template/bundle/import-url', {
          body: { url: importUrl.trim(), override },
        })
        setImportUrl('')
        setOverride(false)
      } else {
        throw new Error('Choose a bundle file or a URL first.')
      }
      notifications.show({ message: 'Template imported.', color: 'teal' })
      await load()
    } catch (err: any) {
      notifications.show({
        message: (err?.data?.message as string) || err?.message || 'Import failed.',
        color: 'red',
      })
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <PageLoading label="Loading template management…" />

  return (
    <Stack gap="md">
      {cfgError ? <PageError label="Couldn’t load gallery settings" detail={cfgError} onRetry={() => void load()} /> : null}

      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <Group justify="space-between" wrap="nowrap" gap="sm">
            <div>
              <Text fw={700}>Public template gallery</Text>
              <Text size="sm" c="dimmed" mt={4}>
                Controls the /templates page and the “Use template” flows. Management (this
                section) keeps working while the gallery is off.
              </Text>
            </div>
            <Switch
              checked={sectionCfg?.enabled !== false}
              onChange={v => void saveSection({ enabled: v })}
              color="ollitex"
            />
          </Group>

          {sectionCfg?.categories?.length ? (
            <Stack gap="xs">
              <Text size="sm" fw={600}>
                Categories
              </Text>
              {sectionCfg.categories.map(c => (
                <Group key={c.key} justify="space-between" wrap="nowrap">
                  <Text size="sm">{c.name || c.key}</Text>
                  <Switch
                    size="xs"
                    checked={c.enabled !== false}
                    color="ollitex"
                    onChange={v =>
                      void saveSection({
                        categories: (sectionCfg?.categories || []).map(x =>
                          x.key === c.key ? { ...x, enabled: v } : x
                        ),
                      })
                    }
                  />
                </Group>
              ))}
            </Stack>
          ) : null}
        </Stack>
      </Card>

      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <Text fw={700}>Import a template bundle</Text>
          <FileInput
            value={importFile}
            onChange={setImportFile}
            accept="application/zip,.zip"
            placeholder="…/thesis-template.zip"
            leftSection={<Icon name="upload_file" size={18} />}
          />
          <Group gap="sm" wrap="wrap">
            <div style={{ flex: 1, minWidth: 260 }}>
              <TextInput
                value={importUrl}
                onChange={e => setImportUrl(e.currentTarget.value)}
                placeholder="…or a URL: https://…/template.zip"
                leftSection={<Icon name="link" size={18} />}
              />
            </div>
            <Button size="sm" color="ollitex" loading={busy} onClick={() => void doImport()}>
              Import
            </Button>
          </Group>
          <Group gap="sm">
            <Switch
              checked={override}
              onChange={setOverride}
              label="Replace a template with the same name"
              size="xs"
              color="ollitex"
            />
          </Group>
        </Stack>
      </Card>

      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <Group justify="space-between" wrap="nowrap">
            <Text fw={700}>
              Templates {list ? `(${list.length})` : ''}
            </Text>
            <Anchor href="/templates/manage" target="_blank" rel="noreferrer" size="sm">
              Open legacy manage page
            </Anchor>
          </Group>
          {!list || list.length === 0 ? (
            <Text size="sm" c="dimmed">
              No templates on this instance yet — import a bundle above.
            </Text>
          ) : (
            <Table striped highlightOnHover withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Template</Table.Th>
                  <Table.Th style={{ width: 140 }}>Category</Table.Th>
                  <Table.Th style={{ width: 90 }}>Version</Table.Th>
                  <Table.Th style={{ width: 170, textAlign: 'right' }}>Actions</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {list.map(t => (
                  <Table.Tr key={tid(t)}>
                    <Table.Td>
                      <Text size="sm" fw={600} ellipsis>
                        {tname(t)}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Badge size="xs" variant="light" radius="sm">
                        {t.category || '—'}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" c="dimmed">
                        {t.version || '—'}
                      </Text>
                    </Table.Td>
                    <Table.Td style={{ textAlign: 'right' }}>
                      <Group gap={4} justify="flex-end">
                        <Tooltip label="Download bundle">
                          <Anchor href={`/template/${tid(t)}/bundle`} target="_blank" rel="noreferrer" size="sm">
                            <ActionIcon variant="subtle" aria-label="Download bundle">
                              <Icon name="download" size={18} />
                            </ActionIcon>
                          </Anchor>
                        </Tooltip>
                        <Tooltip label="Delete template">
                          <ActionIcon
                            variant="subtle"
                            color="red"
                            aria-label="Delete template"
                            style={{ cursor: 'pointer' }}
                            onClick={() => setConfirmDel(t)}
                          >
                            <Icon name="delete" size={18} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Stack>
      </Card>

      <ConfirmModal
        open={!!confirmDel}
        title="Delete template?"
        body={confirmDel ? `“${tname(confirmDel)}” will be removed from the gallery.` : ''}
        confirmLabel="Delete"
        danger
        loading={deleting}
        onCancel={() => setConfirmDel(null)}
        onConfirm={() => {
          setDeleting(true)
          void (async () => {
            try {
              await deleteJSON(`/template/${tid(confirmDel as GalleryTemplate)}/delete`)
              notifications.show({ message: 'Template deleted.', color: 'gray' })
              setConfirmDel(null)
              await load()
            } catch (err: any) {
              notifications.show({
                message: (err?.data?.message as string) || 'Delete failed.',
                color: 'red',
              })
              setConfirmDel(null)
            } finally {
              setDeleting(false)
            }
          })()
        }}
      />
    </Stack>
  )
}
