import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Anchor,
  Badge,
  Button,
  Card,
  Group,
  Image,
  Modal,
  NativeSelect,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
} from '@mantine/core'
import { notify } from '../../shared/notify'
import { getJSON, postJSON } from '@/infrastructure/fetch-json'
import Icon from '../../shared/icons'
import { EmptyState, PageError, PageLoading } from '../../shared/page-state'

type Template = {
  id: string
  name: string
  author?: string
  description?: string
  version?: string
  tags?: string[] | { _id: string; name: string }[]
}

function normalize(t: any): Template {
  const tags = Array.isArray(t.tags)
    ? t.tags.map(x => (typeof x === 'string' ? x : x?.name || ''))
    : []
  return {
    id: t.id || t._id || '',
    name: t.name || t.title || 'Template',
    author: t.author,
    description: t.description,
    version: t.version,
    tags: tags.filter(Boolean),
  }
}

export default function TemplatesSection({
  initialCategory,
}: {
  initialCategory?: string
} = {}) {
  const [templates, setTemplates] = useState<Template[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState('name')
  const [sortOrder, setSortOrder] = useState('asc')
  const [using, setUsing] = useState<Template | null>(null)
  const [projectName, setProjectName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createErr, setCreateErr] = useState<string | null>(null)
  const [categories, setCategories] = useState<Array<{ key: string; name: string }>>([])
  const [activeCategory, setActiveCategory] = useState(initialCategory || 'none')

  const load = useCallback(async (category = 'none') => {
    setError(null)
    try {
      // owner #3 (2026-09-07): 'none' means "ALL templates" (the legacy
      // gallery's All tab queries WITHOUT a category filter — ?category=none
      // used to select only the ungrouped ones).
      const catParam =
        category && category !== 'none' ? `&category=${encodeURIComponent(category)}` : ''
      const data = await getJSON(
        `/api/templates?by=${encodeURIComponent(sort)}&order=${sortOrder}${catParam}`
      )
      const list = Array.isArray(data?.templates) ? data.templates.map(normalize).filter(t => t.id) : []
      setTemplates(list)
    } catch (err: any) {
      setTemplates([])
      setError((err?.data?.message as string) || String(err?.message || err))
    }
  }, [sort, sortOrder])

  useEffect(() => {
    void load(activeCategory)
  }, [load, activeCategory])

  useEffect(() => {
    let alive = true
    getJSON('/api/template/categories')
      .then((data: any) => {
        if (!alive) return
        const list = Array.isArray(data) ? data : Array.isArray(data?.categories) ? data.categories : []
        setCategories(list.filter((c: any) => c && typeof c.key === 'string'))
      })
      .catch(() => {
        if (alive) setCategories([])
      })
    return () => {
      alive = false
    }
  }, [])

  const filtered = useMemo(() => {
    if (!templates) return []
    const q = query.trim().toLowerCase()
    if (!q) return templates
    return templates.filter(
      t =>
        (t.name || '').toLowerCase().includes(q) ||
        (t.author || '').toLowerCase().includes(q) ||
        (t.description || '').toLowerCase().includes(q)
    )
  }, [templates, query])

  const startFromTemplate = async () => {
    if (!using) return
    setCreating(true)
    setCreateErr(null)
    try {
      const data = await postJSON('/project/new', {
        body: { projectName: projectName.trim() || 'Untitled', template: using.id },
      })
      const id = data?.project_id
      setUsing(null)
      setProjectName('')
      if (id) {
        window.location.assign(`/project/${id}`)
        return
      }
      notify({ message: 'Project created from template.', color: 'teal' })
    } catch (err: any) {
      setCreateErr((err?.data?.message as string) || 'Could not create the project.')
    } finally {
      setCreating(false)
    }
  }

  if (!templates && !error) return <PageLoading label="Loading templates…" />

  return (
    <Stack gap="md">
      {/* Categories live in the hub rail (owner review #6: in-page category
          nav removed — it was a duplicate of the rail accordion). */}
      <Group justify="space-between" wrap="wrap" gap="sm">
        <TextInput
          aria-label="Search templates"
          withLeftSection
          leftSection={<Icon name="search" size={18} />}
          value={query}
          onChange={e => setQuery(e.currentTarget.value)}
          placeholder="Search templates by name, author, or description"
          style={{ maxWidth: 460, width: '100%' }}
        />
        <div style={{ width: 180 }}>
          {/* PG-TG-1 (parity): the legacy gallery filters by category (DS-nav
              categories / ?category=). The section had activeCategory state
              but no control at all — add the category selector beside sort. */}
          <NativeSelect
            value={activeCategory}
            onChange={e => setActiveCategory(e.currentTarget.value)}
            data={[
              { value: 'none', label: 'All templates' },
              ...categories.map(c => ({ value: c.key, label: c.name || c.key })),
            ]}
            size="sm"
            aria-label="Template category"
          />
          <NativeSelect
            value={sort === 'name' && sortOrder === 'asc' ? 'name' : sort}
            onChange={e => {
              const v = e.currentTarget.value
              if (v === 'name') {
                setSort('name')
                setSortOrder('asc')
              } else {
                setSort('lastUpdated')
                setSortOrder('desc')
              }
            }}
            data={[
              { value: 'name', label: 'Sort: name' },
              { value: 'lastUpdated', label: 'Sort: recently updated' },
            ]}
            size="sm"
            aria-label="Template sort"
          />
        </div>
      </Group>

      {error ? (
        <PageError label="Couldn’t load templates" detail={error} onRetry={() => void load()} />
      ) : null}

      {!templates || templates.length === 0 ? (
        <EmptyState
          icon="extension"
          title="No templates available"
          hint="This instance has no shared templates yet. Ask an administrator about enabling the template gallery."
        />
      ) : filtered.length === 0 ? (
        <EmptyState icon="search_off" title="No templates match" hint={`Nothing matched “${query}”.`} />
      ) : (
        <SimpleGrid cols={{ base: 1, sm: 2, xl: 3 }} spacing="md">
          {filtered.map(t => (
            <Card key={t.id} withBorder paddings="md" radius="lg" mah="100%" miw={280}>
              {/* 8a (2026-09-16, owner): the legacy gallery showed a preview
                  thumbnail on every card (legacy: /template/:id/preview?style=thumbnail).
                  Hide the image if the template has no compiled preview yet. */}
              <Image
                src={`/template/${t.id}/preview?version=${encodeURIComponent(String(t.version || 'latest'))}&style=thumbnail`}
                alt={t.name}
                h={150}
                fit="cover"
                radius="md"
                mbd="md"
                onError={e => { e.currentTarget.style.display = 'none' }}
              />
              <Group justify="space-between" wrap="nowrap" gap="xs">
                <Text fw={600} size="md" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>
                  {t.name}
                </Text>
                {t.version ? (
                  <Badge size="xs" variant="subtle" radius="sm">
                    v{t.version}
                  </Badge>
                ) : null}
              </Group>
              {t.author ? (
                <Text size="sm" c="dimmed" mt={4}>
                  {t.author}
                </Text>
              ) : null}
              {t.description ? (
                <Text size="sm" c="dimmed" mt={6} style={{ display: '-webkit-box', WebkitLineClamp: 3, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>
                  {t.description}
                </Text>
              ) : null}
              {t.tags?.slice(0, 3).length ? (
                <Group gap={4} mt={8}>
                  {t.tags.slice(0, 3).map(tag => (
                    <Badge key={tag} size="xs" variant="light" color="blue" radius="sm">
                      {tag}
                    </Badge>
                  ))}
                </Group>
              ) : null}
              <Card.Section mt="md">
                <Group gap="xs">
                  <Button
                    size="sm"
                    color="ollitex"
                    grow
                    leftSection={<Icon name="rocket_launch" size={16} />}
                    onClick={() => {
                      setUsing(t)
                      setProjectName('')
                      setCreateErr(null)
                    }}
                  >
                    Use template
                  </Button>
                </Group>
                {/* 8b (2026-09-16, owner): the legacy detail page offered View PDF +
                    Download bundle; restore both for all user types (the bundle
                    endpoint is login-gated, no longer management-only). */}
                <Group gap="xs" mt="xs">
                  <Anchor
                    href={`/template/${t.id}/preview?version=${encodeURIComponent(String(t.version || 'latest'))}`}
                    target="_blank"
                    rel="noreferrer"
                    size="sm"
                  >
                    <Icon name="visibility" size={16} /> View PDF
                  </Anchor>
                  <Anchor
                    href={`/template/${t.id}/bundle`}
                    target="_blank"
                    rel="noreferrer"
                    size="sm"
                  >
                    <Icon name="download" size={16} /> Download bundle
                  </Anchor>
                </Group>
              </Card.Section>
            </Card>
          ))}
        </SimpleGrid>
      )}

      <Modal
        opened={!!using}
        onClose={() => setUsing(null)}
        size="sm"
        title={
          <Text fw={700}>Use “{using?.name}” as a template</Text>
        }
      >
        <Stack gap="md">
          <Text size="sm" c="dimmed">
            A new project will be created with everything included in this template. You can rename it later from the project menu.
          </Text>
          <div>
            <Text size="sm" fw={600} mb={6}>
              Project name
            </Text>
            <TextInput
              aria-label="Project name"
              value={projectName}
              onChange={e => setProjectName(e.currentTarget.value)}
              placeholder="Untitled"
            />
          </div>
          {createErr ? <Text size="sm" c="red">{createErr}</Text> : null}
          <Group justify="flex-end" gap="xs" mt="sm">
            <Button variant="default" onClick={() => setUsing(null)} disabled={creating}>
              Cancel
            </Button>
            <Button
              color="ollitex"
              loading={creating}
              onClick={() => {
                void startFromTemplate()
              }}
            >
              Create project
            </Button>
          </Group>
        </Stack>
      </Modal>
      </Stack>
  )
}
