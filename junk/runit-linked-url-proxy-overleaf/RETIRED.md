RETIRED 2026-09-29 (owner-approved: "linked-url-proxy retirement — Yes"):
the standalone linked-url-proxy service (Go, 127.0.0.1:3066) is retired
together with its runit entry, cmd entrypoint and standalone e2e contract
battery (this folder).

Rationale (mapping evidence):
- Zero consumers in the live Go stack: no Go web / cmd / server-ce code
  calls it (only LINKED_URL_PROXY_HOST env left a dead setting, now removed
  with a note; its value was consumed solely by the LEGACY Node tree).
- Sole live caller was the legacy Node web (services/web
  UrlHelper.mjs / Features.mjs / settings.defaults.js) — that tree retires
  under the separately-approved services/web retirement phasing; UrlHelper
  is dead in the live flip (web :4000 + api :3000 = Go).
- The Go web implements its linked-URL surfaces in-process (no proxy hop);
  the proxy service was only ever the Node web's external-URL fetcher with
  the SSRF deny-list (OVERLEAF_LINKED_URL_BLOCKED_NETWORKS).
- The Go package go/services/linked-url-proxy REMAINS as the oracle
  reference + its contract/config-DB tests (blocked-networks wiring from
  the config-DB arc).

History preserved via git mv (S5 junking pattern).
