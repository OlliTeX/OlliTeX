import getMeta from '@/utils/meta'

export type CookieConsentValue = 'all' | 'essential'

function loadGA() {
  if (window.olLoadGA) {
    window.olLoadGA()
  }
}

// setConsent — record a consent choice.
//
// 1) LOCAL write first: immediate UX persistence and the availability
//    fallback (server unreachable / no CSRF meta on the page).
//    Protocol-aware: `Secure` is only honored on HTTPS — sending it over
//    plain http makes the browser DISCARD the cookie, which would leave a
//    self-hosted http install unable to remember a choice at all. The
//    `domain` attribute is only sent when configured (a bare "domain="
//    silently invalidates the whole Set-Cookie string).
//
// 2) SERVER record (remember.md P7-post item 2): POST /cookie-consent
//    carries the CSRF token (core applies the global CSRF gate to the
//    POST) and the Go endpoint validates the value against its allowlist
//    and sets the canonical cookie (Secure + SameSite=Lax, Path=/).
//    Non-fatal on failure — step 1 already persisted the choice.
//
// Value contract (compat): 'all' → oa=1 (analytics+marketing),
// anything else ('essential' | null) → oa=0 (essential only).
export function setConsent(value: CookieConsentValue | null) {
  const all = value === 'all'
  const exposed = getMeta('ol-ExposedSettings')
  const cookieDomain = exposed ? exposed.cookieDomain : undefined
  const oneYearInSeconds = 60 * 60 * 24 * 365
  const cookieAttributes =
    '; path=/' +
    (cookieDomain ? '; domain=' + cookieDomain : '') +
    '; max-age=' +
    oneYearInSeconds +
    '; SameSite=Lax' +
    (window.location.protocol === 'https:' ? '; Secure' : '')
  document.cookie = (all ? 'oa=1' : 'oa=0') + cookieAttributes

  const csrfToken = getMeta('ol-csrfToken')
  if (csrfToken) {
    window
      .fetch('/cookie-consent', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          'Content-Type': 'application/json',
          'x-csrf-token': String(csrfToken),
        },
        body: JSON.stringify({ consent: value }),
      })
      .catch(() => {
        // availability-first: a failed server record must not break the
        // consent capture (the local cookie above already persisted it)
      })
  }

  // downstream gates (tracking-loader et al.)
  if (all) {
    loadGA()
    window.dispatchEvent(new CustomEvent('cookie-consent', { detail: true }))
  } else {
    window.dispatchEvent(new CustomEvent('cookie-consent', { detail: false }))
  }
}

export function cookieBannerRequired() {
  const exposedSettings = getMeta('ol-ExposedSettings')
  return Boolean(
    exposedSettings.gaToken ||
    exposedSettings.gaTokenV4 ||
    exposedSettings.propensityId ||
    exposedSettings.hotjarId ||
    (exposedSettings.mixpanelLabsToken && exposedSettings.labsEnabled)
  )
}

export function hasMadeCookieChoice() {
  return document.cookie.split('; ').some(c => c.startsWith('oa='))
}
