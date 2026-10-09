import '@/marketing'
import {
  inflightHelper,
  toggleDisplay,
} from '@/features/form-helpers/hydrate-form'
import getMeta from '@/utils/meta'

// 2026-10-08 (legacy-infra stage 3 / owner: "repoint to /collab or drop"):
// the socket.io client was RETIRED — the page no longer loads
// /socket.io/socket.io.js (the template script tags were removed) and
// `window.io` no longer exists, so the old io.connect(...) probe would
// ReferenceError on exactly the page it guards (fresh installs). The honest
// replacement: probe THE live transport — a plain WebSocket handshake to
// /collab (ygo collab service behind nginx). A 101 upgrade means the
// websockets plane is up (which is what the chip asserts); a refused/
// failed handshake is a real error.

function setUpStatusIndicator(el: HTMLElement, fn: () => Promise<void>) {
  inflightHelper(el)

  const displaySuccess = el.querySelectorAll<HTMLElement>(
    '[data-ol-result="success"]'
  )
  const displayError = el.querySelectorAll<HTMLElement>(
    '[data-ol-result="error"]'
  )

  // The checks are very lightweight and do not appear to do anything
  //  from looking at the UI. Add an artificial delay of 1s to show that
  //  we are actually doing something. :)
  const artificialProgressDelay = 1000

  function run() {
    setTimeout(() => {
      fn()
        .then(() => {
          toggleDisplay(displayError, displaySuccess)
        })
        .catch(error => {
          const errorElement = el.querySelector('[data-ol-error]')
          if (errorElement) {
            errorElement.textContent = error.message
          }
          toggleDisplay(displaySuccess, displayError)
        })
        .finally(() => {
          el.dispatchEvent(new Event('idle'))
        })
    }, artificialProgressDelay)
  }

  el.querySelectorAll('button').forEach(retryBtn => {
    retryBtn.addEventListener('click', function (e) {
      e.preventDefault()
      el.dispatchEvent(new Event('pending'))
      run()
    })
  })

  run()
}

function setUpStatusIndicators() {
  const launchpadCheckElement = document.querySelector<HTMLElement>(
    '[data-ol-launchpad-check="websocket"]'
  )

  if (!launchpadCheckElement) {
    return
  }

  setUpStatusIndicator(launchpadCheckElement, () => {
    const timeout = 10 * 1000
    const wsUrl =
      (window.location.protocol === 'https:' ? 'wss://' : 'ws://') +
      window.location.host +
      '/collab?projectId=404404404404404404404404'
    return new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        try { ws.close() } catch { /* ignore */ }
        reject(new Error('timed out'))
      }, timeout)
      const ws = new WebSocket(wsUrl)
      ws.onopen = () => {
        // The websocket handshake succeeded: the collab plane is up.
        // (The collab service will reject/close the bogus projectId join —
        //  we do not care; the chip asserts transport, not membership.)
        window.clearTimeout(timer)
        try { ws.close() } catch { /* ignore */ }
        resolve()
      }
      ws.onerror = () => {
        window.clearTimeout(timer)
        try { ws.close() } catch { /* ignore */ }
        reject(new Error('websocket handshake failed'))
      }
      ws.onclose = () => {
        window.clearTimeout(timer)
        // Closed without ever opening = failure; closing after open is the
        // normal tear-down of the probe (resolve already fired).
      }
    })
  })
}

if (getMeta('ol-adminUserExists')) {
  setUpStatusIndicators()
}
