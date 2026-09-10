import { useEffect, useState } from 'react'
import SystemMessage from './system-message'
import TranslationMessage from './translation-message'
import useAsync from '../hooks/use-async'
import { getJSON } from '@/infrastructure/fetch-json'
import getMeta from '../../utils/meta'
import { SystemMessage as TSystemMessage } from '../../../../types/system-message'
import {
  detectMessageSurface,
  messageVisibleForSurface,
} from './system-message-surface'
import type { MessageSurface } from './system-message-surface'
import { debugConsole } from '@/utils/debugging'

const MESSAGE_POLL_INTERVAL = 15 * 60 * 1000

function SystemMessages() {
  const { data: messages, runAsync } = useAsync<TSystemMessage[]>()
  const suggestedLanguage = getMeta('ol-suggestedLanguage')

  // #17b (owner 2026-09-13): surface-scoped messages. SSR-safe: on the
  // server (no window) we default to the 'app' surface, where scoped
  // messages are conservative-hidden; the client re-renders with the
  // real surface before first paint settles.
  const [surface] = useState<MessageSurface>(() =>
    typeof window === 'undefined'
      ? 'app'
      : detectMessageSurface(window.location.pathname)
  )

  useEffect(() => {
    const pollMessages = () => {
      // Ignore polling if tab is hidden or browser is offline
      if (document.hidden || !navigator.onLine) {
        return
      }

      runAsync(getJSON('/system/messages')).catch(debugConsole.error)
    }
    pollMessages()

    const interval = setInterval(pollMessages, MESSAGE_POLL_INTERVAL)

    return () => {
      clearInterval(interval)
    }
  }, [runAsync])

  const visibleMessages =
    messages?.filter(
      m => m._id === 'protected' || messageVisibleForSurface(m, surface)
    ) ?? []

  if (!visibleMessages.length && !suggestedLanguage) {
    return null
  }

  return (
    <ul className="system-messages">
      {visibleMessages.map((message, idx) => (
        <SystemMessage key={idx} id={message._id}>
          {message.content}
        </SystemMessage>
      ))}
      {suggestedLanguage ? <TranslationMessage /> : null}
    </ul>
  )
}

export default SystemMessages
