import React, { useCallback, useEffect, useState } from 'react'
import {

  ActionIcon,
  Anchor,
  Badge,
  Button,
  Card,
  FileInput,
  Group,
  Modal,
  NativeSelect,
  Stack,
  Switch,
  Table,
  Text,
  Textarea,
  TextInput,
  Tooltip,
} from '@mantine/core'
import { notify } from '../../shared/notify'
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
  const [editTpl, setEditTpl] = useState<GalleryTemplate | null>(null)
  const [editForm, setEditForm] = useState<{ name?: string; descriptionMD?: string; authorMD?: string; license?: string; category?: string; language?: string }>({})
  const [editCategories, setEditCategories] = useState<Array<{ key: string; name: string; url: string }>>([])
  const [savingEdit, setSavingEdit] = useState(false)

  useEffect(() => {
    if (!editTpl) return
    let alive = true
    getJSON<any>('/api/template/categories')
      .then(data => {
        if (!alive) return
        const list = Array.isArray(data?.categories) ? data.categories : Array.isArray(data) ? data : []
        setEditCategories(
          list
            .map((c: any) => ({ key: c?.key || c?.url || '', name: c?.name || c?.key || '', url: c?.url || '' }))
            .filter((c: any) => c.key)
        )
      })
      .catch(() => {
        if (alive) setEditCategories([])
      })
    return () => {
      alive = false
    }
  }, [editTpl])

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
      notify({ message: 'Template gallery settings saved.', color: 'teal' })
    } catch (err: any) {
      notify({
        message: (err?.data?.message as string) || 'Could not save settings.',
        color: 'red',
      })
    }
  }

  // Mantine 9.6 quirk (PG-TH-1): FileInput onChange can deliver the File,
  // the change event, or even the input element depending on re-renders.
  // Normalize to a real File (or null) before storing.
  const onImportFile = (e: unknown) => {
    const maybe = (e as any)?.target?.files?.[0] ?? e
    const ok = maybe instanceof File || ((maybe as any)?.size !== undefined && typeof (maybe as any)?.name === 'string')
    setImportFile(ok ? (maybe as File) : null)
  }

  const doImport = async () => {
    setBusy(true)
    try {
      // PG-TH-1: only a real File is importable (guards against event/element leakage)
      if (importFile instanceof File) {
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
          body: { data: b64, override: override === true },
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
      notify({ message: 'Template imported.', color: 'teal' })
      await load()
    } catch (err: any) {
      notify({
        message: (err?.data?.message as string) || err?.message || 'Import failed.',
        color: 'red',
      })
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <PageLoading label="Loading template management…" />

  const doEdit = async () => {
    if (!editTpl) return
    setSavingEdit(true)
    try {
      // Partial update: the server schema requires non-empty license; omit blank
      // fields so untouched values survive (mirrors the legacy edit dialog,
      // which never sends the fields the user did not touch).
      const body = Object.fromEntries(Object.entries(editForm).filter(([, v]) => v !== ''))
      await postJSON(`/template/${tid(editTpl)}/edit`, { body: body })
      notify({ message: 'Template updated.', color: 'teal' })
      setEditTpl(null)
      setEditForm({})
      await load()
    } catch (err: any) {
      notify({
        message: (err?.data?.message as string) || 'Could not update the template.',
        color: 'red',
      })
    } finally {
      setSavingEdit(false)
    }
  }


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
              onChange={() => void saveSection({ enabled: sectionCfg?.enabled === false })}
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
                    onChange={() =>
                      void saveSection({
                        categories: (sectionCfg?.categories || []).map(x =>
                          x.key === c.key ? { ...x, enabled: x.enabled === false } : x
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
            onChange={onImportFile}
            accept="application/zip,.zip"
            placeholder="…/thesis-template.zip"
            leftSection={<Icon name="upload_file" size={18} />}
          />
          <Group gap="sm" wrap="wrap">
            <div style={{ flex: 1, minWidth: 260 }}>
              <TextInput
                aria-label="Template import URL"
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
              onChange={() => setOverride(override !== true)}
              label="Replace a template with the same name"
              size="xs"
              color="ollitex"
            />
          </Group>
        </Stack>
      </Card>

      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="md">
          <Group justify="space-between" wrap="nowrap" align="center">
            <Text fw={700}>
              Templates {list ? `(${list.length})` : ''}
            </Text>
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
                        <Tooltip label="Edit template">
                          <ActionIcon
                            variant="subtle"
                            aria-label="Edit template"
                            style={{ cursor: 'pointer' }}
                            onClick={() => {
                              setEditTpl(t)
                              setEditForm({
                                name: tname(t),
                                descriptionMD: (t as any).descriptionMD || (t as any).description || '',
                                authorMD: (t as any).authorMD || (t as any).author || '',
                                license: (t as any).license || '',
                                category: (t as any).category || '',
                                language: (t as any).language || '',
                              })
                            }}
                          >
                            <Icon name="edit" size={18} />
                          </ActionIcon>
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
              notify({ message: 'Template deleted.', color: 'gray' })
              setConfirmDel(null)
              await load()
            } catch (err: any) {
              notify({
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
      <Modal
        opened={!!editTpl}
        onClose={() => setEditTpl(null)}
        title={<Text fw={700}>Edit template</Text>}
        size="lg"
      >
        <Group gap="md" mt="sm" wrap="wrap" align="flex-start">
          <div style={{ width: '55%', minWidth: 260 }}>
            <TextInput
              label="Title"
              value={editForm.name ?? ''}
              onChange={e => { const v = e.currentTarget.value; setEditForm(f => ({ ...f, name: v })) }}
            />
            <TextInput
              mt="sm"
              label="Author (markdown)"
              value={editForm.authorMD ?? ''}
              onChange={e => { const v = e.currentTarget.value; setEditForm(f => ({ ...f, authorMD: v })) }}
            />
            <Group gap="sm" wrap="wrap" mt="sm">
              <div style={{ flex: 1, minWidth: 200 }}>
                <NativeSelect
                  label="Category"
                  value={editForm.category ?? (editTpl?.category || '')}
                  onChange={e => setEditForm(f => ({ ...f, category: e.currentTarget.value }))}
                  data={[
                    { value: '', label: '— no category —' },
                    ...(editCategories.map(c => ({ value: c.url || c.key, label: c.name }))),
                  ]}
                />
              </div>
              <div style={{ flex: 1, minWidth: 200 }}>
                <NativeSelect
                  label="Language"
                  value={editForm.language ?? ''}
                  onChange={e => setEditForm(f => ({ ...f, language: e.currentTarget.value }))}
                  data={[
                    { value: '', label: 'Off' },
                    { value: 'en_US', label: 'English (American)' },
                    { value: 'en_GB', label: 'English (British)' },
                    { value: 'de_DE', label: 'German' },
                    { value: 'fr_FR', label: 'French' },
                    { value: 'es_ES', label: 'Spanish' },
                    { value: 'it_IT', label: 'Italian' },
                    { value: 'pt_PT', label: 'Portuguese' },
                  ]}
                />
              </div>
            </Group>
            <Textarea
              mt="sm"
              label="Description (markdown)"
              minRows={4}
              maxRows={10}
              value={editForm.descriptionMD ?? ''}
              onChange={e => { const v = e.currentTarget.value; setEditForm(f => ({ ...f, descriptionMD: v })) }}
            />
            <TextInput mt="sm" label="License (markdown)" value={editForm.license ?? ''} onChange={e => { const v = e.currentTarget.value; setEditForm(f => ({ ...f, license: v })) }} />
          </div>
          <div style={{ width: '45%', minWidth: 220 }}>
            <Text size="xs" fw={600} mb={4}>Preview</Text>
            <img
              src={`/template/${tid(editTpl)}/preview?version=${encodeURIComponent(String((editTpl as any)?.version || 'latest'))}&style=preview`}
              alt={`Preview of ${tname(editTpl)}`}
              style={{ width: '100%', borderRadius: 10, border: '1px solid var(--mantine-color-default-border)', background: 'var(--mantine-color-white)' }}
            />
            <Group gap="xs" mt="sm">
              <Anchor href={`/project/new/template/${tid(editTpl)}?version=${encodeURIComponent(String((editTpl as any)?.version || 'latest'))}&name=${encodeURIComponent(tname(editTpl))}`} target="_blank" rel="noreferrer" size="sm">Open as template</Anchor>
              <Anchor href={`/template/${tid(editTpl)}/preview?version=${encodeURIComponent(String((editTpl as any)?.version || 'latest'))}`} target="_blank" rel="noreferrer" size="sm">View PDF</Anchor>
            </Group>
          </div>
        </Group>
        <Group justify="flex-end" mt="md">
          <Button variant="default" onClick={() => setEditTpl(null)}>Cancel</Button>
          <Button color="ollitex" loading={savingEdit} onClick={() => void doEdit()}>Save</Button>
        </Group>
      </Modal>

    </Stack>
  )
}