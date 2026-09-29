// PSH — generic DS-nav settings shell (2026-09-04 R5, owner round-5).
//
// Shared chrome for the settings-family pages that adopt the EXACT
// /admin/site golden DOM (user-ds-nav-page / user-list-wrapper /
// user-list-sidebar-* / ds-nav-sidebar-lower) without owning a React
// page tree of their own — currently:
//
//   * /user/llm-settings               (blue user family, scroll nav)
//   * /admin/llm/settings              (red admin family, tab nav)
//   * /user/notification-preferences   (blue user family, scroll nav)
//
// Same behavior as my-settings-shell.js, generalized: the page's pug sets
// window.__dsSettingsShell = { navbarRootId, accountRootId, cookieRootId? ,
// footerRootId? } BEFORE this entry loads.
//
// Two nav models (both golden-faithful):
//   * scroll model — `li.ds-nav-page-nav-item > button[data-target=<sel>]`:
//     smooth-scroll + IntersectionObserver active highlight;
//   * tab model (the /admin/site model) — `button[data-sec=<id>]`: one
//     section visible at a time; the shell dispatches the 'ds-settings-nav'
//     CustomEvent and the page's React app switches its active section.
// Nav items whose target section is absent in this build are hidden after
// the content has rendered.
import { renderDsNavChrome } from '../components/ds-nav-chrome'

function cfg() {
  try {
    return window.__dsSettingsShell || {}
  } catch (e) {
    return {}
  }
}

document.addEventListener('DOMContentLoaded', () => {
  const c = cfg()
  renderDsNavChrome({
    navbarRootId: c.navbarRootId,
    accountRootId: c.accountRootId,
    footerRootId: c.footerRootId,
    cookieRootId: c.cookieRootId,
  })

  function navBtns() {
    return Array.prototype.slice.call(
      document.querySelectorAll('li.ds-nav-page-nav-item > button')
    )
  }

  function targetOf(btn) {
    const sel = btn.getAttribute('data-target') || ''
    if (!sel) return null
    try {
      return document.querySelector(sel)
    } catch (e) {
      return null
    }
  }

  function secOf(btn) {
    return btn.getAttribute('data-sec') || ''
  }

  function secEl(btn) {
    const sec = secOf(btn)
    if (!sec) return null
    try {
      return document.querySelector("[data-sec='" + sec + "']")
    } catch (e) {
      return null
    }
  }

  function targetOfAny(btn) {
    return secEl(btn) || targetOf(btn)
  }

  function setActive(btn) {
    // Golden styling lives on `.user-list-filters > li.active > button`.
    navBtns().forEach(b => {
      const li = b.closest('li')
      if (li) li.classList.toggle('active', li === btn.closest('li'))
    })
  }

  navBtns().forEach(btn => {
    btn.addEventListener('click', () => {
      if (secOf(btn)) {
        // Tab model: hand the section switch to the page's React app.
        window.dispatchEvent(
          new CustomEvent('ds-settings-nav', { detail: { sec: secOf(btn) } })
        )
      } else {
        const el = targetOf(btn)
        if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
      }
      setActive(btn)
    })
  })

  // Hide nav entries whose section is not rendered in this build, and once
  // everything has resolved, start the active-section highlight (scroll
  // model only).
  function start() {
    const btns = navBtns()
    const scrollTargets = []
    let anyTab = false
    btns.forEach(btn => {
      const el = targetOfAny(btn)
      if (!el) {
        const li = btn.closest('li')
        if (li) li.style.display = 'none'
        return
      }
      if (secOf(btn)) {
        anyTab = true
      } else {
        scrollTargets.push({ btn: btn, el: el })
      }
    })
    // Tab-model page: nothing to observe — clicks drive the highlight.
    if (anyTab && scrollTargets.length === 0) return true
    if (!scrollTargets.length) return false
    if (!('IntersectionObserver' in window)) return true
    const map = new Map(scrollTargets.map(x => [x.el, x.btn]))
    const io = new IntersectionObserver(
      entries => {
        const vis = entries
          .filter(e => e.isIntersecting)
          .sort((a, b) => ((a.target.compareDocumentPosition(b.target) & 2) ? 1 : -1))
        if (vis.length === 0) return
        const btn = map.get(vis[0].target)
        if (btn) setActive(btn)
      },
      { rootMargin: '0px 0px -55% 0px', threshold: 0 }
    )
    scrollTargets.forEach(x => io.observe(x.el))
    return true
  }

  let tries = 0
  const timer = setInterval(() => {
    tries += 1
    if (start() || tries > 40) clearInterval(timer)
  }, 250)
})
