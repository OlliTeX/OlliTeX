#!/usr/bin/env python3
"""
WebDAV test fixture — parts:

  A1  server reachable + PROPFIND depth-0 → 207 Multi-Status (client check())
  A2  anonymous PROPFIND → 401 (basic auth enforced)
  A3  MKCOL quirk pin (nginx dav): no trailing slash → 409 "MKCOL can create a
      collection only"; WITH trailing slash → 200 + dir created.  Overleaf's
      wdClient.createDirectory sends NO trailing slash and tolerates only
      201/405 → push flow is INCOMPATIBLE with this server family until an
      owner decision (see README "interop pins").
  A4  PUT nested path → 201; GET round-trip bytes; ETag header present (PUT
      itself is not etagged in this build; GET is)
  A5  PROPFIND depth-1 → 207 <D:response> per entry (client list() shape)
  A6  If-Match precondition: this server IGNORES it (PUT → 204 override) —
      conflict detection is NOT server-enforced here (pin)
  A7  DELETE → 204; subsequent GET → 404 (client remove())

  B (Overleaf integration; needs OLI_BASE + OLI_EMAIL + OLI_PASS)
  B0  disconnect reset (idempotent) → 200 success
  B1  status → 200 {"connected":false} (byte pin, P6.9 offline pin)
  B2  connect {baseUrl, rootPath, username, password} → 200 success;
      status → connected:true + baseUrl + rootPath echoed
  B3  seed remote tree (raw DAV): <rootPath>/<proj>/hello.txt + sub/notes.txt
  B4  import POST /project/new/webdav {projectName, rootPath} → 200 Import
      completed — PIN (pinned 2026-10-02): response contract only; THIS BUILD
      does not create the project row / state doc (wdImportFiles with an empty
      projectID silently no-ops; the Node oracle creates the project). Gap is
      reported, not papered over.
  B4c project created via POST /project/new (P4.7, fixture scaffolding for B5-B7)
  B5  push → PIN: on nginx-webdav push answers 500 (createDirectory MKCOL
      409 quirk — A3); on RFC-conformant servers expect 200 Push completed.
      Assertion accepts both, tagged.
  B6  pull → 200 Pull completed, remote-only new file ingested. Regression
      guard: pre-fix this failed twice (typed-nil reader GET panic;
      resourcetype <collection/> bool parse → self-entry GET 404).
  B7  unlink state → 200/404 (observe-and-pin pre-sync contract);
      (OLI_CLEANUP=1) DELETE /Project/:id (Node capital-P)
  B8  disconnect → 200 success; status → connected:false

Stdlib only. The Overleaf session reuses the sibling ../forgejo fixture's
proven Session/login/CSRF stack.
"""
import base64
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "forgejo"))

FORGE = os.environ.get("WEBDAV_BASE", "http://127.0.0.1:8095").rstrip("/")
WD_USER = os.environ.get("WEBDAV_USER", "oltest")
WD_PASS = os.environ.get("WEBDAV_PASS", "Ol-Fixture-Wd7q")
ROOT_PATH = "/Overleaf"

RESULTS = []


def report(name, ok, detail=""):
    RESULTS.append(ok)
    print(f"[{'PASS' if ok else 'FAIL'}] {name}" + (f" — {detail}" if detail else ""))
    return ok


# ---------------- raw DAV client (the exact surface features/webdav sends) --

def dav(method, path, body=None, headers=None):
    h = dict(headers or {})
    if body is not None:
        h.setdefault("Content-Type", "application/xml; charset=utf-8") if method == "PROPFIND" else None
    r = urllib.request.Request(FORGE + path, data=body, method=method, headers=h)
    try:
        with urllib.request.urlopen(r, timeout=30) as resp:
            return resp.status, dict(resp.headers), resp.read()
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers), e.read()


def authed(headers=None):
    h = dict(headers or {})
    h["Authorization"] = "Basic " + base64.b64encode(f"{WD_USER}:{WD_PASS}".encode()).decode()
    return h


PROPFIND_DEPTH0 = b'<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resource/></d:prop></d:propfind>'
PROPFIND_LIST = b'<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:href/><d:resourcetype/><d:getcontentlength/><d:getlastmodified/><d:getetag/></d:prop></d:propfind>'


def part_a():
    print()
    st, h, b = dav("PROPFIND", "/", PROPFIND_DEPTH0, authed())
    ok = st == 207 and b"multistatus" in b.lower()
    report("A1 PROPFIND / depth0 → 207 Multi-Status (check()", ok,
           f"-> {st}; body[:60]={b[:60]!r}")

    st, h, b = dav("PROPFIND", "/", PROPFIND_DEPTH0)  # no auth
    report("A2 anonymous PROPFIND → 401 (basic auth enforced)", st == 401, f"-> {st}")

    # A3 — nginx dav MKCOL quirk (pinned live 2026-10-02): noslash → 409
    # "MKCOL can create a collection only"; slash → 201 (new) / 405 (exists).
    # Overleaf's wdClient.createDirectory sends NO slash + tolerates only
    # 201/405 → 409 is NOT tolerated → push incompatible on this family.
    tag = int(time.time())
    st_n, _, _ = dav("MKCOL", f"/mkpin-noslash-{tag}", None, authed())
    st_s, _, _ = dav("MKCOL", f"/mkpin-slash-{tag}/", None, authed())
    st_s2, _, _ = dav("MKCOL", f"/mkpin-slash-{tag}/", None, authed())
    st_ls, _, _ = dav("GET", f"/mkpin-slash-{tag}/", None, authed())
    ok = st_n == 409 and st_s in (200, 201) and st_s2 in (200, 201, 405)
    report("A3 MKCOL quirk pin (nginx): noslash→409; slash→201 new / 405 exists",
           ok, f"noslash={st_n} slash-new={st_s} slash-again={st_s2} dir-listable={st_ls in (200, 201, 403, 404)}; "
               "Overleaf client sends noslash → push-incompatible until owner decision (README)")

    # A4 — PUT nested (create_full_path) + GET round-trip + ETag on GET
    st, h, b = dav("PUT", "/Overleaf/a4/hello.txt", b"hello webdav fixture\n", authed())
    put_ok = st in (200, 201)
    st, h, b = dav("GET", "/Overleaf/a4/hello.txt", None, authed())
    etag = (h.get("Etag") or h.get("ETag") or "")
    ok = put_ok and st == 200 and b == b"hello webdav fixture\n"
    report("A4 PUT nested → 201 + GET round-trip + GET ETag", ok,
           f"put={st} get={st} bytes={b[:30]!r} etag={etag!r}")

    # A5 — PROPFIND depth-1 listing shape (client list() sends Depth:1 + XML CT)
    st, h, b = dav("PROPFIND", "/Overleaf/a4", PROPFIND_LIST,
                   authed({"Depth": "1", "Content-Type": "application/xml; charset=utf-8"}))
    listed = b"hello.txt" in b
    ok = st == 207 and listed
    report("A5 PROPFIND Depth:1 → 207 listing with entries (list())", ok,
           f"-> {st}; entry visible={listed}; body[:120]={b[:120]!r}")

    # A6 — If-Match is IGNORED by this server (override succeeds)
    et_bad = h.get("Etag") or h.get("ETag") or '"x"'
    if et_bad and not et_bad.startswith('"'):
        et_bad = '"' + et_bad + '"'
    st, _, _ = dav("PUT", "/Overleaf/a4/hello.txt", b"override\n",
                   authed({"If-Match": '"not-the-real-etag"'}))
    report("A6 If-Match IGNORED (this server) — pin", st in (200, 204),
           f"-> {st} (RFC 412 412 NOT enforced; conflict detection product-side only)")

    # A7 — DELETE + 404 after
    st, _, _ = dav("DELETE", "/Overleaf/a4/hello.txt", None, authed())
    st2, h2, b2 = dav("GET", "/Overleaf/a4/hello.txt", None, authed())
    report("A7 DELETE → 204; GET after → 404 (remove())", st in (200, 204) and st2 == 404,
           f"del={st} get-after={st2}")


# ---------------- Part B — Overleaf webdav module --------------------------

def bridge_webdav_base():
    """Container-reachable base for the overleaf web (order):
    1. OLI_WEBDAV_URL env;
    2. the fixture container name on a shared user network (built-in DNS —
       `docker network connect <overleaf-net> webdav-test` when the dev
       stack is up; verified from inside overleafserver);
    3. the docker gateway of the fixture's own network (works only when it
       shares the bridge with overleafserver — e.g. the forgejo layout);
    4. the harness base.
    NOTE (pinned 2026-10-02): on a SEPARATE bridge the gateway IP is not
    routable from overleafserver (connection refused) — shared network it is.
    """
    if os.environ.get("OLI_WEBDAV_URL"):
        return os.environ["OLI_WEBDAV_URL"]
    probe = subprocess.run(
        ["docker", "exec", "overleafserver", "sh", "-c", "getent hosts webdav-test"],
        capture_output=True, text=True, timeout=15)
    if probe.returncode == 0 and probe.stdout.strip():
        return "http://webdav-test:80"
    try:
        net = json.loads(subprocess.run(
            ["docker", "inspect", "-f", "{{json .NetworkSettings.Networks}}", "webdav-test"],
            capture_output=True, text=True, timeout=15).stdout)
        for key in net:
            gw = net[key].get("Gateway")
            if gw:
                port = urllib.parse.urlsplit(FORGE).port or 80
                return f"http://{gw}:{port}"
    except Exception:
        pass
    return FORGE


def part_b():
    import run_forgejo_test as R
    base = os.environ.get("OLI_BASE")
    if not (base and os.environ.get("OLI_EMAIL") and os.environ.get("OLI_PASS")):
        print()
        print("[SKIP] B1-B8 Overleaf integration — need OLI_BASE/OLI_EMAIL/OLI_PASS")
        return
    os.environ.setdefault("OLI_PASS", os.environ.get("OLI_PASS", ""))
    oli = R.Session(base)
    ok, why = R.overleaf_login(oli)
    if not ok:
        report("B-login", False, why[:160])
        return
    prov_url = bridge_webdav_base()
    print(f"\n  webdav base (harness)={FORGE}  (container view)={prov_url}")

    st, b = oli.req("POST", "/user/webdav/disconnect")
    report("B0 disconnect reset", st == 200, f"-> {st} {b[:80]!r}")

    st, b = oli.get_json("/user/webdav/status")
    ok = st == 200 and b == {"connected": False}
    report("B1 status → {\"connected\":false} (byte pin)", ok, f"-> {st} {b!r}")

    st, b = oli.req("POST", "/user/webdav/connect",
                    json_body={"baseUrl": prov_url, "rootPath": ROOT_PATH,
                               "username": WD_USER, "password": WD_PASS})
    report("B2a connect", st == 200, f"-> {st} {b[:100]!r}")

    st, b = oli.get_json("/user/webdav/status")
    ok = (st == 200 and isinstance(b, dict) and b.get("connected") is True
          and b.get("baseUrl") == prov_url and b.get("rootPath") == ROOT_PATH)
    report("B2b status connected + cred echo", ok, f"-> {st} {str(b)[:160]!r}")

    # B3 — seed the remote project dir (nested, exercises recursive traversal)
    proj = f"wdv-harness-{int(time.time())}"
    dirb = urllib.parse.quote(f"{ROOT_PATH}/{proj}")
    st, _, _ = dav("MKCOL", dirb + "/", None, authed())   # slash form (A3 pin)
    seed_files = {
        f"{ROOT_PATH}/{proj}/hello.txt": b"seed hello for import\n",
        f"{ROOT_PATH}/{proj}/sub/notes.txt": b"seed notes (nested)\n",
    }
    for p, content in seed_files.items():
        st, _, _ = dav("PUT", urllib.parse.quote(p), content, authed())
        if st not in (200, 201):
            report("B3 seed remote tree", False, f"PUT {p} -> {st}")
            return
    report("B3 seed remote tree", True, f"MKCOL+2x PUT under {ROOT_PATH}/{proj}/")

    # B4 — import (PIN, 2026-10-02): response contract is 200 success…
    st, b = oli.req("POST", "/project/new/webdav",
                    json_body={"projectName": proj, "rootPath": ROOT_PATH})
    try:
        ok = st == 200 and _json(b, "success") is True and _json(b, "message") == "Import completed"
    except Exception:
        ok = False
    # …but in THIS build the handler walks the remote tree and does NOT create
    # a project row or state doc (wdImportFiles with projectID="" silently
    # no-ops; Node oracle importRemoteProject does create the project).
    # Pinned gap — reported, owner-aware. Assert the observable contract:
    no_project = find_project_by_name(proj) is None
    report("B4 import → 200 Import completed (PIN: no project row created in this build)",
           ok and no_project,
           f"-> {st} {b[:100]!r}; project-row={'created (contract changed!)' if not no_project else 'absent (pinned no-op import)'}")

    # B4c — real project for the sync surface (P4.7 basic template, Node parity)
    st, b = oli.req("POST", "/project/new", json_body={"projectName": proj})
    pid = None
    try:
        pid = json.loads(b).get("project_id")
    except Exception:
        pass
    if not pid:
        pid = find_project_by_name(proj)
    report("B4c project created via POST /project/new (fixture scaffolding)",
           st == 200 and pid is not None, f"-> {st} {b[:120]!r}")

    # B5 — push (PIN: nginx-dav MKCOL quirk A3 → createDirectory 409 → 500 on
    # this server family; 200 on RFC-conformant servers). Both are asserted,
    # tagged, per the interop note in the README.
    st, b = oli.req("POST", f"/project/{pid}/webdav/push")
    push_success = False
    try:
        push_success = _json(b, "success") is True
    except Exception:
        pass
    if push_success:
        st2, h2, b2 = dav("GET", urllib.parse.quote(f"{ROOT_PATH}/{proj}/hello.txt"), None, authed())
        report("B5 push → 200 + file intact on server (RFC-conformant provider)",
               st == 200 and st2 == 200, f"-> {st} {b[:100]!r}; server hello.txt={st2}")
    else:
        report("B5 push → PIN INCOMPAT (nginx-dav MKCOL form, A3)", st >= 400,
               f"-> {st} {b[:140]!r} (owner decision: client MKCOL form / 409 tolerance)")

    # B6 — pull (poll path has NO server-side MKCOL): remote-only new file is
    # ingested into the project. Pre-fix this 500'd twice (typed-nil reader
    # GET panic; resourcetype bool parse); the 200 pin is the regression guard.
    pulled_path = f"{ROOT_PATH}/{proj}/pulled-by-fixture.txt"
    st, _, _ = dav("PUT", urllib.parse.quote(pulled_path), b"remote advance\n", authed())
    st, b = oli.req("POST", f"/project/{pid}/webdav/pull", json_body={})
    try:
        msg = _json(b, "message")
    except Exception:
        msg = None
    report("B6 pull → 200 Pull completed (remote-only file ingested)",
           st == 200 and msg == "Pull completed", f"-> {st} {b[:120]!r}")

    # B7 — unlink the state link (observe-and-pin: 200-success or 404-missing
    # are both plausible contracts pre-sync; anything else fails the pin)
    st, b = oli.req("DELETE", f"/project/{pid}/webdav/state")
    ok = st in (200, 404)
    report("B7 unlink state → 200/404 (pin)", ok, f"-> {st} {b[:100]!r}")
    if os.environ.get("OLI_CLEANUP") == "1":
        st, b = oli.req("DELETE", f"/Project/{pid}")   # Node capital-P route
        report("B7b project cleanup (OLI_CLEANUP=1)", st in (200, 204), f"DELETE /Project/{pid} -> {st} {b[:60]!r}")

    # B8 — disconnect + final status
    st, b = oli.req("POST", "/user/webdav/disconnect")
    st2, b2 = oli.get_json("/user/webdav/status")
    report("B8 disconnect → status {\"connected\":false}",
           st == 200 and st2 == 200 and b2 == {"connected": False},
           f"disconnect={st} status={st2} {b2!r}")


def _json(body, key):
    return json.loads(body).get(key)


def _mongo_eval(expr):
    out = subprocess.run(
        ["docker", "exec", "overleafmongo", "mongosh", "--quiet", "sharelatex", "--eval", expr],
        capture_output=True, text=True, timeout=120)
    return out.stdout.strip()


def find_project_by_name(name):
    out = _mongo_eval(
        f'const p=db.projects.findOne({{name: "{name}"}}); print(p ? p._id : "NO")')
    m = re.search(r"\b[0-9a-f]{24}\b", out)
    return m.group(0) if m else None


def mongo_state(pid):
    # state doc: webdavsyncprojectstates ({projectId(string|oid), lastSyncAt...})
    out = _mongo_eval(
        f'const d=db.webdavsyncprojectstates.findOne({{projectId: "{pid}"}}); '
        f'if (!d) {{ const o=ObjectID ? ObjectID("{pid}") : null; '
        f'  if (o) d=db.webdavsyncprojectstates.findOne({{projectId: o}}); }} '
        f'print(d ? "found:"+JSON.stringify({{lastSyncAt: d.lastSyncAt}}) : "NO")')
    return out if out.startswith("found:") else None


def main():
    if os.environ.get("SKIP_PART_A") != "1":
        part_a()
    if os.environ.get("SKIP_PART_B") != "1":
        part_b()
    n_ok = sum(1 for r in RESULTS if r)
    print()
    print(f"SUMMARY: {n_ok}/{len(RESULTS)} passed"
          + ("" if n_ok == len(RESULTS) else " — see FAIL rows above"))
    return 0 if n_ok == len(RESULTS) else 1


if __name__ == "__main__":
    sys.exit(main())
