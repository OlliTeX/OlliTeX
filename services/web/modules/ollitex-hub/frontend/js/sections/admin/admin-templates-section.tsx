import React, { useCallback, useEffect, useRef, useState } from 'react'
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

type Category = {
  key: string
  name?: string
  enabled?: boolean
  description?: string
  publishable?: boolean
  url?: string
}
// Keep the full server object — saveSection round-trips it, so extra fields
// (allUsersCanManageTemplates / nonAdminCanPublishTemplates / …) are never
// accidentally wiped by a partial save (owner batch 1, 2026-09-11).
type GallerySection = {
  enabled?: boolean
  categories?: Category[]
  allUsersCanManageTemplates?: boolean
  nonAdminCanPublishTemplates?: boolean
  [k: string]: any
}
type TemplateAdmin = {
  id: string
  email: string
  firstName?: string
  lastName?: string
  isAdmin?: boolean
  hasTemplateFlag?: boolean
}
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
  // 2026-09 (mega-batch): refs backing the optimistic + serialized saveSection
  const cfgRef = useRef<GallerySection | null>(null)
  const saveChainRef = useRef<Promise<void>>(Promise.resolve())
  if (cfgRef.current !== sectionCfg) cfgRef.current = sectionCfg
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
  const [counts, setCounts] = useState<Record<string, number | null>>({})
  const [admins, setAdmins] = useState<TemplateAdmin[] | null>(null)
  const [revokeBusy, setRevokeBusy] = useState<string | null>(null)
  const [editCat, setEditCat] = useState<Category | null>(null)
  const [editCatDraft, setEditCatDraft] = useState<{ name: string; description: string }>({ name: '', description: '' })
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
      // full object — legacy /admin/site parity (owner batch 1): keep the
      // site-wide switches + category publishable/description fields intact.
      setSectionCfg({
        ...tpl,
        categories: Array.isArray(tpl.categories) ? tpl.categories : [],
      })
      setCounts((tpl as any).counts || {})
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
    // R6 item 7 parity: users with the template gallery admin flag
    getJSON<any>('/admin/site/template-admins')
      .then(d => setAdmins(Array.isArray(d?.users) ? d.users : []))
      .catch(() => setAdmins(null))
    setLoading(false)
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const saveSection = (patch: Partial<GallerySection>) => {
    // 2026-09 (mega-batch): optimistic + serialized saves. The switches are
    // prop-controlled from sectionCfg and saveSection PUTs the FULL section;
    // with two rapid clicks the second full-state PUT used to carry the
    // stale (pre-first-click) state and overwrite the first flip (e2e
    // "admins flip persisted" flake). Now: the local state is bumped
    // immediately (so every subsequent save composes on the latest value)
    // and saves run one-after-another in order.
    if (!sectionCfg) return Promise.resolve()
    const base = cfgRef.current ?? sectionCfg
    const next = { ...base, ...patch }
    setSectionCfg(next)
    cfgRef.current = next
    const run = saveChainRef.current.then(async () => {
      await putJSON('/admin/site-settings/templates', { body: next })
      notify({ message: 'Template gallery settings saved.', color: 'teal' })
    }).catch((err: any) => {
      // the optimistic bump stays visible until the reload restores truth
      void load()
      notify({
        message: (err?.data?.message as string) || 'Could not save settings.',
        color: 'red',
      })
    })
    saveChainRef.current = run.catch(() => undefined)
    return run
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

          {/* owner batch 1 (2026-09-11): the two site-wide permissions the
              legacy /admin/site Templates tab had — restore exact copy + ids */}
          <Group justify="space-between" wrap="nowrap" gap="sm">
            <div>
              <Text fw={700}>All users are template gallery admins</Text>
              <Text size="sm" c="dimmed" mt={4}>
                Grants every logged-in user the template gallery admin role (manage templates
                only — no other admin powers). When off, the role can be assigned per user on
                the user page.
              </Text>
            </div>
            <Switch
              checked={Boolean(sectionCfg?.allUsersCanManageTemplates)}
              onChange={() => void saveSection({ allUsersCanManageTemplates: !sectionCfg?.allUsersCanManageTemplates })}
              color="ollitex"
            />
          </Group>
          <Group justify="space-between" wrap="nowrap" gap="sm">
            <div>
              <Text fw={700}>Non-admins can publish templates</Text>
              <Text size="sm" c="dimmed" mt={4}>
                Allow any user to propose/publish templates (otherwise template admins only).
              </Text>
            </div>
            <Switch
              checked={Boolean(sectionCfg?.nonAdminCanPublishTemplates)}
              onChange={() => void saveSection({ nonAdminCanPublishTemplates: !sectionCfg?.nonAdminCanPublishTemplates })}
              color="ollitex"
            />
          </Group>

          {/* owner batch 1: full category table (legacy /admin/site parity):
              Name → /templates/<key>, Status, Publishable, Templates count,
              Description, Edit (name/description modal) */}
          {sectionCfg?.categories?.length ? (
            <Stack gap="xs">
              <Table withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Name</Table.Th>
                    <Table.Th>Status</Table.Th>
                    <Table.Th title="Whether non-admin users may publish templates in this category (overrides the site-wide setting). Site admins can always publish.">Publishable</Table.Th>
                    <Table.Th>Templates</Table.Th>
                    <Table.Th>Description</Table.Th>
                    <Table.Th aria-label="Edit category" style={{ width: 70 }} />
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {sectionCfg.categories.map(c => (
                    <Table.Tr key={c.key}>
                      <Table.Td>
                        <Anchor href={`/templates/${c.key}`} target="_blank" rel="noreferrer" size="sm" fw={500}>
                          {c.name || c.key}
                        </Anchor>
                      </Table.Td>
                      <Table.Td>
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
                      </Table.Td>
                      <Table.Td>
                        <Switch
                          size="xs"
                          checked={c.publishable !== false}
                          color="ollitex"
                          onChange={() =>
                            void saveSection({
                              categories: (sectionCfg?.categories || []).map(x =>
                                x.key === c.key ? { ...x, publishable: x.publishable === false } : x
                              ),
                            })
                          }
                        />
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{counts[c.key] == null ? '—' : counts[c.key]}</Text>
                      </Table.Td>
                      <Table.Td style={{ maxWidth: 320 }}>
                        <Text size="sm" c="dimmed" lineClamp={1}>
                          {c.description || '—'}
                        </Text>
                      </Table.Td>
                      <Table.Td style={{ textAlign: 'right' }}>
                        <Button
                          size="xs"
                          variant="default"
                          onClick={() => {
                            setEditCat(c)
                            setEditCatDraft({ name: c.name || '', description: c.description || '' })
                          }}
                        >
                          Edit
                        </Button>
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            </Stack>
          ) : null}
          <Text size="xs" c="dimmed">
            Template bundles (create / edit / import individual templates) live in the template
            gallery admin: <Anchor href="/templates/manage" target="_blank" rel="noreferrer">/templates/manage</Anchor>
          </Text>
        </Stack>
      </Card>

      {/* R6 item 7 parity: who holds the template gallery admin role */}
      <Card withBorder paddings="lg" radius="lg">
        <Stack gap="sm">
          <Text fw={700}>Template gallery admins</Text>
          <Text size="sm" c="dimmed">
            Template gallery admins can manage templates (create, edit in place, download/import
            bundles) without full site admin powers. Assign the role on the user page (Create /
            Update account).
          </Text>
          {admins === null ? (
            <Text size="sm" c="dimmed">—</Text>
          ) : admins.length === 0 ? (
            <Text size="sm" c="dimmed">No users have the template gallery admin role yet.</Text>
          ) : (
            <Table withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>Email</Table.Th>
                  <Table.Th style={{ width: 150, textAlign: 'right' }} />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {admins.map(u => {
                  const fullName = [u.firstName, u.lastName].filter(Boolean).join(' ').trim()
                  return (
                    <Table.Tr key={u.id}>
                      <Table.Td>
                        <Text size="sm" fw={600}>
                          {fullName || u.email}
                        </Text>
                        <Text size="xs" c="dimmed">
                          {u.isAdmin ? 'Site admin' : 'Template gallery admin'}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Anchor href={`/admin/user/${u.id}`} size="sm" target="_blank" rel="noreferrer">
                          {u.email}
                        </Anchor>
                      </Table.Td>
                      <Table.Td style={{ textAlign: 'right' }}>
                        {!u.isAdmin && u.hasTemplateFlag ? (
                          <Button
                            size="xs"
                            variant="light"
                            color="red"
                            loading={revokeBusy === u.id}
                            onClick={() => {
                              setRevokeBusy(u.id)
                              void postJSON(`/admin/user/${u.id}/update`, {
                                body: { canManageTemplates: false },
                              })
                                .then(() => {
                                  notify({ message: 'Template admin role revoked.', color: 'teal' })
                                  return getJSON<any>('/admin/site/template-admins')
                                })
                                .then(d => setAdmins(Array.isArray(d?.users) ? d.users : []))
                                .catch((err: any) =>
                                  notify({
                                    message: (err?.data?.message as string) || 'Could not revoke the role.',
                                    color: 'red',
                                  })
                                )
                                .finally(() => setRevokeBusy(null))
                            }}
                          >
                            Revoke
                          </Button>
                        ) : (
                          <Text size="xs" c="dimmed">
                            Managed via site-admin role
                          </Text>
                        )}
                      </Table.Td>
                    </Table.Tr>
                  )
                })}
              </Table.Tbody>
            </Table>
          )}
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
            {/* 6 (2026-09-16, owner): the legacy manage page used a license DROPDOWN
                (cc_by_4.0 / lppl_1.3c / other) — restore that instead of free text. */}
            <NativeSelect
              mt="sm"
              label="License"
              value={
                ['cc_by_4.0', 'lppl_1.3c', 'other'].includes(editForm.license ?? (editTpl as any)?.license ?? '')
                  ? editForm.license ?? (editTpl as any)?.license
                  : 'other'
              }
              onChange={e => { const v = e.currentTarget.value; setEditForm(f => ({ ...f, license: v })) }}
              data={[
                { value: 'cc_by_4.0', label: 'Creative Commons CC BY 4.0' },
                { value: 'lppl_1.3c', label: 'LaTeX Project Public License 1.3c' },
                { value: 'other', label: 'Other (as stated in the work)' },
              ]}
            />
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