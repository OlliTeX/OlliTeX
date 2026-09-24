#!/usr/bin/env python3
"""Generate go/services/web/views/pages_data_p620.go from the P6.20 raw
captures (tools/capture-p620-raw/, captured live from the Node oracle on
the e2e stack — the exact bytes nginx serves).

Capture context (2026-09-21, ol-e2e stack):
  lp-admin-raw.html — admin session, admin-exists state (200, 16982 bytes,
                      ETag W/"4256-…"); admin user e2e-admin@e2e.test.
  lp-fresh-raw.html — anonymous, fresh state (200, 14596 bytes,
                      ETag W/"3904-…"); no-admin + LDAP-branch forms.

Slot plan (see views/pages.go):
  \x01NONCE\x02  — the captured CSP nonce (preload link, inline script,
                 every trailing script tag)
  \x01CSRF\x02   — the captured csrf token (ol-csrfToken meta + every
                 _csrf hidden input)
  \x01OLUSERS\x02— the captured fixture email (ol-usersEmail meta +
                 admin navbar account pill; fresh renders empty)
  \x01LPUID\x02  — ol-user_id: `` or ` content="HEX"` (REGUID semantics;
                 fresh renders absent)
  \x01CANMGTPL\x02 — ExposedSettings canManageTemplatesMenu value
                 (finalize renders "true"/"false" from PageData)
  \x01LPADM\x02  — ol-adminUserExists bare-content boolean
                 (finalize renders " content"/"" from PageData)

Verification: reverse-rendering each skeleton with its own capture values
must be byte-identical to the raw capture (checked here before emitting).
"""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RAW = os.path.join(ROOT, "tools", "capture-p620-raw")
OUT = os.path.join(ROOT, "go", "services", "web", "views", "pages_data_p620.go")

SLOT = {
    "nonce": "\x01NONCE\x02",
    "csrf": "\x01CSRF\x02",
    "email": "\x01OLUSERS\x02",
    "lpuuid": "\x01LPUID\x02",
    "canmgt": "\x01CANMGTPL\x02",
    "lpadmin": "\x01LPADM\x02",
}


def load(name):
    with open(os.path.join(RAW, name), encoding="utf-8") as f:
        return f.read()


def go_string(s):
    if "`" not in s and not s.startswith("\n"):
        return "`" + s + "`"
    # fall back to JSON-style Go string
    import json
    return json.dumps(s)


def ctx_extract(doc):
    ctx = {}
    m = re.search(r'name="ol-csrfToken" content="([^"]+)"', doc)
    if not m:
        raise SystemExit("ctx: no csrf meta")
    ctx["csrf"] = m.group(1)
    n = re.findall(r'nonce="([^"]+)"', doc)
    if not n or len(set(n)) != 1:
        raise SystemExit(f"ctx: nonce set unexpected: {n}")
    ctx["nonce"] = n[0]
    m = re.search(r'<meta name="ol-usersEmail" content="([^"]*)"', doc)
    ctx["email"] = m.group(1) if m else ""
    m = re.search(r'<meta name="ol-user_id" content="([^"]*)"', doc)
    ctx["uid"] = m.group(1) if m else ""
    return ctx


def slotify(doc, ctx):
    out = doc
    # 1. ol-user_id content → LPUID (the whole ` content="…"` or nothing)
    new, nsub = re.subn(
        r'(<meta name="ol-user_id")( content="[^"]*")?>',
        r"\1" + SLOT["lpuuid"] + ">",
        out,
        count=1,
    )
    if nsub != 1:
        raise SystemExit("slotify: ol-user_id not found")
    out = new
    # 2. ol-usersEmail content → OLUSERS (value may be empty on fresh)
    out = re.sub(
        r'(<meta name="ol-usersEmail" content=")[^"]*(")',
        r"\1" + SLOT["email"] + r"\2",
        out,
        count=1,
    )
    if SLOT["email"] not in out:
        raise SystemExit("slotify: ol-usersEmail not found")
    # 2b. remaining raw email occurrences (navbar account pill, …)
    if ctx["email"]:
        out = out.replace(ctx["email"], SLOT["email"])
    # 3. csrf token everywhere (meta + _csrf inputs)
    if ctx["csrf"]:
        out = out.replace(ctx["csrf"], SLOT["csrf"])
    else:
        raise SystemExit("slotify: empty csrf ctx")
    # 4. nonce everywhere
    out = out.replace(ctx["nonce"], SLOT["nonce"])
    # 5. canManageTemplatesMenu value inside ol-ExposedSettings
    pat = r'(&quot;canManageTemplatesMenu&quot;:)(true|false)'
    new, nsub = re.subn(
        pat, r"\1" + SLOT["canmgt"], out, count=1)
    if nsub != 1:
        raise SystemExit("slotify: canManageTemplatesMenu not found")
    out = new
    # 6. ol-adminUserExists bare-content boolean → LPADM
    if '<meta name="ol-adminUserExists" data-type="boolean" content>' in out:
        out = out.replace(
            '<meta name="ol-adminUserExists" data-type="boolean" content>',
            '<meta name="ol-adminUserExists" data-type="boolean"'
            + SLOT["lpadmin"] + ">",
            1,
        )
    elif '<meta name="ol-adminUserExists" data-type="boolean">' in out:
        out = out.replace(
            '<meta name="ol-adminUserExists" data-type="boolean">',
            '<meta name="ol-adminUserExists" data-type="boolean"'
            + SLOT["lpadmin"] + ">",
            1,
        )
    else:
        raise SystemExit("slotify: ol-adminUserExists not found")
    return out


def render(skel, ctx, email, uid, canmgt, lpadmin):
    out = skel.replace(SLOT["csrf"], ctx["csrf"])
    out = out.replace(SLOT["nonce"], ctx["nonce"])
    out = out.replace(SLOT["email"], email)
    out = out.replace(SLOT["lpuuid"],
                      (f' content="{uid}"' if uid else ""))
    out = out.replace(SLOT["canmgt"], "true" if canmgt else "false")
    out = out.replace(SLOT["lpadmin"], " content" if lpadmin else "")
    return out


def main():
    admin_raw = load("lp-admin-raw.html")
    fresh_raw = load("lp-fresh-raw.html")

    ca = ctx_extract(admin_raw)
    cf = ctx_extract(fresh_raw)

    admin = slotify(admin_raw, ca)
    fresh = slotify(fresh_raw, cf)

    def check(name, skel, raw, ctx, email, uid, canmgt, lpadmin):
        rendered = render(skel, ctx, email, uid, canmgt, lpadmin)
        if rendered != raw:
            for i, (a, b) in enumerate(zip(rendered, raw)):
                if a != b:
                    print(f"{name}: first diff at {i}")
                    print("render:", rendered[max(0, i - 80):i + 120])
                    print("raw   :", raw[max(0, i - 80):i + 120])
                    break
            else:
                print(name, "len diff", len(rendered), len(raw))
            raise SystemExit(f"{name}: reverse-render mismatch")
        # no residual captured secrets/values
        for bad in (ctx["csrf"], ctx["nonce"]):
            if bad in skel:
                raise SystemExit(f"{name}: residual {bad[:12]}… in skeleton")
        if uid and uid in skel:
            raise SystemExit(f"{name}: residual uid in skeleton")
        if email and email in skel:
            raise SystemExit(f"{name}: residual email in skeleton")
        print(f"{name}: skeleton verified against raw capture "
              f"({len(raw)} bytes)")

    # admin: rendered with its own capture values + flag true
    check("admin", admin, admin_raw, ca, ca["email"], ca["uid"],
          True, True)
    # fresh: rendered with its own capture values + flags false
    check("fresh", fresh, fresh_raw, cf, cf["email"], cf["uid"],
          False, False)

    with open(OUT, "w", encoding="utf-8") as f:
        f.write("// Code generated by tools/webviews-gen-p620.py from the Node\n")
        f.write("// oracle captures (tools/capture-p620-raw/). Do not edit.\n")
        f.write("//\n")
        f.write("// P6.20 launchpad page bakes (services/web/modules/launchpad).\n")
        f.write("// admin  — admin session + admin-exists state (status checks,\n")
        f.write("//          send-test-email form, admin/minimal-nav as captured)\n")
        f.write("// fresh  — anonymous + no-admin state (first-admin forms; the\n")
        f.write("//          fork's SSO module always sets Settings.ldap, so the\n")
        f.write("//          live oracle renders the LDAP-branch layout)\n")
        f.write("package views\n\n")
        f.write("const launchpadAdminHTML = " + go_string(admin) + "\n\n")
        f.write("const launchpadFreshHTML = " + go_string(fresh) + "\n")

    print("wrote", OUT, len(admin), "+", len(fresh), "bytes")


if __name__ == "__main__":
    main()
