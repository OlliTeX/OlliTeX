import React, { useCallback, useEffect, useState } from 'react'
import {
  ActionIcon,
  Badge,
  Button,
  Group,
  Menu,
  Modal,
  NativeSelect,
  Stack,
  Table,
  Tabs,
  Text,
  Textarea,
  TextInput,
  Tooltip,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import BibImportModal from '@modules/bib-editor/frontend/js/components/bib-import-modal'
import OrcidPickerModal from '@modules/orcid-picker/frontend/js/components/orcid-picker-modal'
import ZoteroPickerModal from '@modules/zotero/frontend/js/components/zotero-picker-modal'
import { normaliseOrcidEntryKeys, splitImportText } from '@modules/bib-editor/frontend/js/utils/bib-import'
import Icon from '../../shared/icons'
import { EmptyState, PageError, PageLoading } from '../../shared/page-state'
import ConfirmModal from '../../shared/confirm-modal'
import {
  listEntries,
  createEntries,
  deleteEntries,
  restoreEntries,
  countEntries,
  failureFromError,
} from '@modules/bib-editor/frontend/js/library/library-api'
import type {
  LibraryEntryApi,
  LibraryFieldApi,
} from '@modules/bib-editor/frontend/js/library/library-model'

const BIB_TYPES = [
  'article',
  'book',
  'incollection',
  'inproceedings',
  'misc',
  'online',
  'phdthesis',
  'proceedings',
  'report',
  'techreport',
  'unpublished',
  'reference',
]

function field(entry: LibraryEntryApi, name: string): string {
  const f = (entry.fields || []).find((x: LibraryFieldApi) => x.name === name)
  return f?.value || ''
}

function fmtDate(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString()
}

function AddReferenceModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: () => void
}) {
  const [type, setType] = useState('article')
  const [key, setKey] = useState('')
  const [title, setTitle] = useState('')
  const [author, setAuthor] = useState('')
  const [year, setYear] = useState('')
  const [url, setUrl] = useState('')
  const [note, setNote] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const reset = () => {
    setType('article')
    setKey('')
    setTitle('')
    setAuthor('')
    setYear('')
    setUrl('')
    setNote('')
    setErr(null)
  }

  const submit = async () => {
    if (!title.trim()) {
      setErr('Title is required.')
      return
    }
    setBusy(true)
    setErr(null)
    try {
      const fields: LibraryFieldApi[] = [
        { name: 'title', value: title.trim() },
        { name: 'author', value: author.trim() },
        { name: 'year', value: year.trim() },
        { name: 'url', value: url.trim() },
        { name: 'note', value: note.trim() },
      ].filter(f => f.value)
      await createEntries([
        {
          key: key.trim() || '',
          type,
          fields,
        },
      ])
      notifications.show({ message: 'Reference added to your library.', color: 'teal' })
      reset()
      onClose()
      onCreated()
    } catch (e) {
      const f = failureFromError(e)
      setErr(f.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal opened={open} onClose={onClose} size="md" title={<Text fw={700}>Add reference</Text>}>
      <Stack gap="md">
        <Group gap="md" wrap="wrap">
          <div style={{ width: 200 }}>
            <Text size="sm" fw={600} mb={6}>
              Type
            </Text>
            <NativeSelect
              value={type}
              onChange={e => setType(e.currentTarget.value)}
              data={BIB_TYPES}
            />
          </div>
          <div style={{ width: 220 }}>
            <Text size="sm" fw={600} mb={6}>
              Citation key
            </Text>
            <TextInput
              value={key}
              onChange={e => setKey(e.currentTarget.value)}
              placeholder="auto-generated"
            />
          </div>
        </Group>
        <div>
          <Text size="sm" fw={600} mb={6}>
            Title <span style={{ color: 'var(--mantine-color-red-6)' }}>*</span>
          </Text>
          <TextInput value={title} onChange={e => setTitle(e.currentTarget.value)} placeholder="e.g. The LaTeX Companion" />
        </div>
        <Group gap="md" wrap="wrap">
          <div style={{ flex: 1, minWidth: 220 }}>
            <Text size="sm" fw={600} mb={6}>
              Author
            </Text>
            <TextInput value={author} onChange={e => setAuthor(e.currentTarget.value)} placeholder="Last, First" />
          </div>
          <div style={{ width: 140 }}>
            <Text size="sm" fw={600} mb={6}>
              Year
            </Text>
            <TextInput value={year} onChange={e => setYear(e.currentTarget.value)} placeholder="2024" />
          </div>
        </Group>
        <div>
          <Text size="sm" fw={600} mb={6}>
            URL
          </Text>
          <TextInput value={url} onChange={e => setUrl(e.currentTarget.value)} placeholder="https://…" />
        </div>
        <div>
          <Text size="sm" fw={600} mb={6}>
            Note
          </Text>
          <Textarea
            value={note}
            onChange={e => setNote(e.currentTarget.value)}
            placeholder="Free text"
            minRows={2}
            maxRows={5}
          />
        </div>
        {err ? <Text size="sm" c="red">{err}</Text> : null}
        <Group justify="flex-end" gap="xs" mt="sm">
          <Button variant="default" onClick={() => { reset(); onClose() }} disabled={busy}>
            Cancel
          </Button>
          <Button color="ollitex" loading={busy} onClick={() => void submit()}>
            Add reference
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}

function ReferencesTable({
  entries,
  trashed,
  onChanged,
}: {
  entries: LibraryEntryApi[]
  trashed: boolean
  onChanged: () => void
}) {
  const [confirm, setConfirm] = useState<{ entry: LibraryEntryApi; permanent: boolean } | null>(null)
  const [busy, setBusy] = useState(false)

  const act = async () => {
    if (!confirm) return
    setBusy(true)
    try {
      if (confirm.permanent) {
        await deleteEntries([confirm.entry._id || confirm.entry.key], true)
        notifications.show({ message: 'Reference permanently deleted.', color: 'gray' })
      } else {
        await restoreEntries([confirm.entry._id || confirm.entry.key])
        notifications.show({ message: 'Reference restored.', color: 'teal' })
      }
      onChanged()
    } catch (e) {
      notifications.show({ message: failureFromError(e).message, color: 'red' })
    } finally {
      setBusy(false)
      setConfirm(null)
    }
  }

  const confirmModal = (
    <ConfirmModal
      open={!!confirm}
      title={confirm?.permanent ? 'Delete permanently?' : 'Restore this reference?'}
      body={
        confirm
          ? confirm.permanent
            ? `“${confirm.entry.key}” will be permanently removed from the library.`
            : `“${confirm.entry.key}” will be moved back to your library.`
          : ''
      }
      confirmLabel={confirm?.permanent ? 'Delete forever' : 'Restore'}
      danger={!!confirm?.permanent}
      loading={busy}
      onCancel={() => setConfirm(null)}
      onConfirm={() => void act()}
    />
  )

  if (entries.length === 0) {
    return (
      <>
        {trashed ? (
          <EmptyState icon="delete_sweep" title="Trash is empty" hint="References you remove will appear here until deleted permanently." />
        ) : (
          <EmptyState
            icon="menu_book"
            title="Your library is empty"
            hint="Add references here and cite them from any project, or import a .bib file inside a project's bibliography."
          />
        )}
        {confirmModal}
      </>
    )
  }

  return (
    <>
    <Table striped highlightOnHover withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
      <Table.Thead>
        <Table.Tr>
          <Table.Th style={{ width: 170 }}>Key</Table.Th>
          <Table.Th style={{ width: 130 }}>Type</Table.Th>
          <Table.Th>Title / details</Table.Th>
          <Table.Th style={{ width: 110 }}>Year</Table.Th>
          <Table.Th style={{ width: 120 }}>Updated</Table.Th>
          <Table.Th style={{ width: 110, textAlign: 'right' }}>Actions</Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {entries.map(entry => (
          <Table.Tr key={entry._id || entry.key}>
            <Table.Td>
              <Text size="sm" fw={600} style={{ fontFamily: 'var(--mantine-font-family-mono)' }} ellipsis>
                {entry.key}
              </Text>
            </Table.Td>
            <Table.Td>
              <Badge size="xs" variant="light" radius="sm" color="blue">
                {entry.type}
              </Badge>
            </Table.Td>
            <Table.Td>
              <Text size="sm" fw={500}>{field(entry, 'title') || '—'}</Text>
              {!!field(entry, 'author') ? <Text size="xs" c="dimmed">{field(entry, 'author')}</Text> : null}
            </Table.Td>
            <Table.Td>
              <Text size="sm">{field(entry, 'year') || '—'}</Text>
            </Table.Td>
            <Table.Td>
              <Text size="sm" c="dimmed">{fmtDate(entry.updatedAt)}</Text>
            </Table.Td>
            <Table.Td style={{ textAlign: 'right' }}>
              <Group gap={4} justify="flex-end">
                {trashed ? (
                  <>
                    <Tooltip label="Restore">
                      <ActionIcon
                        variant="subtle"
                        color="teal"
                        aria-label="Restore"
                        onClick={() => setConfirm({ entry, permanent: false })}
                        style={{ cursor: 'pointer' }}
                      >
                        <Icon name="restore_from_trash" size={18} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="Delete permanently">
                      <ActionIcon
                        variant="subtle"
                        color="red"
                        aria-label="Delete permanently"
                        onClick={() => setConfirm({ entry, permanent: true })}
                        style={{ cursor: 'pointer' }}
                      >
                        <Icon name="delete_forever" size={18} />
                      </ActionIcon>
                    </Tooltip>
                  </>
                ) : (
                  <Tooltip label="Move to trash">
                    <ActionIcon
                      variant="subtle"
                      color="red"
                      aria-label="Move to trash"
                      onClick={() => setConfirm({ entry, permanent: true })}
                      style={{ cursor: 'pointer' }}
                    >
                      <Icon name="delete" size={18} />
                    </ActionIcon>
                  </Tooltip>
                )}
              </Group>
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
    {confirmModal}
    </>
  )
}

export default function LibrarySection() {
  const [libEntries, setLibEntries] = useState<LibraryEntryApi[] | null>(null)
  const [trashEntries, setTrashEntries] = useState<LibraryEntryApi[] | null>(null)
  const [libCount, setLibCount] = useState<number | null>(null)
  const [trashCount, setTrashCount] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [pasteOpen, setPasteOpen] = useState(false)
  const [bibUploadOpen, setBibUploadOpen] = useState(false)
  const [orcidOpen, setOrcidOpen] = useState(false)
  const [zoteroOpen, setZoteroOpen] = useState(false)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [lib, trash, libC, trashC] = await Promise.all([
        listEntries({ limit: 200 }),
        listEntries({ trashed: true, limit: 200 }),
        countEntries(),
        countEntries({ trashed: true }),
      ])
      setLibEntries(lib.items || [])
      setTrashEntries(trash.items || [])
      setLibCount(libC)
      setTrashCount(trashC)
    } catch (e) {
      setLibEntries([])
      setTrashEntries([])
      setError(failureFromError(e).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const importEntries = async (entries: { type: string; id: string; fields: Record<string, string> }[]) => {
    try {
      const mapped = entries.map(e => ({
        key: e.id,
        type: e.type,
        fields: Object.entries(e.fields).map(([name, value]) => ({ name, value })),
      }))
      if (mapped.length === 0) {
        notifications.show({ message: 'No references to import.', color: 'orange' })
        return
      }
      await createEntries(mapped)
      notifications.show({
        message: `Imported ${mapped.length} reference${mapped.length > 1 ? 's' : ''}.`,
        color: 'green',
      })
      void load()
    } catch (e) {
      notifications.show({ message: failureFromError(e).message || 'Import failed.', color: 'red' })
    }
  }

  const importBibtex = async (bibtex: string) => {
    const entries = normaliseOrcidEntryKeys(
      splitImportText(bibtex)
        .filter(i => i.kind === 'bibtex')
        .map(i => (i as { entry: { type: string; id: string; fields: Record<string, string> } }).entry),
    )
    await importEntries(entries)
  }

  if (libEntries === null && !error) return <PageLoading label="Loading your reference library…" />

  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="wrap" gap="sm">
        <Text size="sm" c="dimmed">
          Personal reference library — cite these from any project.
        </Text>
        <Menu width={300} position="bottom-end" withinPortal>
          <Menu.Target>
            <Button
              size="md"
              color="ollitex"
              leftSection={<Icon name="add" size={18} />}
              rightSection={<Icon name="expand_more" size={16} />}
            >
              Add reference
            </Button>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Item leftSection={<Icon name="edit" size={16} />} onClick={() => setAddOpen(true)}>
              Enter manually
            </Menu.Item>
            <Menu.Item leftSection={<Icon name="content_paste" size={16} />} onClick={() => { setPasteOpen(true)}}>
              Paste references (BibTeX, DOI)
            </Menu.Item>
            <Menu.Item leftSection={<Icon name="upload_file" size={16} />} onClick={() => setBibUploadOpen(true)}>
              Upload .bib file
            </Menu.Item>
            <Menu.Divider>Import</Menu.Divider>
            <Menu.Item leftSection={<Icon name="badge" size={16} />} onClick={() => setOrcidOpen(true)}>
              Import from ORCID.org
            </Menu.Item>
            <Menu.Item leftSection={<Icon name="import_contacts" size={16} />} onClick={() => setZoteroOpen(true)}>
              Import from Zotero
            </Menu.Item>
          </Menu.Dropdown>
        </Menu>
      </Group>

      {error ? <PageError label="Couldn’t load the library" detail={error} onRetry={() => void load()} /> : null}

      <Tabs defaultValue="library" style={{ borderWidth: 0 }}>
        <Tabs.List mb="md">
          <Tabs.Tab value="library" leftSection={<Icon name="menu_book" size={16} />}>
            Library{libCount != null ? ` (${libCount})` : ''}
          </Tabs.Tab>
          <Tabs.Tab value="trash" leftSection={<Icon name="delete" size={16} />}>
            Trash{trashCount != null ? ` (${trashCount})` : ''}
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="library">
          {libEntries && libEntries.length > 0 ? (
            <ReferencesTable key="lib" entries={libEntries} trashed={false} onChanged={() => void load()} />
          ) : (
            !libEntries?.length ? (
              <EmptyState
                icon="menu_book"
                title="Your library is empty"
                hint="Add references here and cite them from any project."
                action={
                  <Button size="sm" color="ollitex" leftSection={<Icon name="add" size={16} />} onClick={() => setAddOpen(true)}>
                    Add reference
                  </Button>
                }
              />
            ) : null
          )}
        </Tabs.Panel>

        <Tabs.Panel value="trash">
          <ReferencesTable key="trash" entries={trashEntries || []} trashed={true} onChanged={() => void load()} />
        </Tabs.Panel>
      </Tabs>

      <AddReferenceModal open={addOpen} onClose={() => setAddOpen(false)} onCreated={() => void load()} />

      <BibImportModal
        show={pasteOpen}
        existingIds={[]}
        expectedSource="library"
        onImport={entries => void importEntries(entries)}
        onHidden={() => setPasteOpen(false)}
      />
      <BibImportModal
        show={bibUploadOpen}
        existingIds={[]}
        expectedSource="library"
        onImport={entries => void importEntries(entries)}
        onHidden={() => setBibUploadOpen(false)}
      />
      <OrcidPickerModal show={orcidOpen} handleHide={() => setOrcidOpen(false)} onInsert={text => void importBibtex(text)} />
      <ZoteroPickerModal show={zoteroOpen} handleHide={() => setZoteroOpen(false)} onInsert={text => void importBibtex(text)} />

      {loading && libEntries !== null ? (
        <Text size="xs" c="dimmed">
          Refreshing…
        </Text>
      ) : null}
    </Stack>
  )
}
