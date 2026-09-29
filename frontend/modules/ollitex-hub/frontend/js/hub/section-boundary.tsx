import React, { Component, ReactNode } from 'react'
import { Stack, Text, Title, Button, Icon as _i } from '@mantine/core'
import Icon from '../shared/icons'

interface Props {
  label: string
  children: ReactNode
}
interface State {
  error: Error | null
}

/** Per-section error boundary: one broken section never takes the hub down. */
export default class SectionBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: unknown) {
    try {
      // eslint-disable-next-line no-console
      console.error('[ollitex-hub] section failed:', this.props.label, error, info)
    } catch {
      // console may be unavailable in odd test environments
    }
  }

  render() {
    if (this.state.error) {
      const message = this.state.error.message || String(this.state.error)
      return (
        <Stack align="center" gap="xs" py="xl" ta="center" style={{ maxWidth: 480, margin: '0 auto' }}>
          <Icon name="error" size={36} style={{ color: 'var(--mantine-color-red-6)' }} />
          <Title order={4} style={{ margin: 0 }}>
            “{this.props.label}” failed to load
          </Title>
          <Text size="sm" c="dimmed" inline={false} style={{ margin: 0 }}>
            The rest of the hub is unaffected.{' '}
            <Text component="code" size="xs" inline style={{ background: 'var(--mantine-color-default-hover)', padding: '1px 6px', borderRadius: 6 }}>
              {message}
            </Text>
          </Text>
          <Button
            size="sm"
            variant="default"
            leftSection={<Icon name="refresh" size={16} />}
            onClick={() => this.setState({ error: null })}
          >
            Try again
          </Button>
        </Stack>
      )
    }
    return this.props.children
  }
}
