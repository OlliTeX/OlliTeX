import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  ActionIcon,
  Badge,
  Button,
  Checkbox,
  Group,
  Menu,
  Modal,
  Stack,
  Table,
  Tabs,
  Text,
  Textarea,
  TextInput,
  Tooltip,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import BibEntryForm from '@modules/bib-editor/frontend/js/components/bib-entry-form'
import type { BibEntry } from '@modules/bib-editor/frontend/js/utils/bib-types'
import {
  splitImportText,
  buildImportRows,
  type BibImportRow,
} from '@modules/bib-editor/frontend/js/utils/bib-import'
import { fetchEntryFromDoi } from '@modules/bib-editor/frontend/js/utils/doi-fetcher'
import { generateCitationKey } from '@modules/bib-editor/frontend/js/utils/bib-parser'
import { getJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import { EmptyState, PageError, PageLoading } from '../../shared/page-state'
import ConfirmModal from '../../shared/confirm-modal'
import {
  listEntries,
  createEntries,
  updateEntry,
  deleteEntries,
  restoreEntries,
  countEntries,
  triggerDownload,
  failureFromError,
} from '@modules/bib-editor/frontend/js/library/library-api'
import type {
  LibraryEntryApi,
  LibraryFieldApi,
} from '@modules/bib-editor/frontend/js/library/library-model'

const PAGE_SIZE = 25

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

/**
 * Owner 4a–4e (2026-09-07): the reference library leaf is fully reworked —
 *  4a  "Enter manually" uses the ORIGINAL full BibEntryForm (type-specific
 *      fields, multiple authors, optional fields, DOI import row) in a
 *      Mantine modal; paste/.bib upload/ORCID/Zotero imports are Mantine
 *      reworks of the legacy OLModal pickers (same APIs)
 *  4b  pagination (server cursor paging, 25/page + search)
 *  4c  multi-select checkboxes + bulk Delete (trash) / Download
 *  4d  per-row & bulk Download (one .bib via /library/references/download)
 *  4e  per-row Edit (BibEntryForm kind=existing → PATCH)
 * All on the existing library REST API (listEntries/createEntries/
 * updateEntry/deleteEntries/restoreEntries/countEntries/download).
 */

// ─────────────────────────────── shared import pipeline ──────────────────
type ImportRowsState = {
  rows: BibImportRow[]
  checked: string[]
  done: boolean
}

function useImportRows(existingIds: string[]) {
  const [text, setText] = useState('')
  const [rows, setRows] = useState<BibImportRow[]>([])
  const [checked, setChecked] = useState<string[]>([])
  const [previewing, setPreviewing] = useState(false)
  const [done, setDone] = useState(false)

  const reset = () => {
    setText('')
    setRows([])
    setChecked([])
    setDone(false)
  }

  const preview = useCallback(
    async (source: string) => {
      setText(source)
      setDone(false)
      setChecked([])
      setRows([])
      setPreviewing(true)
      const items = splitImportText(source)
      const doiItems = items.filter(it => it.kind === 'doi')
      const doiResults: (string | BibEntry | undefined)[] = doiItems.map(() => undefined)
      setRows(buildImportRows(items, doiResults.map(() => undefined), existingIds))
      let i = 0
      for (const it of items) {
        if (it.kind !== 'doi') continue
        const raw = it.raw
        let result: string | BibEntry
        try {
          const fetched = await fetchEntryFromDoi(raw)
          result = {
            type: fetched.type,
            id: fetched.id || generateCitationKey(fetched.fields),
            fields: { ...fetched.fields },
          }
        } catch (err) {
          result = err instanceof Error ? err.message : 'Failed to fetch DOI'
        }
        doiResults[i] = result
        i += 1
        setRows(buildImportRows(items, doiResults, existingIds))
      }
      const finalRows = buildImportRows(items, doiResults, existingIds)
      setRows(finalRows)
      setCheckbox(
        finalRows
          .filter(r => r.status === 'ok' || r.status === 'doi-ok')
          .map(r => r.rowId)
      )
      setDone(true)
      setPreviewing(false)
      // eslint-disable-next-line react-hooks/exhaustive-deps
      return finalRows
    },
    [existingIds]
  )

  const setCheckbox = (ids: string[]) => setChecked(ids)
  const toggleRow = (id: string) =>
    setChecked(prev => (prev.includes(id) ? prev.filter(x => x !== id) : [...prev, id]))
  const importable = rows.filter(r => r.status === 'ok' || r.status === 'doi-ok')
  const selectedEntries = importable.filter(r => checked.includes(r.rowId)).map(r => r.entry as BibEntry)

  return {
    text,
    setText,
    rows,
    checked,
    setChecked,
    previewing,
    done,
    reset,
    preview,
    toggleRow,
    importable,
    selectedEntries,
  }
}

function ImportPreviewTable({
  rows,
  checked,
  onToggle,
  onToggleAll,
}: {
  rows: BibImportRow[]
  checked: string[]
  onToggle: (id: string) => void
  onToggleAll: (v: boolean) => void
}) {
  const importable = rows.filter(r => r.status === 'ok' || r.status === 'doi-ok')
  return (
    <Table striped withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
      <Table.Thead>
        <Table.Tr>
          <Table.Th style={{ width: 40 }}>
            <Checkbox
              checked={importable.length > 0 && importable.every(r => checked.includes(r.rowId))}
              onChange={e => onToggleAll(!!e.currentTarget.checked)}
              aria-label="Select all importable"
            />
          </Table.Th>
          <Table.Th>Key</Table.Th>
          <Table.Th>Type</Table.Th>
          <Table.Th>Title / details</Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {rows.map(r => (
          <Table.Tr key={r.rowId} style={{ opacity: /error|conflict/.test(r.status) ? 0.65 : 1 }}>
            <Table.Td>
              {(r.status === 'ok' || r.status === 'doi-ok') ? (
                <Checkbox checked={checked.includes(r.rowId)} onChange={() => onToggle(r.rowId)} aria-label={`Select ${r.entry?.id || 'entry'}`} />
              ) : (
                <Tooltip label={r.status === 'doi-error' ? r.errorMsg || 'DOI failed to resolve' : `Status: ${r.status}`}>
                  <span aria-hidden>—</span>
                </Tooltip>
              )}
            </Table.Td>
            <Table.Td>
              <Text size="sm" fw={600} style={{ fontFamily: 'var(--mantine-font-family-mono)' }} ellipsis>
                {r.entry?.id || r.raw || '—'}
              </Text>
            </Table.Td>
            <Table.Td>
              <Badge size="xs" variant="light" radius="sm" color="blue">
                {r.entry?.type || r.typeLabel || '—'}
              </Badge>
            </Table.Td>
            <Table.Td>
              <Text size="sm" fw={500}>{r.title || r.heading || '—'}</Text>
              {r.error ? <Text size="xs" c="red">{r.error}</Text> : null}
              {r.conflictWith ? <Text size="xs" c="orange">Key already in library or duplicated (skipped on import).</Text> : null}
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  )
}

// ─────────────────────────────────── 4a paste modal ──────────────────────
function PasteImportModal({
  open,
  existingIds,
  onClose,
  onImported,
}: {
  open: boolean
  existingIds: string[]
  onClose: () => void
  onImported: () => void
}) {
  const imp = useImportRows(existingIds)
  const [importing, setImporting] = useState(false)

  useEffect(() => {
    if (open) imp.reset()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const doImport = async () => {
    if (imp.selectedEntries.length === 0) return
    setImporting(true)
    try {
      const created = await createEntries(
        imp.selectedEntries.map(e => ({
          key: e.id,
          type: e.type,
          fields: Object.entries(e.fields || {}).map(([name, value]) => ({ name, value: String(value ?? '') })),
        }))
      )
      notifications.show({ message: `Imported ${created.length} reference${created.length > 1 ? 's' : ''}.`, color: 'teal' })
      imp.reset()
      onClose()
      onImported()
    } catch (e) {
      notifications.show({ message: failureFromError(e).message, color: 'red' })
    } finally {
      setImporting(false)
    }
  }

  return (
    <Modal opened={open} onClose={onClose} size="lg" title={<Text fw={700}>Paste references</Text>} withinPortal>
      <Stack gap="md">
        <Textarea
          label="BibTeX entries, DOIs, or a mix (one DOI per line)"
          placeholder={'@article{smith2020,\n  title = {…},\n  author = {Smith, J.},\n  year = {2020}\n}\n10.1038/s41586-020-2649-2'}
          value={imp.text}
          onChange={e => imp.setText(e.currentTarget.value)}
          minRows={8}
          maxRows={14}
          style={{ fontFamily: 'var(--mantine-font-family-mono)' }}
        />
        <Group justify="space-between" wrap="wrap" gap="xs">
          <Text size="sm" c="dimmed">
            Paste anything from a bibliography — BibTeX blocks and bare DOIs are both resolved.
          </Text>
          <Button color="ollitex" loading={imp.previewing} disabled={!imp.text.trim()} onClick={() => void imp.preview(imp.text)}>
            Preview
          </Button>
        </Group>
        {imp.rows.length > 0 ? (
          <>
            <ImportPreviewTable
              rows={imp.rows}
              checked={imp.checked}
              onToggle={imp.toggleRow}
              onToggleAll={v => imp.setChecked(v ? imp.importable.map(r => r.rowId) : [])}
            />
            <Group justify="space-between" wrap="wrap" gap="xs">
              <Button variant="default" size="sm" onClick={() => imp.preview(imp.text)}>
                Back to editing
              </Button>
              <Button color="ollitex" loading={importing} disabled={imp.selectedEntries.length === 0} onClick={() => void doImport()}>
                Import {imp.selectedEntries.length > 0 ? `(${imp.selectedEntries.length})` : ''}
              </Button>
            </Group>
          </>
        ) : null}
      </Stack>
    </Modal>
  )
}

// ─────────────────────────────────── 4a upload modal ─────────────────────
function BibUploadModal({
  open,
  existingIds,
  onClose,
  onImported,
}: {
  open: boolean
  existingIds: string[]
  onClose: () => void
  onImported: () => void
}) {
  const imp = useImportRows(existingIds)
  const fileRef = useRef<HTMLInputElement>(null)
  const [fileName, setFileName] = useState('')
  const [importing, setImporting] = useState(false)

  useEffect(() => {
    if (open) {
      imp.reset()
      setFileName('')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const onFile = async (f: File) => {
    setFileName(f.name)
    try {
      const text = await f.text()
      await imp.preview(text)
    } catch (e) {
      notifications.show({ message: `Could not read ${f.name}`, color: 'red' })
    }
  }

  const doImport = async () => {
    if (imp.selectedEntries.length === 0) return
    setImporting(true)
    try {
      const created = await createEntries(
        imp.selectedEntries.map(e => ({
          key: e.id,
          type: e.type,
          fields: Object.entries(e.fields || {}).map(([name, value]) => ({ name, value: String(value ?? '') })),
        }))
      )
      notifications.show({ message: `Imported ${created.length} reference${created.length > 1 ? 's' : ''} from ${fileName}.`, color: 'teal' })
      imp.reset()
      setFileName('')
      onClose()
      onImported()
    } catch (e) {
      notifications.show({ message: failureFromError(e).message, color: 'red' })
    } finally {
      setImporting(false)
    }
  }

  return (
    <Modal opened={open} onClose={onClose} size="lg" title={<Text fw={700}>Upload a .bib file</Text>} withinPortal>
      <Stack gap="md">
        <Group justify="space-between" align="flex-end" gap="xs" wrap="wrap">
          <Group gap="xs">
            <Button variant="default" onClick={() => fileRef.current?.click()} leftSection={<Icon name="upload_file" size={16} />}>
              Choose .bib file
            </Button>
            <input
              ref={fileRef}
              type="file"
              accept=".bib,.txt,application/x-bibtex,text/plain"
              style={{ display: 'none' }}
              onChange={e => {
                const f = e.target.files?.[0]
                if (f) void onFile(f)
                e.currentTarget.value = ''
              }}
            />
            {fileName ? <Text size="sm" c="dimmed">{fileName}</Text> : null}
          </Group>
          {imp.rows.length > 0 ? (
            <Group gap="xs">
              <Button variant="default" size="sm" onClick={() => { imp.reset(); setFileName(''); fileRef.current?.click?.() }} disabled={!fileName}>
                Another file
              </Button>
              <Button color="ollitex" loading={importing} disabled={imp.selectedEntries.length === 0} onClick={() => void doImport()}>
                Import {imp.selectedEntries.length > 0 ? `(${imp.selectedEntries.length})` : ''}
              </Button>
            </Group>
          ) : null}
        </Group>
        {imp.rows.length > 0 ? (
          <ImportPreviewTable
            rows={imp.rows}
            checked={imp.checked}
            onToggle={imp.toggleRow}
            onToggleAll={v => imp.setChecked(v ? imp.importable.map(r => r.rowId) : [])}
          />
        ) : (
          <EmptyState
            icon="description"
            title="No file yet"
            hint="Choose a .bib file (BibTeX) — every entry is parsed and can be picked individually before importing."
          />
        )}
      </Stack>
    </Modal>
  )
}

// ─────────────────────────────────── 4a ORCID modal ──────────────────────
type OrcidWork = { putCode?: number; title?: string; year?: string; type?: string }

function OrcidImportModal({
  open,
  onClose,
  onImported,
}: {
  open: boolean
  onClose: () => void
  onImported: () => void
}) {
  const [mode, setMode] = useState<'search' | 'works'>('search')
  const [nameQuery, setNameQuery] = useState('')
  const [results, setResults] = useState<Array<{ orcid: string; name?: string; lastStatus?: string }>>([])
  const [searching, setSearching] = useState(false)
  const [orcid, setOrcid] = useState('')
  const [works, setWorks] = useState<OrcidWork[]>([])
  const [worksLoading, setWorksLoading] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [importing, setImporting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (open) {
      setMode('search')
      setNameQuery('')
      setResults([])
      setOrcid('')
      setWorks([])
      setSelected([])
      setError(null)
    }
  }, [open])

  const searchName = async () => {
    setSearching(true)
    setError(null)
    try {
      const data = await getJSON<{ results: Array<{ orcid: string; name?: string; lastStatus?: string }> }>(
        `/orcid-picker/search?q=${encodeURIComponent(nameQuery.trim())}`
      )
      setResults(data?.results || [])
      if ((data?.results || []).length === 0) setError('No ORCID records found for that name.')
    } catch (e) {
      setError(failureFromError(e).message)
    } finally {
      setSearching(false)
    }
  }

  const pickOrcid = async (o: string) => {
    setMode('works')
    setOrcid(o)
    setWorks([])
    setSelected([])
    setWorksLoading(true)
    try {
      const data = await getJSON<{ works: OrcidWork[] }>(`/orcid-picker/works?orcid=${encodeURIComponent(o)}`)
      setWorks(data?.works || [])
    } catch (e) {
      setError(failureFromError(e).message)
    } finally {
      setWorksLoading(false)
    }
  }

  const toggleWork = (code: number) => {
    const key = String(code)
    setSelected(prev => (prev.includes(key) ? prev.filter(x => x !== key) : [...prev, key]))
  }

  const doImport = async () => {
    const codes = selected.map(s => Number(s))
    if (codes.length === 0) return
    setImporting(true)
    setError(null)
    let imported = 0
    try {
      for (const code of codes) {
        try {
          const data = await getJSON<{ bibtex: string }>(
            `/orcid-picker/fetch-bib?orcid=${encodeURIComponent(orcid)}&putCode=${encodeURIComponent(String(code))}`
          )
          const items = splitImportText(data?.bibtex || '').filter(i => i.kind === 'bibtex')
          if (items.length > 0) {
            const entry = (items[0] as { entry: BibEntry }).entry
            await createEntries([{
              key: entry.id,
              type: entry.type,
              fields: Object.entries(entry.fields || {}).map(([name, value]) => ({ name, value: String(value ?? '') })),
            }])
            imported += 1
          }
        } catch {
          // keep importing the rest (partial import is the legacy behaviour)
        }
      }
      if (imported > 0) notifications.show({ message: `Imported ${imported} reference${imported > 1 ? 's' : ''} from ORCID.`, color: 'teal' })
      else notifications.show({ message: 'No references could be imported from that ORCID record.', color: 'orange' })
      onClose()
      if (imported > 0) onImported()
    } catch (e) {
      setError(failureFromError(e).message)
    } finally {
      setImporting(false)
    }
  }

  return (
    <Modal opened={open} onClose={onClose} size="lg" title={<Text fw={700}>Import from ORCID.org</Text>} withinPortal>
      <Stack gap="md">
        {mode === 'search' ? (
          <>
            <Group gap="xs">
              <TextInput
                label="Researcher name (searches public ORCID records)"
                value={nameQuery}
                onChange={e => setNameQuery(e.currentTarget.value)}
                placeholder="e.g. Ada Lovelace"
                style={{ flex: 1 }}
              />
              <Button color="ollitex" loading={searching} disabled={!nameQuery.trim()} onClick={() => void searchName()}>
                Search
              </Button>
            </Group>
            {error ? <Text size="sm" c="red">{error}</Text> : null}
            {results.length > 0 ? (
              <Stack gap={6}>
                {results.map(r => (
                  <Group key={r.orcid} justify="space-between" wrap="nowrap" gap="xs" style={{ border: '1px solid var(--mantine-color-default-border)', borderRadius: 8, padding: '10px 12px' }}>
                    <div style={{ minWidth: 0 }}>
                      <Text size="sm" fw={600} ellipsis>{r.name || r.orcid}</Text>
                      <Text size="xs" c="dimmed">{r.orcid}{r.lastStatus ? ` · ${r.lastStatus}` : ''}</Text>
                    </div>
                    <Button size="xs" variant="light" color="ollitex" onClick={() => void pickOrcid(r.orcid)}>
                      Browse works
                    </Button>
                  </Group>
                ))}
              </Stack>
            ) : null}
          </>
        ) : (
          <>
            <Group justify="space-between" gap="xs" wrap="wrap">
              <Text size="sm" c="dimmed">
                ORCID <Text span inline fw={600}>{orcid}</Text> — select the works to import.
              </Text>
              <Button size="xs" variant="default" onClick={() => setMode('search')}>
                Search again
              </Button>
            </Group>
            {error ? <Text size="sm" c="red">{error}</Text> : null}
            {worksLoading ? (
              <PageLoading label="Loading works…" />
            ) : works.length === 0 ? (
              <EmptyState icon="article" title="No works found" hint="This ORCID record has no works available for import." />
            ) : (
              <>
                <Table striped withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
                  <Table.Tbody>
                    {works.map(w => (
                      <Table.Tr key={String(w.putCode)}>
                        <Table.Td style={{ width: 40 }}>
                          <Checkbox checked={selected.includes(String(w.putCode))} onChange={() => w.putCode !== undefined && toggleWork(w.putCode)} aria-label={`Select ${w.title || 'work'}`} />
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" fw={500}>{w.title || 'Untitled'}</Text>
                          <Text size="xs" c="dimmed">{[w.type, w.year].filter(Boolean).join(' · ')}</Text>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
                <Group justify="flex-end" gap="xs">
                  <Button color="ollitex" loading={importing} disabled={selected.length === 0} onClick={() => void doImport()}>
                    Import {selected.length > 0 ? `(${selected.length})` : ''}
                  </Button>
                </Group>
              </>
            )}
          </>
        )}
      </Stack>
    </Modal>
  )
}

// ─────────────────────────────────── 4a Zotero modal ─────────────────────
type ZLibrary = { id: string; kind: string; name: string }
type ZCollection = { key: string; name: string }
type ZItem = { key: string; itemType: string; title: string; date?: string }

function ZoteroImportModal({
  open,
  onClose,
  onImported,
}: {
  open: boolean
  onClose: () => void
  onImported: () => void
}) {
  const [libraries, setLibraries] = useState<ZLibrary[]>([])
  const [libIdx, setLibIdx] = useState(0)
  const [collections, setCollections] = useState<ZCollection[]>([])
  const [collectionKey, setCollectionKey] = useState('')
  const [step, setStep] = useState<'library' | 'items'>('library')
  const [items, setItems] = useState<ZItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [importing, setImporting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notLinked, setNotLinked] = useState(false)

  useEffect(() => {
    if (open) {
      setLibraries([])
      setLibIdx(0)
      setCollections([])
      setCollectionKey('')
      setStep('library')
      setItems([])
      setSelected([])
      setError(null)
      setNotLinked(false)
      getJSON<ZLibrary[]>('/user/zotero/picker/libraries')
        .then(data => setLibraries(Array.isArray(data) ? data : []))
        .catch(err => {
          const f = failureFromError(err)
          if (/not linked|zotero_not_linked/i.test(f.message)) setNotLinked(true)
          else setError(f.message)
        })
    }
  }, [open])

  const current = libraries[libIdx]

  const loadCollections = useCallback((lib: ZLibrary) => {
    setCollections([])
    setCollectionKey('')
    const qs = new URLSearchParams({ libraryKind: lib.kind })
    if (lib.kind === 'group') qs.set('library', lib.id)
    getJSON<ZCollection[]>(`/user/zotero/picker/collections?${qs.toString()}`)
      .then(data => setCollections(Array.isArray(data) ? data : []))
      .catch(() => setCollections([]))
  }, [])

  useEffect(() => {
    if (open && current) loadCollections(current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, libIdx, libraries.length])

  const browse = () => {
    if (!current) return
    setStep('items')
    setError(null)
    setItems([])
    setSelected([])
    setLoading(true)
    const qs = new URLSearchParams({ libraryKind: current.kind, limit: '100' })
    if (current.kind === 'group') qs.set('library', current.id)
    if (collectionKey) qs.set('collection', collectionKey)
    getJSON<{ items: ZItem[]; total: number }>(`/user/zotero/picker/items?${qs.toString()}`)
      .then(data => {
        setItems(data?.items || [])
        setTotal(data?.total || (data?.items || []).length)
      })
      .catch(err => setError(failureFromError(err).message))
      .finally(() => setLoading(false))
  }

  const toggle = (k: string) => setSelected(prev => (prev.includes(k) ? prev.filter(x => x !== k) : [...prev, k]))

  const doImport = async () => {
    if (!current || selected.length === 0) return
    setImporting(true)
    setError(null)
    try {
      const qs = new URLSearchParams({
        libraryKind: current.kind,
        keys: selected.join(','),
      })
      if (current.kind === 'group') qs.set('library', current.id)
      const data = await getJSON<{ bibtex: string }>(`/user/zotero/picker/bibtex?${qs.toString()}`)
      const items = splitImportText(data?.bibtex || '').filter(i => i.kind === 'bibtex')
      const entries = items.map(i => (i as { entry: BibEntry }).entry)
      if (entries.length === 0) {
        notifications.show({ message: 'No references to import.', color: 'orange' })
        return
      }
      await createEntries(
        entries.map(e => ({
          key: e.id,
          type: e.type,
          fields: Object.entries(e.fields || {}).map(([name, value]) => ({ name, value: String(value ?? '') })),
        }))
      )
      notifications.show({ message: `Imported ${entries.length} reference${entries.length > 1 ? 's' : ''} from Zotero.`, color: 'teal' })
      onClose()
      onImported()
    } catch (e) {
      setError(failureFromError(e).message)
    } finally {
      setImporting(false)
    }
  }

  return (
    <Modal opened={open} onClose={onClose} size="lg" title={<Text fw={700}>Import from Zotero</Text>} withinPortal>
      <Stack gap="md">
        {notLinked ? (
          <EmptyState
            icon="link_off"
            title="Zotero is not linked"
            hint="Link your Zotero account in My settings → Reference managers → Zotero, then import from here."
          />
        ) : step === 'library' ? (
          <>
            <Group gap="xs" wrap="wrap" align="flex-end">
              <div style={{ minWidth: 260 }}>
                {libraries.length > 0 ? (
                  <select
                    value={String(libIdx)}
                    onChange={e => setLibIdx(Number(e.currentTarget.value))}
                    style={{
                      width: '100%',
                      padding: '8px 10px',
                      borderRadius: 8,
                      border: '1px solid var(--mantine-color-default-border)',
                      background: 'var(--mantine-color-body)',
                      color: 'var(--mantine-color-text)',
                    }}
                    aria-label="Zotero library"
                  >
                    {libraries.map((l, i) => (
                      <option key={l.id} value={i}>{l.name}{l.kind === 'group' ? ' (group)' : ''}</option>
                    ))}
                  </select>
                ) : (
                  <Text size="sm" c="dimmed">Loading libraries…</Text>
                )}
              </div>
              {current ? (
                <select
                  value={collectionKey}
                  onChange={e => setCollectionKey(e.currentTarget.value)}
                  style={{
                    minWidth: 220,
                    padding: '8px 10px',
                    borderRadius: 8,
                    border: '1px solid var(--mantine-color-default-border)',
                    background: 'var(--mantine-color-body)',
                    color: 'var(--mantine-color-text)',
                  }}
                  aria-label="Zotero collection"
                >
                  <option value="">Whole library</option>
                  {collections.map(c => (
                    <option key={c.key} value={c.key}>{c.name}</option>
                  ))}
                </select>
              ) : null}
            </Group>
            {error ? <Text size="sm" c="red">{error}</Text> : null}
            {libraries.length > 0 ? (
              <Button color="ollitex" onClick={() => browse()} leftSection={<Icon name="search" size={16} />}>
                Browse items
              </Button>
            ) : null}
          </>
        ) : (
          <>
            <Group justify="space-between" gap="xs" wrap="wrap">
              <Text size="sm" c="dimmed">
                {current?.name}{collectionKey ? ' · selected collection' : ' · whole library'} — {total} item{total === 1 ? '' : 's'}
              </Text>
              <Button size="xs" variant="default" onClick={() => setStep('library')}>
                Back to libraries
              </Button>
            </Group>
            {error ? <Text size="sm" c="red">{error}</Text> : null}
            {loading ? (
              <PageLoading label="Loading items…" />
            ) : items.length === 0 ? (
              <EmptyState icon="menu_book" title="No items in this selection" hint="Pick a different library or collection." />
            ) : (
              <>
                <Table striped withTableBorder style={{ borderRadius: 10, overflow: 'hidden', maxHeight: 420, overflowY: 'auto' }}>
                  <Table.Tbody>
                    {items.map(it => (
                      <Table.Tr key={it.key}>
                        <Table.Td style={{ width: 40 }}>
                          <Checkbox checked={selected.includes(it.key)} onChange={() => toggle(it.key)} aria-label={`Select ${it.title || 'item'}`} />
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm" fw={500}>{it.title || 'Untitled'}</Text>
                          <Text size="xs" c="dimmed">{[it.itemType, it.date].filter(Boolean).join(' · ')}</Text>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
                <Group justify="flex-end" gap="xs">
                  <Button color="ollitex" loading={importing} disabled={selected.length === 0} onClick={() => void doImport()}>
                    Import {selected.length > 0 ? `(${selected.length})` : ''}
                  </Button>
                </Group>
              </>
            )}
          </>
        )}
      </Stack>
    </Modal>
  )
}

// ─────────────────────────────────── 4a/4e manual + edit modal ───────────
function ManualEntryModal({
  open,
  editing,
  existingIds,
  onClose,
  onSaved,
}: {
  open: boolean
  editing: LibraryEntryApi | null
  existingIds: string[]
  onClose: () => void
  onSaved: () => void
}) {
  const [entry, setEntry] = useState<BibEntry | null>(null)

  useEffect(() => {
    if (!open) return
    if (editing) {
      setEntry({
        type: editing.type,
        id: editing.key,
        fields: Object.fromEntries((editing.fields || []).map(f => [f.name, f.value])),
      })
    } else {
      setEntry({ type: 'article', id: '', fields: {} })
    }
  }, [open, editing])

  if (!open || !entry) return null

  const handleChecked = async (e: BibEntry, kind: 'existing' | 'new', originalId: string | null) => {
    const cleanFields = Object.entries(e.fields || {}).filter(([, v]) => String(v ?? '').trim() !== '').map(([name, value]) => ({ name, value: String(value) }))
    try {
      if (kind === 'existing' && originalId) {
        await updateEntry(originalId, { key: e.id, type: e.type, fields: cleanFields })
        notifications.show({ message: `Updated “${e.id}”.`, color: 'teal' })
      } else {
        await createEntries([{ key: e.id, type: e.type, fields: cleanFields }])
        notifications.show({ message: `Added “${e.id}” to your library.`, color: 'teal' })
      }
      onClose()
      onSaved()
    } catch (err) {
      notifications.show({ message: failureFromError(err).message, color: 'red' })
    }
  }

  return (
    <Modal opened={open} onClose={onClose} size="lg" title={<Text fw={700}>{editing ? `Edit “${editing.key}”` : 'Enter reference manually'}</Text>} withinPortal>
      {editing ? (
        <BibEntryForm
          entry={entry}
          kind="existing"
          originalId={editing.key}
          existingIds={existingIds}
          onFormChange={() => undefined}
          onChecked={(e, kind, orig) => void handleChecked(e, kind, orig)}
          variant="modal"
          submitText="Save changes"
        />
      ) : (
        <BibEntryForm
          entry={entry}
          kind="new"
          originalId={null}
          existingIds={existingIds}
          onFormChange={() => undefined}
          onChecked={(e, kind) => void handleChecked(e, kind, null)}
          variant="modal"
          submitText="Add reference"
        />
      )}
    </Modal>
  )
}

// ─────────────────────────────────────────── main section ────────────────
function LibraryTable({
  entries,
  trashed,
  selected,
  onToggle,
  onToggleAll,
  onEdit,
  onDownload,
  onTrash,
  onRestore,
  onPurge,
}: {
  entries: LibraryEntryApi[]
  trashed: boolean
  selected: string[]
  onToggle: (key: string) => void
  onToggleAll: (v: boolean) => void
  onEdit: (e: LibraryEntryApi) => void
  onDownload: (keys: string[]) => void
  onTrash: (keys: string[]) => void
  onRestore: (keys: string[]) => void
  onPurge: (keys: string[]) => void
}) {
  const allSelected = entries.length > 0 && entries.every(e => selected.includes(e._id))
  return (
    <Table striped highlightOnHover withTableBorder style={{ borderRadius: 10, overflow: 'hidden' }}>
      <Table.Thead>
        <Table.Tr>
          <Table.Th style={{ width: 40 }}>
            <Checkbox checked={allSelected} onChange={e => onToggleAll(!!e.currentTarget.checked)} aria-label="Select all" />
          </Table.Th>
          <Table.Th style={{ width: 160 }}>Key</Table.Th>
          <Table.Th style={{ width: 120 }}>Type</Table.Th>
          <Table.Th>Title / details</Table.Th>
          <Table.Th style={{ width: 90 }}>Year</Table.Th>
          <Table.Th style={{ width: 110 }}>Updated</Table.Th>
          <Table.Th style={{ width: 150, textAlign: 'right' }}>Actions</Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {entries.map(entry => (
          <Table.Tr key={entry._id || entry.key} style={{ background: selected.includes(entry._id) ? 'var(--mantine-color-ollitex-0, rgba(30,107,65,0.06))' : undefined }}>
            <Table.Td>
              <Checkbox
                checked={selected.includes(entry._id)}
                onChange={() => onToggle(entry._id)}
                aria-label={`Select ${entry.key}`}
              />
            </Table.Td>
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
              {field(entry, 'author') ? <Text size="xs" c="dimmed">{field(entry, 'author')}</Text> : null}
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
                      <ActionIcon variant="subtle" color="teal" aria-label="Restore" onClick={() => onRestore([entry._id])} style={{ cursor: 'pointer' }}>
                        <Icon name="restore_from_trash" size={18} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="Delete permanently">
                      <ActionIcon variant="subtle" color="red" aria-label="Delete permanently" onClick={() => onPurge([entry._id])} style={{ cursor: 'pointer' }}>
                        <Icon name="delete_forever" size={18} />
                      </ActionIcon>
                    </Tooltip>
                  </>
                ) : (
                  <>
                    <Tooltip label="Edit">
                      <ActionIcon variant="subtle" aria-label="Edit" onClick={() => onEdit(entry)} style={{ cursor: 'pointer' }}>
                        <Icon name="edit" size={18} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="Download (.bib)">
                      <ActionIcon variant="subtle" aria-label="Download (.bib)" onClick={() => onDownload([entry._id])} style={{ cursor: 'pointer' }}>
                        <Icon name="download" size={18} />
                      </ActionIcon>
                    </Tooltip>
                    <Tooltip label="Move to trash">
                      <ActionIcon variant="subtle" color="red" aria-label="Move to trash" onClick={() => onTrash([entry._id])} style={{ cursor: 'pointer' }}>
                        <Icon name="delete" size={18} />
                      </ActionIcon>
                    </Tooltip>
                  </>
                )}
              </Group>
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  )
}

export default function LibrarySection() {
  const [tab, setTab] = useState<'library' | 'trash'>('library')
  const [trashed, setTrashed] = useState(false)
  const [entries, setEntries] = useState<LibraryEntryApi[] | null>(null)
  const [cursor, setCursor] = useState<string | null>(null)
  const [total, setTotal] = useState<number | null>(null)
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [confirm, setConfirm] = useState<{ keys: string[]; kind: 'trash' | 'purge' | 'restore' } | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)
  const [manualOpen, setManualOpen] = useState(false)
  const [editing, setEditing] = useState<LibraryEntryApi | null>(null)
  const [pasteOpen, setPasteOpen] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [orcidOpen, setOrcidOpen] = useState(false)
  const [zoteroOpen, setZoteroOpen] = useState(false)

  const existingIds = useMemo(() => (entries || []).map(e => e.key), [entries])

  const load = useCallback(
    async (
      base: { cursor?: string; search?: string; trashed?: boolean } = {},
      mode: 'replace' | 'append' = 'replace'
    ) => {
      if (mode === 'replace') {
        setEntries(null)
        setError(null)
      }
      try {
        const [page, count] = await Promise.all([
          listEntries({ limit: PAGE_SIZE, cursor: base.cursor || null, search: base.search, trashed: base.trashed }),
          countEntries({ search: base.search, trashed: base.trashed }),
        ])
        setEntries(prev => (mode === 'append' && prev ? [...prev, ...(page.items || [])] : page.items || []))
        setCursor(page.nextCursor || null)
        setTotal(count)
      } catch (e) {
        if (mode === 'append') {
          notifications.show({ message: failureFromError(e).message, color: 'red' })
        } else {
          setEntries([])
          setError(failureFromError(e).message)
        }
      }
    },
    []
  )

  const refresh = useCallback(() => {
    setSelected([])
    void load({ search, trashed }, 'replace')
  }, [load, search, trashed])

  useEffect(() => {
    void load({ search, trashed }, 'replace')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [trashed, search])

  const switchTab = (t: 'library' | 'trash') => {
    setTab(t)
    setTrashed(t === 'trash')
    setSelected([])
    setEntries(null)
    setCursor(null)
    void load({ search, trashed: t === 'trash' }, 'replace')
  }

  const toggle = (key: string) => setSelected(prev => (prev.includes(key) ? prev.filter(k => k !== key) : [...prev, key]))
  const toggleAll = (v: boolean) => setSelected(v ? (entries || []).map(e => e._id) : [])

  const doDownload = (ids: string[]) => {
    if (ids.length === 0) return
    triggerDownload({ ids })
    setSelected([])
  }

  const runConfirm = async () => {
    if (!confirm) return
    setConfirmBusy(true)
    try {
      if (confirm.kind === 'trash') {
        const n = await deleteEntries(confirm.keys, false)
        notifications.show({ message: `Moved ${n} reference${n === 1 ? '' : 's'} to trash.`, color: 'gray' })
      } else if (confirm.kind === 'restore') {
        const n = await restoreEntries(confirm.keys)
        notifications.show({ message: `Restored ${n} reference${n === 1 ? '' : 's'}.`, color: 'teal' })
      } else {
        const n = await deleteEntries(confirm.keys, true)
        notifications.show({ message: `Permanently deleted ${n} reference${n === 1 ? '' : 's'}.`, color: 'gray' })
      }
      setConfirm(null)
      setSelected([])
      await refresh()
    } catch (e) {
      notifications.show({ message: failureFromError(e).message, color: 'red' })
      setConfirm(null)
    } finally {
      setConfirmBusy(false)
    }
  }

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const onSearchInput = (v: string) => {
    setSearchInput(v)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => setSearch(v.trim()), 400)
  }

  if (entries === null && !error) return <PageLoading label="Loading your reference library…" />

  return (
    <Stack gap="md">
      <Group justify="space-between" wrap="wrap" gap="sm">
        <TextInput
          leftSection={<Icon name="search" size={18} />}
          value={searchInput}
          onChange={e => onSearchInput(e.currentTarget.value)}
          placeholder="Search key, title, author…"
          size="md"
          style={{ maxWidth: 420, width: '100%' }}
        />
        <Menu width={300} position="bottom-end" withinPortal>
          <Menu.Target>
            <Button size="md" color="ollitex" leftSection={<Icon name="add" size={18} />} rightSection={<Icon name="expand_more" size={16} />}>
              Add reference
            </Button>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Item leftSection={<Icon name="edit" size={16} />} onClick={() => { setEditing(null); setManualOpen(true) }}>
              Enter manually
              <Text size="xs" c="dimmed" mt={2} fw={400}>
                Full entry form — fields adapt to the reference type
              </Text>
            </Menu.Item>
            <Menu.Item leftSection={<Icon name="content_paste" size={16} />} onClick={() => setPasteOpen(true)}>
              Paste references (BibTeX, DOI)
              <Text size="xs" c="dimmed" mt={2} fw={400}>
                BibTeX blocks and bare DOIs
              </Text>
            </Menu.Item>
            <Menu.Item leftSection={<Icon name="upload_file" size={16} />} onClick={() => setUploadOpen(true)}>
              Upload .bib file
              <Text size="xs" c="dimmed" mt={2} fw={400}>
                A single BibTeX (.bib) file
              </Text>
            </Menu.Item>
            <Menu.Divider>Import</Menu.Divider>
            <Menu.Item leftSection={<Icon name="badge" size={16} />} onClick={() => setOrcidOpen(true)}>
              Import from ORCID.org
              <Text size="xs" c="dimmed" mt={2} fw={400}>
                Search by name or ORCID iD
              </Text>
            </Menu.Item>
            <Menu.Item leftSection={<Icon name="import_contacts" size={16} />} onClick={() => setZoteroOpen(true)}>
              Import from Zotero
              <Text size="xs" c="dimmed" mt={2} fw={400}>
                Browse your Zotero libraries and collections
              </Text>
            </Menu.Item>
          </Menu.Dropdown>
        </Menu>
      </Group>

      {error ? <PageError label="Couldn’t load the library" detail={error} onRetry={refresh} /> : null}

      <Tabs value={tab} onChange={v => switchTab(v as 'library' | 'trash')} style={{ borderWidth: 0 }}>
        <Tabs.List mb="md">
          <Tabs.Tab value="library" leftSection={<Icon name="menu_book" size={16} />}>
            Library{total != null && !trashed ? ` (${total})` : ''}
          </Tabs.Tab>
          <Tabs.Tab value="trash" leftSection={<Icon name="delete" size={16} />}>
            Trash
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value={tab}>
          <Stack gap="md">
            {selected.length > 0 ? (
              <Group gap="xs" wrap="wrap" align="center" style={{ background: 'rgba(30,107,65,0.06)', borderRadius: 10, padding: '8px 12px', border: '1px solid rgba(30,107,65,0.2)' }}>
                <Text size="sm" fw={700}>
                  {selected.length} selected
                </Text>
                <ActionIcon label="Clear selection" variant="subtle" onClick={() => setSelected([])} aria-label="Clear selection">
                  <Icon name="close" size={16} />
                </ActionIcon>
                <Group gap={6} wrap="nowrap">
                  <Button size="xs" variant="light" color="ollitex" leftSection={<Icon name="download" size={14} />} onClick={() => doDownload(selected)}>
                    Download
                  </Button>
                  {trashed ? (
                    <>
                      <Button size="xs" variant="light" color="teal" leftSection={<Icon name="restore_from_trash" size={14} />} onClick={() => setConfirm({ keys: selected, kind: 'restore' })}>
                        Restore
                      </Button>
                      <Button size="xs" variant="light" color="red" leftSection={<Icon name="delete_forever" size={14} />} onClick={() => setConfirm({ keys: selected, kind: 'purge' })}>
                        Delete permanently
                      </Button>
                    </>
                  ) : (
                    <Button size="xs" variant="light" color="red" leftSection={<Icon name="delete" size={14} />} onClick={() => setConfirm({ keys: selected, kind: 'trash' })}>
                      Move to trash
                    </Button>
                  )}
                </Group>
              </Group>
            ) : null}

            {entries && entries.length > 0 ? (
              <LibraryTable
                entries={entries}
                trashed={trashed}
                selected={selected}
                onToggle={toggle}
                onToggleAll={toggleAll}
                onEdit={e => { setEditing(e); setManualOpen(true) }}
                onDownload={keys => doDownload(keys)}
                onTrash={keys => setConfirm({ keys, kind: 'trash' })}
                onRestore={keys => setConfirm({ keys, kind: 'restore' })}
                onPurge={keys => setConfirm({ keys, kind: 'purge' })}
              />
            ) : (
              !error ? (
                <EmptyState
                  icon={trashed ? 'delete_sweep' : 'menu_book'}
                  title={trashed ? 'Trash is empty' : search ? 'No references match your search' : 'Your library is empty'}
                  hint={
                    search
                      ? `Nothing matched “${search}”.`
                      : trashed
                        ? 'References you move to the trash appear here until you delete them permanently.'
                        : 'Add references — manually, by pasting BibTeX/DOI, from an ORCID or Zotero, or a .bib file — and cite them from any project.'
                  }
                  action={
                    !search && !trashed ? (
                      <Button size="sm" color="ollitex" leftSection={<Icon name="add" size={16} />} onClick={() => { setEditing(null); setManualOpen(true) }}>
                        Add reference
                      </Button>
                    ) : undefined
                  }
                />
              ) : null
            )}

            {entries && entries.length > 0 && cursor ? (
              <Group justify="center" gap="xs">
                <Button
                  variant="default"
                  size="sm"
                  loading={loadingMore}
                  leftSection={cursor ? <Icon name="expand_more" size={16} /> : undefined}
                  onClick={() => {
                    setLoadingMore(true)
                    void load({ cursor, search, trashed }, 'append').finally(() => setLoadingMore(false))
                  }}
                >
                  Load more{total != null ? ` (${entries.length} of ${total})` : ''}
                </Button>
              </Group>
            ) : null}
            {entries && entries.length > 0 && !cursor && total != null ? (
              <Text size="xs" c="dimmed" ta="center">
                Showing all {entries.length} result{entries.length === 1 ? '' : 's'}
                {total !== entries.length ? ` (of ${total} matching)` : ''}
                {search ? ` for “${search}”` : ''}.
              </Text>
            ) : null}
          </Stack>
        </Tabs.Panel>
      </Tabs>

      <ManualEntryModal
        open={manualOpen}
        editing={editing}
        existingIds={existingIds}
        onClose={() => { setManualOpen(false); setEditing(null) }}
        onSaved={() => { setManualOpen(false); setEditing(null); refresh() }}
      />
      <PasteImportModal open={pasteOpen} existingIds={existingIds} onClose={() => setPasteOpen(false)} onImported={refresh} />
      <BibUploadModal open={uploadOpen} existingIds={existingIds} onClose={() => setUploadOpen(false)} onImported={refresh} />
      <OrcidImportModal open={orcidOpen} onClose={() => setOrcidOpen(false)} onImported={refresh} />
      <ZoteroImportModal open={zoteroOpen} onClose={() => setZoteroOpen(false)} onImported={refresh} />

      <ConfirmModal
        open={!!confirm}
        title={confirm?.kind === 'trash' ? 'Move to trash?' : confirm?.kind === 'restore' ? 'Restore references?' : 'Delete permanently?'}
        body={
          confirm
            ? confirm.kind === 'trash'
              ? `${confirm.keys.length} reference${confirm.keys.length === 1 ? '' : 's'} will be moved to the trash (recoverable until permanently deleted).`
              : confirm.kind === 'restore'
                ? `${confirm.keys.length} reference${confirm.keys.length === 1 ? '' : 's'} will be moved back to your library.`
                : `${confirm.keys.length} reference${confirm.keys.length === 1 ? '' : 's'} will be permanently removed from the library. This cannot be undone.`
            : ''
        }
        confirmLabel={confirm?.kind === 'trash' ? 'Move to trash' : confirm?.kind === 'restore' ? 'Restore' : 'Delete forever'}
        danger={confirm?.kind === 'purge'}
        loading={confirmBusy}
        onCancel={() => setConfirm(null)}
        onConfirm={() => void runConfirm()}
      />
    </Stack>
  )
}
