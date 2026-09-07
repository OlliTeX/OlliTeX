import React, { Component, ReactNode, useCallback, useEffect, useState } from 'react'
import { Text } from '@mantine/core'
import HubLayout, { type HubNavGroup } from '../shared/hub-layout'
import useHashSection from '../shared/use-hash-section'
import Icon from '../shared/icons'
import ThemeToggle from '../shared/theme-toggle'
import AdminInstanceSection from '../sections/admin/admin-instance-section'
import AdminUsersSection from '../sections/admin/admin-users-section'
import AdminProjectsSection from '../sections/admin/admin-projects-section'
import AdminLlmSection from '../sections/admin/admin-llm-section'
import AdminTemplatesSection from '../sections/admin/admin-templates-section'
import AdminSiteSection from '../sections/admin/admin-site-section'

const INST = 'admin-instance'
const USERS = 'admin-users'
const PROJECTS = 'admin-projects'
const LLM = 'admin-llm'
const TPL = 'admin-templates'
const SITE = 'admin-site'

const NAV_GROUPS: HubNavGroup[] = [
  {
    label: 'Instance',
    items: [
      { id: INST, label: 'Overview & activity', icon: 'insights', badge: 'AI' },
    ],
  },
  {
    label: 'Administration',
    items: [
      { id: USERS, label: 'Users', icon: 'groups' },
      { id: PROJECTS, label: 'Projects', icon: 'folder' },
      { id: TPL, label: 'Templates', icon: 'tag' },
    ],
  },
  {
    label: 'AI',
    items: [{ id: LLM, label: 'LLM instance', icon: 'psychology' }],
  },
  {
    label: 'Configuration',
    items: [{ id: SITE, label: 'Site settings', icon: 'tune' }],
  },
]

function SectionBoundary({
  name,
  onRetry,
  children,
}: {
  name: string
  onRetry: (e: Error, info: any) => void
  children: ReactNode
}) {
  if (process.env.NODE_ENV !== 'production') {
    try {
      return <>{children}</>
    } catch (err) {
      return <BoundaryErrorUI name={name} err={err} onRetry={onRetry} />
    }
  }
  return <Boundary name={name} onRetry={onRetry}>{children}</Boundary>
}

function BoundaryErrorUI({ name, err, onRetry }: { name: string; err: Error; onRetry: (e: Error, info: any) => void }) {
  return (
    <div style={{ padding: 28 }}>
      <Text ff="monospace" ta="uppercase" size="xs" c="dimmed" mb={8}>
        section fault · {name}
      </Text>
      <Text size="sm" c="red">
        {err?.message || 'This section could not render.'}
      </Text>
      <button
        type="button"
        style={{ marginTop: 12, border: '1px solid var(--mantine-color-default-border)', borderRadius: 9, padding: '8px 14px', cursor: 'pointer', background: 'var(--mantine-color-body)', fontSize: 13 }}
        onClick={() => onRetry(err, { phase: 'render' })}
      >
        Retry section
      </button>
    </div>
  )
}

class Boundary extends Component<
  { name: string; onRetry: (e: Error, info: any) => void; children: ReactNode },
  { err: Error | null }
> {
  constructor(props: any) {
    super(props)
    this.state = { err: null }
  }
  static getDerivedStateFromError(err: Error) {
    return { err }
  }
  componentDidCatch(err: Error, info: any) {
    this.props.onRetry(err, { section: this.props.name, ...info })
  }
  render() {
    if (this.state.err)
      return <BoundaryErrorUI name={this.props.name} err={this.state.err} onRetry={this.props.onRetry} />
    return this.props.children
  }
}

export default function AdminHubRoot() {
  const { section, select } = useHashSection(INST)
  const [nonce, setNonce] = useState(0)

  const handleSectionChange = useCallback((next: string) => {
    select(next)
  }, [select])
  const reload = useCallback(() => setNonce(n => n + 1), [])
  const onSectionError = useCallback((err: Error, info: any) => {
    console.error('[admin-hub]', err, info)
  }, [])

  let content: ReactNode
  switch (section) {
    case USERS:
      content = (
        <SectionBoundary name={USERS} onRetry={onSectionError}>
          <AdminUsersSection key={nonce} />
        </SectionBoundary>
      )
      break
    case PROJECTS:
      content = (
        <SectionBoundary name={PROJECTS} onRetry={onSectionError}>
          <AdminProjectsSection key={nonce} />
        </SectionBoundary>
      )
      break
    case LLM:
      content = (
        <SectionBoundary name={LLM} onRetry={onSectionError}>
          <AdminLlmSection key={nonce} />
        </SectionBoundary>
      )
      break
    case TPL:
      content = (
        <SectionBoundary name={TPL} onRetry={onSectionError}>
          <AdminTemplatesSection key={nonce} />
        </SectionBoundary>
      )
      break
    case SITE:
      content = (
        <SectionBoundary name={SITE} onRetry={onSectionError}>
          <AdminSiteSection key={nonce} />
        </SectionBoundary>
      )
      break
    case INST:
    default:
      content = (
        <SectionBoundary name={INST} onRetry={onSectionError}>
          <AdminInstanceSection key={nonce} />
        </SectionBoundary>
      )
      break
  }

  return (
    <HubLayout
      brand="LibreLeaf"
      tagline="Instance administration"
      nav={NAV_GROUPS}
      active={section}
      onSelect={handleSectionChange}
      title="Administration"
      subtitle="Users, projects, templates, AI, and site configuration."
      headerActions={
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <ThemeToggle />
          <a
            href="/admin"
            target="_blank"
            rel="noreferrer"
            style={{
              fontSize: 12.5,
              color: 'var(--color-ink-2)',
              textDecoration: 'none',
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
            }}
          >
            <Icon name="open_in_new" size={16} /> Classic admin
          </a>
        </div>
      }
    >
      {content}
    </HubLayout>
  )
}
