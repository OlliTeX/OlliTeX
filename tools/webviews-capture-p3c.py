#!/usr/bin/env python3
"""Capture the Node web service's P3.3 pages (user settings + sessions)
as byte-exact Go skeletons. Run against a live Node web service
(e2e default http://127.0.0.1:7420).

Captures (e2e-user fixture; session A + sibling B):
  settle_clean   — GET /user/settings (clean session, no pop-flags)
  settle_flags   — GET /user/settings (pop-flags + samlBeta seeded)
  sessions_a     — GET /user/sessions (one other session tracked)
  sessions_list  — GET /user/sessions/list (JSON twin)
  full response header dumps -> <tag>_headers.json for each.

Outputs raw bytes to tools/capture-p3c-raw/ for review; Go skeletons are
generated in a second pass only after diffing the two settings captures.
"""
import http.client
import json
import os
import re
import subprocess
import sys
import time
import urllib.parse

BASE_HOST = "127.0.0.1"
BASE_PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 7420
E2E_USER = "e2e-user@e2e.test"
E2E_PASS = "Ol-Fixture-3m2Q"
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RAW = os.path.join(ROOT, "tools", "capture-p3c-raw")


def request(method, path, headers=None, body=None):
    conn = http.client.HTTPConnection(BASE_HOST, BASE_PORT, timeout=30)
    conn.request(method, path, headers=headers or {}, body=body)
    resp = conn.getresponse()
    data = resp.read()
    conn.close()
    return resp.status, resp.getheaders(), data


def cookie_of(headers):
    for k, v in headers:
        if k.lower() != "set-cookie":
            continue
        first = v.split(";", 1)[0].strip()
        if "=" in first and first.split("=", 1)[0] == "overleaf.sid":
            return first.split("=", 1)[1]
    return ""


def anon_session():
    status, headers, html = request("GET", "/login")
    assert status == 200, status
    c = cookie_of(headers)
    csrf = re.search(r'name="ol-csrfToken" content="([^"]+)"', html.decode()).group(1)
    return c, csrf


def login():
    c, csrf = anon_session()
    s, h, b = request(
        "POST", "/login",
        headers={"cookie": "overleaf.sid=" + c, "content-type": "application/json",
                 "x-csrf-token": csrf, "accept": "application/json"},
        body=json.dumps({"email": E2E_USER, "password": E2E_PASS}))
    assert s in (200, 302), (s, b[:200])
    time.sleep(0.6)
    return cookie_of(h) or c


def redis_plain(args):
    out = subprocess.run(
        ["docker", "exec", "ol-e2e-redis-1", "redis-cli", *args],
        capture_output=True, text=True, timeout=15,
    )
    return out.stdout.strip()


def sid_of(cookie_wire):
    v = urllib.parse.unquote(cookie_wire)
    if v.startswith("s:"):
        v = v[2:]
    return v.split(".", 1)[0]


def patch_session_doc(sid, patch):
    key = "sess:" + sid
    doc = json.loads(redis_plain(["GET", key]))
    doc.update(patch)
    val = json.dumps(doc, separators=(",", ":"))
    out = subprocess.run(
        ["docker", "exec", "ol-e2e-redis-1", "redis-cli", "SET", key, val],
        capture_output=True, text=True, timeout=15,
    ).stdout.strip()
    assert "OK" in out, (out, key)


def main():
    os.makedirs(RAW, exist_ok=True)
    cookieA = login()
    sidA = sid_of(cookieA)
    cookieB = login()
    sidB = sid_of(cookieB)
    print("sidA", sidA, "sidB", sidB)

    def get(path, cookie, tag):
        s, h, b = request("GET", path, headers={"cookie": "overleaf.sid=" + cookie})
        with open(os.path.join(RAW, tag + ".html"), "wb") as f:
            f.write(b)
        with open(os.path.join(RAW, tag + "_headers.json"), "w") as f:
            json.dump({"status": s,
                       "headers": [[k, v] for k, v in h]}, f, indent=1)
        print(tag, s, len(b), "bytes")
        return b

    settle_clean = get("/user/settings", cookieA, "settle_clean")
    print("clean has ssoMsg:", b"ssoErrorMessage" in settle_clean)

    # state 2 — pop-flags + samlBeta (server-side seeded)
    patch_session_doc(sidA, {
        "ssoErrorMessage": "CAP-SAML-ERR",
        "ssoError": "saml",
        "projectSyncSuccessMessage": "CAP-SYNC-OK",
        "projectSyncErrorMessage": "CAP-SYNC-ERR",
        "referenceLinkingErrorMessage": "CAP-REF-ERR",
        "samlBeta": "CAP-SAMLBETA",
    })
    settle_flags = get("/user/settings", cookieA, "settle_flags")
    for k in (b"ssoErrorMessage", b"projectSyncSuccessMessage",
              b"projectSyncErrorMessage", b"referenceLinkingErrorMessage",
              b"samlBeta"):
        print("flags has", k.decode(), ":", k in settle_flags)

    # sessions page (A viewing; B is the other tracked session)
    sessions_a = get("/user/sessions", cookieA, "sessions_a")
    print("sessions has B sid:", sidB.encode() in sessions_a)

    s, h, b = request("GET", "/user/sessions/list",
                      headers={"cookie": "overleaf.sid=" + cookieA,
                               "accept": "application/json"})
    with open(os.path.join(RAW, "sessions_list.json"), "wb") as f:
        f.write(b)
    print("sessions/list", s, b.decode()[:200])

    print("RAW dir:", RAW)
    print("sids:", sidA, sidB)


if __name__ == "__main__":
    main()
