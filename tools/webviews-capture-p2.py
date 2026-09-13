#!/usr/bin/env python3
"""Capture the Node web service's P2 pages (password reset / set / token
access / sharing updates) as byte-exact Go skeletons. Run from the overleaf
root against a live Node web service (e2e default http://127.0.0.1:7420).

P2 slots added on top of the P1 set (value-only, inside attribute quotes):
  \\x01EMAIL\\x02      — rendered email local (password set form)
  \\x01RSTOKEN\\x02    — password reset token (hidden input, per flow)
  \\x01USER\\x02       — layout user meta content (JSON or empty)
  \\x01RESETERR\\x02   — ol-password-reset-error meta content
  \\x01POSTURL\\x02    — token page postUrl meta
Appends to go/services/web/views/pages_data.go (keeps existing constants).
The e2e gate (web-go flip spec) remains the authority for live parity.
"""
import http.client
import json
import os
import re
import sys

BASE_HOST = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1"
BASE_PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 7420
E2E_USER = "e2e-user@e2e.test"
E2E_PASS = "Ol-Fixture-3m2Q"
HERE_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def request(method, path, headers=None, body=None):
    conn = http.client.HTTPConnection(BASE_HOST, BASE_PORT, timeout=20)
    conn.request(method, path, headers=headers or {}, body=body)
    resp = conn.getresponse()
    data = resp.read().decode("utf-8")
    conn.close()
    return resp.status, resp.getheaders(), data


def cookie_of(headers):
    if "Set-Cookie" not in dict((k, v) for k, v in headers):
        return ""
    sc = ""
    for k, v in headers:
        if k.lower() == "set-cookie":
            sc = v
            break
    return "".join(
        k.split("=", 1)[0] + "=" + k.split("=", 1)[1].split(";")[0] + "; "
        for k in sc.split(", ")
        if "=" in k
    ).rstrip("; ")


def anon_session():
    status, headers, login_html = request("GET", "/login")
    assert status == 200, status
    c = cookie_of(headers)
    csrf = re.search(r'name="ol-csrfToken" content="([^"]+)"', login_html).group(1)
    return c, csrf


def docker_mongo(query):
    import subprocess

    out = subprocess.run(
        ["docker", "exec", "ol-e2e-mongo-1", "mongosh", "--quiet",
         "--eval", query, "sharelatex"],
        capture_output=True, text=True, timeout=60,
    )
    return out.stdout


def h0loc(h):
    d = {}
    for k, v in h:
        d[k.lower()] = v
    return d.get("location")


def main():
    # login first (session regeneration kills any pre-login anonymous sid)
    l_c, l_csrf = anon_session()
    s, h, body = request(
        "POST", "/login",
        headers={"cookie": l_c, "content-type": "application/json",
                 "x-csrf-token": l_csrf, "accept": "application/json"},
        body=json.dumps({"email": E2E_USER, "password": E2E_PASS}))
    assert s in (200, 302), (s, body[:200])
    import time
    time.sleep(0.8)
    login_cookie = cookie_of(h) or l_c

    # fresh anonymous session A for the reset flow (its sid is what the
    # reset POST + set-page redirects validate against)
    anon_c, anon_csrf = anon_session()

    # --- password reset request page (anonymous) ---
    s, h, raw_pr = request("GET", "/user/password/reset")
    assert s == 200, (s, raw_pr[:300])

    # --- obtain a real reset token via POST reset (email to sink) ---
    s, h, body = request(
        "POST", "/user/password/reset",
        headers={"cookie": anon_c, "content-type": "application/json",
                 "x-csrf-token": anon_csrf, "accept": "application/json"},
        body=json.dumps({"email": E2E_USER}))
    assert s == 200, (s, body[:300])
    tok = None
    for _ in range(20):
        out = docker_mongo(
            "db.tokens.find().sort({createdAt:-1}).limit(3).toArray()")
        m = re.search(r"token['\"]?\s*[:=]\s*['\"]?([0-9a-f]{64})", out)
        if m and "password" in out:
            tok = m.group(1)
            break
        time.sleep(0.5)
    assert tok, "could not read reset token from mongo: %r" % out[:200]

    # --- password set page: follow redirect chain with the anon session ---
    s, h, _ = request(
        "GET", "/user/password/set?passwordResetToken=%s&email=%s" % (tok, E2E_USER),
        headers={"cookie": anon_c})
    assert s == 302, (s, dict(h).get("Location"))
    s, h, raw_sp = request(
        "GET", "/user/password/set?email=%s" % E2E_USER,
        headers={"cookie": anon_c})
    assert s == 200, (s, raw_sp[:300])

    # --- token access pages: the global gate bounces anonymous (302
    # /login — Go's gate is P1-pinned) — capture the logged-in render ---
    real_rw = "1234567890abcdefgh"  # fixture: e2e-seed-project
    s, h, _ = request("GET", "/" + real_rw)
    assert s == 302 and h0loc(h), (s, h0loc(h))
    s, h, raw_ta_log = request("GET", "/" + real_rw, headers={"cookie": login_cookie})
    assert s == 200, (s, raw_ta_log[:300])

    # --- sharing updates page (fixture user is an rw-token member) ---
    su_page = None
    for pidc in ("6aa4b8c973ef0e5094f4cc02",):
        s, h, raw_su2 = request(
            "GET", "/project/%s/sharing-updates" % pidc,
            headers={"cookie": login_cookie})
        if s == 200:
            su_page = raw_su2
            break
    if su_page is None:
        s, h, raw_su = request("GET", "/project/zzz-nonexistent/sharing-updates",
                               headers={"cookie": login_cookie})
        assert s in (200, 404, 302), (s, raw_su[:200])

    td = os.path.join(HERE_ROOT, "go", "services", "web", "views", "testdata")
    os.makedirs(td, exist_ok=True)
    CS, NO = "\x01CSRF\x02", "\x01NONCE\x02"
    EM = "\x01EMAIL\x02"
    RT = "\x01RSTOKEN\x02"
    US = "\x01USER\x02"
    RE = "\x01RESETERR\x02"
    PU = "\x01POSTURL\x02"

    def slot(raw, csrf=None):
        skel = raw
        if csrf:
            m = re.search(r'name="ol-csrfToken" content="([^"]+)"', raw)
            if m:
                skel = skel.replace('content="%s"' % m.group(1), 'content="%s"' % CS, 1)
        for n in sorted(set(re.findall(r'nonce="([^"]+)"', raw))):
            skel = skel.replace('nonce="%s"' % n, 'nonce="%s"' % NO)
        return skel

    pages = []

    def add(name, raw, csrf, special=None):
        skel = slot(raw, csrf)
        # per-user layout slots (ol-usersEmail / ol-user_id)
        skel = re.sub(
            r'(ol-usersEmail"[^>]*content=")[^"]*(")',
            lambda m: m.group(1) + US + m.group(2), skel, count=1, flags=re.S)
        if 'ol-user_id" content="' in skel:
            skel = re.sub(
                r'(ol-user_id"[^>]*content=")[0-9a-f]{24}(")',
                lambda m: m.group(1) + "\x01USERID\x02" + m.group(2), skel, count=1)
        if special:
            skel = special(skel, raw)
        with open(os.path.join(td, name + ".html"), "w") as f:
            f.write(raw)
        with open(os.path.join(td, name + ".skel"), "w") as f:
            f.write(skel)
        pages.append((name, skel))
        print("%s: %d bytes" % (name, len(raw)))

    # passwordReset: error meta (order-tolerant) → RESETERR slot
    def pr_special(s, r):
        s = re.sub(
            r'(name="ol-password-reset-error"[^>]*content=")[^"]*(")',
            lambda m: m.group(1) + RE + m.group(2), s, count=1)
        return s
    add("passwordResetHTML", raw_pr,
        re.search(r'ol-csrfToken" content="([^"]+)"', raw_pr).group(1),
        special=pr_special)

    # setPassword: email value + token value (order-tolerant)
    sp = raw_sp
    skel = slot(sp, re.search(r'ol-csrfToken" content="([^"]+)"', sp).group(1))
    skel = re.sub(
        r'(name="email"[^>]*value=")[^"]*(")',
        lambda m: m.group(1) + EM + m.group(2), skel, count=1)
    skel = re.sub(
        r'(name="passwordResetToken"[^>]*value=")[0-9a-f]{64}(")',
        lambda m: m.group(1) + RT + m.group(2), skel, count=1)
    with open(os.path.join(td, "setPasswordHTML.html"), "w") as f:
        f.write(sp)
    with open(os.path.join(td, "setPasswordHTML.skel"), "w") as f:
        f.write(skel)
    pages.append(("setPasswordHTML", skel))
    print("setPasswordHTML: %d bytes" % len(sp))

    # token access: postUrl + user meta (captured logged-in → USEREMAIL/USERID slots)
    tlog = raw_ta_log
    skel = slot(tlog, re.search(r'ol-csrfToken" content="([^"]+)"', tlog).group(1))
    skel = re.sub(
        r'(name="ol-postUrl"[^>]*content=")[^"]*(")',
        lambda m: m.group(1) + PU + m.group(2), skel, count=1)
    skel = re.sub(
        r'(ol-usersEmail"[^>]*content=")[^"]*(")',
        lambda m: m.group(1) + US + m.group(2), skel, count=1)
    skel = re.sub(
        r'(ol-user_id"[^>]*content=")[0-9a-f]{24}(")',
        lambda m: m.group(1) + "\x01USERID\x02" + m.group(2), skel, count=1)
    with open(os.path.join(td, "tokenAccessLegacyHTML.html"), "w") as f:
        f.write(tlog)
    with open(os.path.join(td, "tokenAccessLegacyHTML.skel"), "w") as f:
        f.write(skel)
    pages.append(("tokenAccessLegacyHTML", skel))
    print("tokenAccessLegacyHTML: %d bytes" % len(tlog))

    if su_page:
        su = su_page
        skel = slot(su, re.search(r'ol-csrfToken" content="([^"]+)"', su).group(1))
        skel = re.sub(
            r'(ol-usersEmail"[^>]*content=")[^"]*(")',
            lambda m: m.group(1) + US + m.group(2), skel, count=1)
        skel = re.sub(
            r'(ol-user_id"[^>]*content=")[0-9a-f]{24}(")',
            lambda m: m.group(1) + "\x01USERID\x02" + m.group(2), skel, count=1)
        with open(os.path.join(td, "sharingUpdatesHTML.html"), "w") as f:
            f.write(su)
        with open(os.path.join(td, "sharingUpdatesHTML.skel"), "w") as f:
            f.write(skel)
        pages.append(("sharingUpdatesHTML", skel))
        print("sharingUpdatesHTML: %d bytes" % len(su))
    else:
        print("sharingUpdatesHTML: SKIPPED (fixture not created)")

    out_path = os.path.join(HERE_ROOT, "go", "services", "web", "views", "pages_data_p2.go")
    consts = ["package views\n\n// Code generated by tools/webviews-capture-p2.py (P2).",
              "// DO NOT EDIT BY HAND — regenerate + re-run the e2e parity gate."]
    for name, skel in pages:
        consts.append("const %s = " % name + json.dumps(skel, ensure_ascii=False))
    with open(out_path, "w") as f:
        f.write("\n".join(consts) + "\n")
    print("pages_data_p2.go written (%d pages)" % len(pages))
    print("SLOT VALUES: rs_token=%s..." % tok[:12])


if __name__ == "__main__":
    main()
