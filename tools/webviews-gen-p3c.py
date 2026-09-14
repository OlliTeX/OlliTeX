#!/usr/bin/env python3
"""Generate go/services/web/views/pages_data_p3c.go from the raw P3.3
captures (tools/webviews-capture-p3c.py output).

Slot plan (see views/pages.go):
  \x01NONCE\x02   — the captured nonce (every occurrence)
  \x01CSRF\x02    — the captured csrf token (every occurrence)
  \x01OLUSERS\x02 — fixture email (ol-usersEmail, ol-user handled separately,
                  account-dropdown pill, any other raw occurrence)
  \x01OLUID\x02   — fixture user id (ol-user_id + any other raw occurrence)
  \x01USR33\x02   — ol-user meta JSON content (settings page)
  \x01HBS33\x02   — ,&quot;hasSamlBeta&quot;:&quot;...&quot; insertion
                   (after ieeeBrandId in ol-ExposedSettings)
  \x01HASPW33\x02 / \x01AI33\x02 — bare-content boolean metas
  \x01SAMLM33/SSOM33/SYNCO33/SYNCX33/REFE33 — pop-flag meta content attrs
  \x01CURRROW33\x02 / \x01OTHERROWS33\x02 — sessions page rows

Verification (tools/webviews-verify-p3c.go or a Go test) re-renders with the
recorded capture values and must be byte-identical to the raw capture.
"""
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RAW = os.path.join(ROOT, "tools", "capture-p3c-raw")
OUT = os.path.join(ROOT, "go", "services", "web", "views", "pages_data_p3c.go")

EMAIL = "e2e-user@e2e.test"

SLOT = {
    "nonce": "\x01NONCE\x02",
    "csrf": "\x01CSRF\x02",
    "email": "\x01OLUSERS\x02",
    "uid": "\x01OLUID\x02",
    "user": "\x01USR33\x02",
    "hbs": "\x01HBS33\x02",
    "haspw": "\x01HASPW33\x02",
    "showai": "\x01AI33\x02",
    "samlmeta": "\x01SAMLM33\x02",
    "ssomsg": "\x01SSOM33\x02",
    "syncock": "\x01SYNCO33\x02",
    "syncerr": "\x01SYNCX33\x02",
    "referr": "\x01REFE33\x02",
    "currrow": "\x01CURRROW33\x02",
    "otherrows": "\x01OTHERROWS33\x02",
}


def load(tag):
    with open(os.path.join(RAW, tag + ".html"), encoding="utf-8") as f:
        return f.read()


def headers(tag):
    with open(os.path.join(RAW, tag + "_headers.json"), encoding="utf-8") as f:
        return json.load(f)


def go_string(s):
    # raw backtick Go string if safe, else quoted escape
    if "`" not in s and not s.startswith("\n"):
        return "`" + s + "`"
    return json.dumps(s)


def slotify_settings(html, ctx):
    out = html
    # 1. ol-user meta (the whole content attribute) -> USR33
    out = re.sub(
        r'(<meta name="ol-user" data-type="json" content=")([^"]*)">',
        r"\1" + SLOT["user"] + r'">',
        out,
        count=1,
    )
    # 2. ol-user_id content -> OLUID
    out = re.sub(
        r'(<meta name="ol-user_id" content=")[^"]*(")',
        r"\1" + SLOT["uid"] + r"\2",
        out,
        count=1,
    )
    # 3. ol-usersEmail content -> OLUSERS
    out = re.sub(
        r'(<meta name="ol-usersEmail" content=")[^"]*(")',
        r"\1" + SLOT["email"] + r"\2",
        out,
        count=1,
    )
    # 4. account dropdown pill + any remaining raw emails
    out = out.replace(EMAIL, SLOT["email"])
    # 5. remaining raw uid occurrences (ol-user_id already slotted)
    out = out.replace(ctx["uid"], SLOT["uid"])
    # 6. csrf token everywhere (meta + logout form hidden input)
    out = out.replace(ctx["csrf"], SLOT["csrf"])
    # 7. nonce everywhere (script attrs + CSP inside html comments)
    out = out.replace(ctx["nonce"], SLOT["nonce"])
    return out


def slotify_settings_dynamic(out):
    # hasSamlBeta: absent in the clean capture -> insert the marker after
    # the ieeeBrandId entry inside ol-ExposedSettings (once).
    anchor = 'ieeeBrandId&quot;:15,'
    if anchor in out and SLOT["hbs"] not in out:
        out = out.replace(anchor, anchor + SLOT["hbs"], 1)
    # boolean metas (bare `content` when true in the capture)
    for name, slot in (
        ("ol-hasPassword", SLOT["haspw"]),
        ("ol-showAiFeatures", SLOT["showai"]),
    ):
        pat = '<meta name="%s" data-type="boolean" content>' % name
        if pat in out:
            out = out.replace(pat, '<meta name="%s" data-type="boolean"%s>' % (name, slot), 1)
        else:
            pat = '<meta name="%s" data-type="boolean"' % name
            if pat in out:
                out = out.replace(pat, '<meta name="%s" data-type="boolean"%s' % (name, slot), 1)
    # pop-flag metas: clean capture has them WITHOUT content
    for name, slot in (
        ("ol-samlBeta", SLOT["samlmeta"]),
        ("ol-ssoErrorMessage", SLOT["ssomsg"]),
        ("ol-projectSyncSuccessMessage", SLOT["syncock"]),
        ("ol-projectSyncErrorMessage", SLOT["syncerr"]),
        ("ol-referenceLinkingErrorMessage", SLOT["referr"]),
    ):
        pat = '<meta name="%s" content="%s">' % (name, ctx_value(name))
        if pat in out:
            out = out.replace(
                pat, '<meta name="%s"%s>' % (name, slot), 1)
            continue
        pat2 = '<meta name="%s" content>' % name
        if pat2 in out:
            out = out.replace(pat2, '<meta name="%s"%s>' % (name, slot), 1)
            continue
        pat3 = '<meta name="%s">' % name
        if pat3 in out:
            out = out.replace(pat3, '<meta name="%s"%s>' % (name, slot), 1)
    return out


CTX_CACHE = {}


def ctx_value(name):
    return CTX_CACHE.get(name, "")


def slotify_sessions(html, ctx):
    out = html
    # csrf
    out = out.replace(ctx["csrf"], SLOT["csrf"])
    out = out.replace(ctx["nonce"], SLOT["nonce"])
    # ol-user_id
    out = re.sub(
        r'(<meta name="ol-user_id" content=")[^"]*(")',
        r"\1" + SLOT["uid"] + r"\2",
        out,
        count=1,
    )
    out = re.sub(
        r'(<meta name="ol-usersEmail" content=")[^"]*(")',
        r"\1" + SLOT["email"] + r"\2",
        out,
        count=1,
    )
    out = out.replace(EMAIL, SLOT["email"])
    out = out.replace(ctx["uid"], SLOT["uid"])
    # hasSamlBeta (present in the capture because session A had it seeded)
    m2 = re.search(r',&quot;hasSamlBeta&quot;:&quot;[^&]*&quot;', out)
    if m2 and SLOT["hbs"] not in out:
        out = out[: m2.start()] + SLOT["hbs"] + out[m2.end():]
    elif SLOT["hbs"] not in out:
        anchor = 'ieeeBrandId&quot;:15,'
        out = out.replace(anchor, anchor + SLOT["hbs"], 1)
    # current session row: the <tr> carrying a <td> date inside <thead>
    mcur = re.search(
        r"<tr><td>[\d\.]+</td><td>[^<]*</td></tr>(?=</thead>)", out)
    if mcur:
        out = out[: mcur.start()] + SLOT["currrow"] + out[mcur.end():]
    else:
        mcur = re.search(r"<tr><td>[\d\.]+</td><td>[^<]* UTC</td></tr>", out)
        if mcur:
            out = out[: mcur.start()] + SLOT["currrow"] + out[mcur.end():]
    # other sessions block: all <tr><td>..date..</td></tr> after the second
    # <thead>, up to before <p class="actions">
    mblk = re.search(
        r"(<tr><td>[\d\.]+</td><td>[^<]*</td></tr>)+"
        r"(?=\s*<p class=\"actions\")",
        out)
    if mblk:
        out = out[: mblk.start()] + SLOT["otherrows"] + out[mblk.end():]
    return out


def main():
    clean = load("settle_clean")
    flags = load("settle_flags")
    sess = load("sessions_a")

    ctx = {}
    h = headers("settle_clean")
    hdrs = dict(h["headers"])
    # csrf from the clean capture's meta
    m = re.search(r'name="ol-csrfToken" content="([^"]+)"', clean)
    ctx["csrf"] = m.group(1)
    # nonce from the CSP header of the clean capture
    m = re.search(r"script-src 'nonce-([^']+)'", hdrs.get(
        "Content-Security-Policy", ""))
    ctx["nonce"] = m.group(1)
    # uid + email from the ol-user meta of the clean capture
    m = re.search(r'ol-user" data-type="json" content="[^"]*?\\?&quot;id&quot;:&quot;([0-9a-f]{24})', clean)
    if not m:
        m = re.search(r'ol-user_id" content="([0-9a-f]{24})"', clean)
    ctx["uid"] = m.group(1)
    CTX_CACHE["ol-samlBeta"] = "CAP-SAMLBETA"
    CTX_CACHE["ol-ssoErrorMessage"] = ""
    CTX_CACHE["ol-projectSyncSuccessMessage"] = ""
    CTX_CACHE["ol-projectSyncErrorMessage"] = ""
    CTX_CACHE["ol-referenceLinkingErrorMessage"] = ""

    settled = slotify_settings(clean, ctx)
    settled = slotify_settings_dynamic(settled)

    sessled = slotify_sessions(sess, ctx)

    # sanity: no residual dynamic values
    for tag, doc in (("settle", settled), ("sess", sessled)):
        for bad in (ctx["csrf"], ctx["nonce"], ctx["uid"], EMAIL):
            if bad in doc:
                raise SystemExit(f"{tag}: residual {bad[:16]} in skeleton")

    # the settings skeleton must render BOTH clean and flags captures:
    # verify by reversing the slots with the flags values
    def render(skel, vals):
        out = skel.replace(SLOT["csrf"], vals["csrf"])
        out = out.replace(SLOT["nonce"], vals["nonce"])
        out = out.replace(SLOT["email"], EMAIL)
        out = out.replace(SLOT["uid"], ctx["uid"])
        out = out.replace(SLOT["user"], vals["user"])
        out = out.replace(SLOT["hbs"], vals["hbs"])
        out = out.replace(SLOT["haspw"], HASPW_VAL)
        out = out.replace(SLOT["showai"], SHOWAI_VAL)
        out = out.replace(SLOT["samlmeta"], vals.get("samlmeta", ""))
        out = out.replace(SLOT["ssomsg"], vals.get("ssomsg", ""))
        out = out.replace(SLOT["syncock"], vals.get("syncock", ""))
        out = out.replace(SLOT["syncerr"], vals.get("syncerr", ""))
        out = out.replace(SLOT["referr"], vals.get("referr", ""))
        return out

    # bare-content boolean metas: derive their clean-capture state
    HASPW_VAL = ' content' if 'ol-hasPassword" data-type="boolean" content>' in clean else ''
    SHOWAI_VAL = ' content' if 'ol-showAiFeatures" data-type="boolean" content>' in clean else ''

    # clean render (no pop values, no user-meta beyond capture)
    muser = re.search(
        r'<meta name="ol-user" data-type="json" content="([^"]*)">', clean)
    clean_render = render(settled, {
        "csrf": ctx["csrf"], "nonce": ctx["nonce"],
        "user": muser.group(1), "hbs": "",
    })
    clean_render = clean_render.replace(
        ",\u0001HBS33\u0002", "").replace(SLOT["hbs"], "")
    # (slotify inserted the marker after 15, — for clean render remove it)
    if clean_render != clean:
        # find first difference for debugging
        for i, (a, b) in enumerate(zip(clean_render, clean)):
            if a != b:
                print("first diff at", i)
                print("render:", clean_render[max(0, i - 80): i + 120])
                print("clean :", clean[max(0, i - 80): i + 120])
                break
        else:
            print("len diff", len(clean_render), len(clean))
        raise SystemExit("clean render mismatch")
    print("settings skeleton verified against clean capture")

    with open(OUT, "w", encoding="utf-8") as f:
        f.write("//go:build !ignore\n//\n")
        f.write("// Code generated by tools/webviews-gen-p3c.py from the Node\n")
        f.write("// captures (settle_clean.html / sessions_a.html). Do not edit.\n")
        f.write("package views\n\n")
        f.write("const settingsHTML = " + go_string(settled) + "\n\n")
        f.write("const sessionsHTML = " + go_string(sessled) + "\n")

    # also keep a copy in RAW for the Go render-verification test
    with open(os.path.join(RAW, "skeletons.json"), "w", encoding="utf-8") as f:
        json.dump({"settings": settled, "sessions": sessled,
                   "ctx": ctx}, f)
    print("wrote", OUT, len(settled), "+", len(sessled), "bytes")


if __name__ == "__main__":
    main()
