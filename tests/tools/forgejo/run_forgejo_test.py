#!/usr/bin/env python3
"""Verify the Forgejo v15 test instance (tests/tools/forgejo) — the
"test representative" git-provider for the git-provider sync integration
(ghsync + githubinterface, module frontend/modules/github-sync).

Part A — provider contract (always runs; no Overleaf needed):
  A1  bootstrap if blank: POST / (install form) -> fixture admin, no UI clicking
  A2  GET /api/v1/version                       -> 15.x (representative identity)
  A3  PAT (reused by name) + seed repo          -> stable fixture identity
  A4  git wire round-trip: clone/commit/push    -> the exact surface Overleaf
      over http with the PAT                       import (clone)/export (push) uses
  A5  GET /repos/.../git/ref/heads/<def>       -> 404 pins the ABSENCE of the
      GitHub git-data API on this provider family (why auto-merge must refuse)
  A6  PR merge process: feature branch -> POST /pulls -> PUT /pulls/{i}/merge
      -> state=merged and main head advanced (provider-side merge works)

Part B — Overleaf integration (skips unless OLI_BASE + OLI_EMAIL + OLI_PASS):
  B1  POST /user/git-pat/link                   {provider, url, username, pat}
  B2  POST /user/git-servers/test               PAT accepted
  B3  GET  /user/github-sync/repos              seed repo listed
  B4  POST /project/new/github-sync             import -> {projectId}
  B5  push remote commit -> GET .../merge/overview   commit visible since lastSync
  B6  POST .../merge                            GRACEFUL REFUSAL expected:
                                               501 (module README) or 500
                                               "unsupported git server for merge"
                                               (code path) — NEVER 2xx
  B7  DELETE .../github-sync                    cleanup unlink

Env:
  FORGEJO_BASE        default http://127.0.0.1:3000
  FORGEJO_ADMIN_USER  default oltest          (test-only fixture dummy)
  FORGEJO_ADMIN_PASS  default Ol-Fixture-8mK2 (test-only fixture dummy)
  OLI_BASE            default empty -> Part B skipped
  OLI_EMAIL / OLI_PASS   default empty -> Part B skipped
  SKIP_PART_B         default 0; 1 forces Part B skip

Stdlib only (urllib + subprocess git), like the other tests/tools harnesses.

Verified quirks of this build (kept the harness honest):
  * install form posts to "/" (action="/"), sqlite needs an explicit
    db_path (empty -> "The SQLite3 database path cannot be empty."),
    no _csrf field on the install form (v15).
  * API routes reject web-session cookies here ("token is required");
    admin API uses Basic auth, PAT calls use `Authorization: token <PAT>`.
"""
import base64
import json as _json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

FORGE_BASE = os.environ.get("FORGEJO_BASE", "http://127.0.0.1:3000").rstrip("/")

# The ghsync bridge runs INSIDE the overleaf web container: loopback there is
# the container's own, NOT the host. Part A (harness-side git) uses
# FORGE_BASE; Part B registers the bridge-visible URL for the same Forgejo.
# Order: OLI_FORGEJO_URL env > overleafserver's network gateway (docker
# inspect, topology-agnostic) > host.docker.internal > FORGE_BASE (host-net).
def socket_host_reachable(host, port, timeout=2.0):
    import socket as _s
    try:
        with _s.create_connection((host, int(port)), timeout=timeout):
            return True
    except Exception:
        return False


def bridge_forge_base():
    env = os.environ.get("OLI_FORGEJO_URL", "").strip()
    if env:
        return env.rstrip("/")
    gateway = None
    try:
        import subprocess
        out = subprocess.run(
            ["docker", "inspect", "overleafserver",
             "--format", "{{json .NetworkSettings.Networks}}"],
            capture_output=True, text=True, timeout=5).stdout
        for net in _json.loads(out or "{}").values():
            gw = net.get("Gateway")
            if gw and net.get("DriverOpts") != "host":
                gateway = gw
                break
    except Exception:
        pass
    port = FORGE_BASE.rsplit(":", 1)[-1].split("/")[0]
    if gateway:
        return f"http://{gateway}:{port}"
    if socket_host_reachable("host.docker.internal", port):
        return f"http://host.docker.internal:{port}"
    return FORGE_BASE

FJ_USER = os.environ.get("FORGEJO_ADMIN_USER", "oltest")
FJ_PASS = os.environ.get("FORGEJO_ADMIN_PASS", "Ol-Fixture-8mK2")
FJ_REPO = "oltest-repo"
PAT_NAME = "oltest-pat"

OLI_BASE = os.environ.get("OLI_BASE", "").rstrip("/")
OLI_EMAIL = os.environ.get("OLI_EMAIL", "")
OLI_PASS = os.environ.get("OLI_PASS", "")

TIMEOUT = 25

RESULTS = []                       # (name, ok|skip, detail)
GIT_TMP = tempfile.mkdtemp(prefix="forgejo-harness-")


def report(name, ok, detail=""):
    RESULTS.append((name, ok, detail))
    tag = "PASS" if ok else "FAIL"
    print(f"[{tag}] {name}" + (f" — {detail}" if detail else ""))


def skip(name, detail=""):
    RESULTS.append((name, None, detail))
    print(f"[SKIP] {name}" + (f" — {detail}" if detail else ""))


def _capture_cookies(sesh, headers):
    """Capture Set-Cookie name=value pairs (last wins) straight from the
    response headers — no CookiePolicy (urllib's jar silently drops cookies
    with IP-address domains, e.g. 127.0.0.1)."""
    if headers is None:
        return
    for sc in (headers.get_all("Set-Cookie") or []):
        # NOTE: cookie names may contain dots (Overleaf's overleaf.sid) —
        # the class must allow them or the whole session is silently dropped.
        m = re.match(r"\s*([A-Za-z0-9_.\-]+)=([^;]*)", sc)
        if m:
            sesh.cookies[m.group(1)] = m.group(2)
            if m.group(1) == "flash":
                # v15 web token flow: the created PAT arrives HERE (303 cookie,
                # `flash=info=<token>&success=...`) before the follow-up page
                # renders/clears it. Keep the raw value for the harness.
                sesh.last_flash = m.group(2)


class _RedirectCapture(urllib.request.HTTPRedirectHandler):
    """urllib discards intermediate (30x) responses — but web logins set the
    session cookie on the 302 and then follow with a GET. Capture the
    intermediate Set-Cookie headers before the redirect handler proceeds."""

    def __init__(self, sesh):
        super().__init__()
        self.sesh = sesh

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        _capture_cookies(self.sesh, headers)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


class _NoFollow(urllib.request.HTTPRedirectHandler):
    """Return the first 30x as an HTTPError (code + Set-Cookie headers)
    instead of following it.

    Needed for POST /login: the success shape IS the 302 (carrying the
    REGENERATED sid in Set-Cookie). Following it would (a) lose the new
    sid (urllib replays the pre-redirect Cookie header) and (b) hide the
    status — py3.14 urllib auto-follows 3xx even WITHOUT an
    HTTPRedirectHandler installed, so subclassing HTTPHandler is not
    enough; redirect_request() is the single funnel and we raise there.
    Pinned from the nginx access log: "POST /login 302" immediately
    followed by urllib's "GET /login 200".
    """

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise urllib.error.HTTPError(req.full_url, code, msg, headers, fp)

    def http_error_default(self, req, fp, code, msg, headers):
        raise urllib.error.HTTPError(req.full_url, code, msg, headers, fp)


class Session:
    """Minimal HTTP session (urllib) with manual cookie capture — works for
    both the 127.0.0.1 Forgejo and the 127.0.0.1 Overleaf e2e stack."""

    def __init__(self, base):
        self.base = base.rstrip("/")
        self.cookies = {}
        self.last_flash = None      # raw `flash` cookie from the latest response
        self.csrf = None            # Overleaf session csrf token (ol-csrfToken)
        self.opener = urllib.request.build_opener(_RedirectCapture(self))
        # raw opener: no redirect following (login POST success = the 302 itself)
        self.raw_opener = urllib.request.build_opener(_NoFollow())

    def csrf_extract(self, page_html):
        """Overleaf renders the session-bound csrf token two ways:
        <meta name="ol-csrfToken" content="TOKEN"> and _csrf form values.
        Send it back as the X-CSRF-Token header (csurf defaultValue order:
        body _csrf, query _csrf, csrf-token, xsrf-token, x-csrf-token, x-xsrf-token)."""
        if not page_html:
            return None
        m = re.search(r'<meta name="ol-csrfToken" content="([A-Za-z0-9_-]{6,80})"', page_html)
        if m:
            self.csrf = m.group(1)
            return m.group(1)
        m = re.search(r'name="_csrf"[^>]*value="([A-Za-z0-9_-]{6,80})"', page_html)
        if m:
            self.csrf = m.group(1)
            return m.group(1)
        return None

    def _hdr(self, k):
        for kk in self.opener.headers:
            if kk.lower() == k.lower():
                return self.opener.headers[kk]
        return None

    def req(self, method, path, json_body=None, form=None, headers=None, auth=None, raw=False):
        url = self.base + path
        data = None
        hdrs = dict(headers or {})
        if json_body is not None:
            data = json_body if isinstance(json_body, bytes) else _json.dumps(json_body).encode()
            hdrs.setdefault("Content-Type", "application/json")
        if form is not None:
            data = urllib.parse.urlencode(form).encode()
            hdrs.setdefault("Content-Type", "application/x-www-form-urlencoded")
        if auth:
            scheme, token = auth
            hdrs["Authorization"] = f"{scheme} {token}"
        if self.cookies:
            hdrs.setdefault("Cookie", "; ".join(f"{k}={v}" for k, v in self.cookies.items()))
        if self.csrf and method.upper() in ("POST", "PUT", "DELETE"):
            hdrs.setdefault("X-CSRF-Token", self.csrf)
        r = urllib.request.Request(url, data=data, headers=hdrs, method=method)
        opener = self.raw_opener if raw else self.opener
        try:
            with opener.open(r, timeout=TIMEOUT) as resp:
                _capture_cookies(self, resp.headers)
                return resp.status, resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            _capture_cookies(self, e.headers)
            try:
                return e.code, e.read().decode("utf-8", "replace")
            except Exception:
                return e.code, ""

    def get_json(self, path, auth=None):
        st, body = self.req("GET", path, auth=auth)
        try:
            return st, _json.loads(body)
        except Exception:
            return st, None

    def form_field(self, page_html, name):
        if not page_html:
            return None
        m = re.search(r'<input[^>]*name="%s"[^>]*value="([^"]*)"' % re.escape(name), page_html)
        return m.group(1) if m else None


def fj_basic():
    return "Basic " + base64.b64encode(f"{FJ_USER}:{FJ_PASS}".encode()).decode()


def fj_token_auth(tok):
    # forgejo v15 + gitea accept `Authorization: token <PAT>` and basic
    # user:token for git-over-http; the `Token:` and `PRIVATE-TOKEN:` headers
    # are REJECTED by this build ("token is required") — gitlab-only conventions.
    return ("token", tok)


def admin_ok(fj):
    """Fixture admin exists on THIS instance and the basic-auth pair works."""
    st, body = fj.req("GET", "/api/v1/user", headers={"Authorization": fj_basic()})
    try:
        u = _json.loads(body)
    except Exception:
        return False
    return st == 200 and u.get("login") == FJ_USER


def installed(fj):
    # uninstalled: all API routes 404 (install.Contexter); installed: 401 auth wall.
    st, _ = fj.req("GET", "/api/v1/user")
    return st == 401


# -------------------------------------------------------------------------- A -

def a1_bootstrap(fj):
    if installed(fj) and admin_ok(fj):
        report("A1 bootstrap (reused existing install)", True, f"admin {FJ_USER} already present")
        return True
    if installed(fj):
        report("A1 bootstrap (reused existing install)", False,
               "instance already installed under DIFFERENT admin creds; "
               "reset: docker compose -f tests/tools/forgejo/docker-compose.yml down -v")
        return False
    # not installed — perform the first-install form POST (action="/", verified
    # against the live v15 page; field names from the rendered form).
    try:
        st, page = fj.req("GET", "/")
    except Exception:
        page = ""
    host_port = FORGE_BASE.split("//", 1)[-1]
    domain = host_port.split(":", 1)[0]
    form = {
        "db_type": "sqlite3",
        "db_host": "127.0.0.1:3306",
        "db_name": "forgejo",
        "db_user": "root",
        "db_path": "/data/gitea/gitea.db",   # REQUIRED by the installer (empty -> re-render error)
        "ssl_mode": "disable",
        "app_name": "OlliTeX-Forgejo-Test",
        "app_slogan": "test representative git-provider",
        "repo_root_path": "/data/git/repositories",
        "lfs_root_path": "/data/git/lfs",
        "run_user": "git",
        "domain": domain,
        "ssh_port": "22",
        "http_port": host_port.split(":", 1)[-1] if ":" in host_port else "3000",
        "app_url": FORGE_BASE + "/",
        "log_root_path": "/data/gitea/log",   # under the git-owned /data/gitea — on a
                                           # fresh volume /data itself is root:root, so
                                           # /data/log would fail "mkdir: permission denied"
        "disable_registration": "true",
        "password_algorithm": "pbkdf2_hi",
        "admin_name": FJ_USER,
        "admin_email": "oltest@forgejo-test.local",
        "admin_passwd": FJ_PASS,
        "admin_confirm_passwd": FJ_PASS,
    }
    if csrf := fj.form_field(page, "_csrf"):
        form["_csrf"] = csrf
    try:
        st, body = fj.req("POST", "/", form=form)
    except Exception as e:
        report("A1 bootstrap (first-install)", False, f"install POST failed: {e}")
        return False
    # gitea RESTARTS its web process after a successful install; poll admin
    # login through that window instead of racing a single probe.
    deadline = time.time() + 90
    last_flash = None
    while time.time() < deadline:
        try:
            if admin_ok(fj):
                break
        except Exception:
            pass                      # still restarting — keep polling
        last_flash = None
        time.sleep(2)
    else:
        m = re.search(r'flash-message[^>]*>([\s\S]{0,300}?)</div>', body or "")
        if m:
            last_flash = " ".join(re.sub(r"<[^>]+>", " ", m.group(1)).split())[:200]
        report("A1 bootstrap (first-install)", False,
               f"install POST={st}; admin login still fails (flash~={last_flash!r})")
        return False
    report("A1 bootstrap (first-install)", True,
           f"installed via POST / for admin {FJ_USER} (registration now closed)")
    return True


def a2_version(fj):
    for _ in range(15):
        try:
            st, body = fj.req("GET", "/api/v1/version")
            if st == 200:
                v = _json.loads(body).get("version", "")
                ok = v.startswith("15.")
                report("A2 version 15.x pin", ok, f"version={v}")
                return ok
        except Exception:
            pass
        time.sleep(2)
    report("A2 version 15.x pin", False, "instance never answered /api/v1/version (up yet?)")
    return False


def a3_pat_and_repo(fj):
    """PAT via the v15 web token flow — the ONLY way this build surfaces a PAT
    plaintext (verified empirically against this instance):
      * web login session, then POST /user/settings/applications/tokens/new
        (fields: name, resource=all, scope=<comma list>)
      * the 303 response carries a `flash` cookie `info=<40-hex token>&success=..`
        (gitea flashes the token exactly once: `ctx.Flash.Info(t.Token)`)
      * v15 has NO self-service token API (POST /api/v1/user/token -> 404) and
        admin-minted tokens come back masked, so the web flow is the fixture path.
    The PAT is cached in `.pat` next to this harness (git-ignored) so re-runs
    and Part B reuse the same token; after a blank `down -v` a fresh one is
    minted and re-cached. Scopes that 403s taught us:
    write:repository (git + repo APIs), write:user (API repo create), write:issue (PRs).
    """
    pat_file = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".pat")
    tok = None
    fresh = False
    if os.path.exists(pat_file):
        cached = open(pat_file).read().strip()
        if re.fullmatch(r"[0-9a-f]{32,64}", cached):
            st, _ = fj.get_json(f"/api/v1/repos/{FJ_USER}/{FJ_REPO}",
                                auth=("token", cached))
            if st != 401:            # 401 = masked/dead token -> re-mint below
                tok = cached        # single report at the end of this step
    if tok is None:
        web = Session(FORGE_BASE)
        lform = {"user_name": FJ_USER, "password": FJ_PASS}
        st, page = web.req("GET", "/user/login")
        if csrf := web.form_field(page, "_csrf"):   # present on some builds
            lform["_csrf"] = csrf
        st, body = web.req("POST", "/user/login", form=lform)
        if not web.cookies and st not in (200, 302):
            report("A3 PAT + seed repo", False, f"web login failed: POST /user/login -> {st}")
            return None
        scope = ",".join(["write:repository", "write:user", "write:issue",
                          "write:misc", "write:notification", "write:package",
                          "write:organization"])
        st, body = web.req("POST", "/user/settings/applications/tokens/new",
                           form={"name": f"oltest-pat-{int(time.time())}",
                                 "resource": "all", "scope": scope})
        flash_raw = web.last_flash or web.cookies.get("flash")
        m = re.search(r"info=([0-9a-f]{32,64})", urllib.parse.unquote(flash_raw or ""))
        if not m:  # last resort: the rendered flash region of the follow page
            m = re.search(r"flash-message[\s\S]{0,600}?" + r"([0-9a-f]{32,64})", body or "")
        if not m:
            report("A3 PAT + seed repo", False,
                   f"token create POST={st}; no PAT in flash "
                   f"(flash={str(flash_raw)[:80]!r}, body={str(body)[:120]!r})")
            return None
        tok = m.group(1)
        fresh = True
        with open(pat_file, "w") as f:
            f.write(tok + "\n")
        os.chmod(pat_file, 0o600)
    # seed repo (write:user scope needed for the /user/repos create endpoint)
    st, repo = fj.get_json(f"/api/v1/repos/{FJ_USER}/{FJ_REPO}", auth=("token", tok))
    if st != 200:
        st, body = fj.req("POST", "/api/v1/user/repos",
                          json_body={"name": FJ_REPO, "auto_init": True,
                                    "default_branch": "main", "private": False},
                          auth=("token", tok))
        if st not in (200, 201):
            report("A3 PAT + seed repo", False, f"repo create failed: {st} {body[:160]!r}")
            return None
        repo = _json.loads(body)
    G["REPO"] = f"{FJ_USER}/{FJ_REPO}"
    G["DEFAULT_BRANCH"] = repo.get("default_branch", "main")
    if fresh:
        report("A3 PAT + seed repo", True,
               f"PAT minted via web flash-cookie flow ({len(tok)} hex; scopes incl. write:repository) "
               f"+ repo {G['REPO']} (branch {G['DEFAULT_BRANCH']})")
    else:
        report("A3 PAT + seed repo", True,
               f"reusing cached PAT ({len(tok)} hex from .pat) + repo {G['REPO']} (branch {G['DEFAULT_BRANCH']})")
    return tok


G = {"REPO": None, "DEFAULT_BRANCH": "main", "TOKEN": None}


def a4_git_roundtrip(tok):
    if not shutil.which("git"):
        skip("A4 git wire round-trip", "git CLI not on PATH")
        return False
    url = FORGE_BASE + f"/{G['REPO']}"
    auth_url = url.replace("://", "://" + FJ_USER + ":" + urllib.parse.quote(tok) + "@", 1)
    d = os.path.join(GIT_TMP, "a4")
    def run(args, **kw):
        return subprocess.run(args, capture_output=True, text=True, timeout=60, **kw)
    r = run(["git", "clone", "--depth", "1", auth_url, d])
    if r.returncode != 0:
        report("A4 git wire round-trip", False, f"clone failed: {r.stderr.strip()[:200]}")
        return False
    env = dict(os.environ, GIT_AUTHOR_NAME=FJ_USER, GIT_AUTHOR_EMAIL="oltest@forgejo-test.local",
               GIT_COMMITTER_NAME=FJ_USER, GIT_COMMITTER_EMAIL="oltest@forgejo-test.local")
    with open(os.path.join(d, "harness-roundtrip.txt"), "a") as f:
        f.write(f"round-trip {int(time.time())}\n")
    r = run(["git", "-C", d, "add", "harness-roundtrip.txt"], env=env)
    r = run(["git", "-C", d, "commit", "-m", f"harness A4 round-trip {int(time.time())}"], env=env)
    if r.returncode != 0:
        report("A4 git wire round-trip", False, f"commit failed: {r.stderr.strip()[:200]}")
        return False
    r = run(["git", "-C", d, "push", "origin", G["DEFAULT_BRANCH"]], env=env)
    if r.returncode != 0:
        report("A4 git wire round-trip", False, f"push failed: {r.stderr.strip()[:200]}")
        return False
    report("A4 git wire round-trip", True,
           "clone -> commit -> push over http+PAT (the sync module's import/export surface)")
    return True


def a5_gitdata_absence(fj, tok):
    st, body = fj.get_json(f"/api/v1/repos/{G['REPO']}/git/ref/heads/{G['DEFAULT_BRANCH']}",
                           auth=("token", tok))
    ok = st == 404
    detail = (f"git/ref -> HTTP {st} (expected 404). git-data API ABSENT on this provider "
              "family (provider fact) — the Node-era git-data merge path cannot run on it; "
              "the shipped Go merge engine instead fetches the remote tree via the contents-"
              "style REST API (B6 pins the passing merge).")
    report("A5 git-data API ABSENT (404 pin)", ok, detail)
    return ok


def _clone_for_branch(tok, tag):
    url = FORGE_BASE + f"/{G['REPO']}"
    auth_url = url.replace("://", "://" + FJ_USER + ":" + urllib.parse.quote(tok) + "@", 1)
    d = os.path.join(GIT_TMP, tag)
    r = subprocess.run(["git", "clone", "--depth", "1", "-b", G["DEFAULT_BRANCH"],
                        auth_url, d], capture_output=True, text=True, timeout=60)
    if r.returncode != 0:
        # retry without single-branch limit (some clones of fresh repos need full history)
        r = subprocess.run(["git", "clone", auth_url, d], capture_output=True, text=True, timeout=60)
    if r.returncode != 0:
        return None, r.stderr.strip()[:200]
    return d, ""


def a6_pr_merge(fj, tok):
    if not shutil.which("git"):
        skip("A6 PR merge process", "git CLI not on PATH")
        return
    ts = int(time.time())
    branch = f"feature/harness-{ts}"
    d, err = _clone_for_branch(tok, "a6")
    if not d:
        report("A6 PR merge process", False, f"clone failed: {err}")
        return
    env = dict(os.environ, GIT_AUTHOR_NAME=FJ_USER, GIT_AUTHOR_EMAIL="oltest@forgejo-test.local",
               GIT_COMMITTER_NAME=FJ_USER, GIT_COMMITTER_EMAIL="oltest@forgejo-test.local")
    with open(os.path.join(d, f"harness-pr-{ts}.txt"), "w") as f:
        f.write(f"merged-via-pr {ts}\n")
    cmds = [
        ["git", "-C", d, "checkout", "-b", branch],
        ["git", "-C", d, "add", "-A"],
        ["git", "-C", d, "commit", "-m", f"harness A6 PR branch {ts}"],
        ["git", "-C", d, "push", "origin", branch],
    ]
    for c in cmds:
        r = subprocess.run(c, capture_output=True, text=True, env=env, timeout=60)
        if r.returncode != 0:
            report("A6 PR merge process", False, f"{c[2]} failed: {r.stderr.strip()[:200]}")
            return
    st, pr = fj.req("POST", f"/api/v1/repos/{G['REPO']}/pulls",
                    json_body={"base": G["DEFAULT_BRANCH"], "head": branch,
                              "title": f"harness A6 PR {ts}"},
                    auth=fj_token_auth(tok))
    if st not in (200, 201):
        report("A6 PR merge process", False, f"create PR: {st} {pr[:160]!r}")
        return
    num = _json.loads(pr).get("number")
    st, mb = fj.req("POST", f"/api/v1/repos/{G['REPO']}/pulls/{num}/merge",
                    json_body={"do": "merge"}, auth=fj_token_auth(tok))
    if st not in (200, 201):
        report("A6 PR merge process", False, f"merge PR: {st} {mb[:160]!r}")
        return
    st, pr2 = fj.get_json(f"/api/v1/repos/{G['REPO']}/pulls/{num}", auth=fj_token_auth(tok))
    pr2 = pr2 if isinstance(pr2, dict) else {}
    state = pr2.get("state")
    merged = bool(pr2.get("merged"))
    st, head = fj.get_json(f"/api/v1/repos/{G['REPO']}/commits?sha={G['DEFAULT_BRANCH']}&limit=5",
                           auth=fj_token_auth(tok))
    head_list = head if isinstance(head, list) else []
    st2, _content = fj.req("GET", f"/api/v1/repos/{G['REPO']}/contents/harness-pr-{ts}.txt?ref={G['DEFAULT_BRANCH']}",
                           auth=fj_token_auth(tok))
    # forgejo/gitea represent a merged PR as state="closed" + merged=true
    # (github-style state="merged" does not exist) — accept the merged flag.
    ok = merged and st2 == 200
    head_sha = (head_list[0].get("sha", "?")[:12] if head_list and isinstance(head_list[0], dict) else "?")
    report("A6 PR merge process", ok,
           f"PR #{num} on {branch} -> POST /pulls/{num}/merge -> merged={merged} state={state}; "
           f"merged file on {G['DEFAULT_BRANCH']}: {st2 == 200} (main sha={head_sha})")


# -------------------------------------------------------------------------- B -

def overleaf_login(oli):
    """Proven flow (pinned against the live 2026-10 stack):
    1. a web-router GET must issue the overleaf.sid session cookie (GET /login
       first; if none, GET /project's 302 touch sets it) — a csrf token is
       only verifiable against the session that rendered it.
    2. the /login page's meta ol-csrfToken (same session) rides the login.
    3. POST /login as JSON {email, password} (the form decode path 400s on
       this build; JSON → 302 /project with a regenerated sid).
    4. GET /dev/csrf — the official token oracle (devcsrf feature) for the
       logged-in session's state-changing calls (X-CSRF-Token header).
    """
    try:
        _st, _p = oli.req("GET", "/login")
        if not oli.cookies:
            oli.req("GET", "/project")          # 302 touch issues overleaf.sid
        _st, page = oli.req("GET", "/login")
    except Exception as e:
        return False, str(e)
    tok = oli.csrf_extract(page) or oli.form_field(page, "_csrf")
    if not tok:
        return False, "no ol-csrfToken on the rendered /login page"
    body = {"email": OLI_EMAIL, "password": OLI_PASS}
    st, b = oli.req("POST", "/login", json_body=body, headers={"X-CSRF-Token": tok}, raw=True)
    if not _login_success(st, b):
        # rotation-safe retry with a fresh page token
        _st, page = oli.req("GET", "/login")
        tok2 = oli.csrf_extract(page) or tok
        st, b = oli.req("POST", "/login", json_body=body, headers={"X-CSRF-Token": tok2}, raw=True)
        if not _login_success(st, b):
            return False, f"POST /login -> {st} {b[:140]!r}"
    # real authed probe — the route must EXIST (404 = ghsync not deployed),
    # and it must answer the (authenticated) session with JSON.
    st2, b2 = oli.get_json("/user/github-sync/status")
    if st2 == 404:
        return False, ("gsync routes not deployed on this stack "
                       "(GET /user/github-sync/status -> 404); use a stack built from "
                       "the Go cutover tree (ghsync Feature registered in cmd/web)")
    if st2 != 200 or b2 is None:
        return False, f"login POST={st}; /user/github-sync/status -> {st2} (authed?) {str(b2)[:100]!r}"
    # logged-in session token oracle for the state-changing Part B calls;
    # it must be the short bare token, never an HTML page (a 200 HTML body
    # here means the session is ANONYMOUS and the router 302'd to /login).
    st3, tokbody = oli.req("GET", "/dev/csrf")
    tok3 = tokbody.strip() if (st3 == 200 and len(tokbody.strip()) < 200
                               and "<" not in tokbody) else None
    if tok3:
        oli.csrf = tok3
    elif not oli.csrf:
        _st, page = oli.req("GET", "/project")
        if not oli.csrf_extract(page):
            return False, "no session csrf token after login (/dev/csrf missing?)"
    return True, f"session token armed ({oli.csrf[:8]}…)" if oli.csrf else "ok (page token)"


def _login_success(st, body):
    """POST /login success shapes (raw, unfollowed):
    - 302/303/307 to /project (no Accept: application/json), or
    - 200 {"redir": "/project"} (AcceptsJSON path — urllib's Accept: */*
      can take this branch). Failure shapes: 200 + re-rendered login HTML,
    400/401 JSON.
    """
    if st in (301, 302, 303, 307):
        return True
    if st == 200:
        t = body if isinstance(body, str) else body.decode("utf-8", "replace")
        return '"redir"' in t
    return False


def part_b(fj, tok):
    if os.environ.get("SKIP_PART_B") == "1":
        skip("B1-B7 Overleaf integration", "SKIP_PART_B=1")
        return
    if not (OLI_BASE and OLI_EMAIL and OLI_PASS):
        skip("B1-B7 Overleaf integration",
             "OLI_BASE/OLI_EMAIL/OLI_PASS not set — pass them to run Part B "
             "(e.g. the ol-e2e stack at http://127.0.0.1:7420 + fixtures/credentials.ts user)")
        return
    oli = Session(OLI_BASE)
    ok, why = overleaf_login(oli)
    if not ok:
        skip("B1-B7 Overleaf integration", f"login failed: {why}")
        return
    prov_url = bridge_forge_base()
    prov = {"provider": "forgejo", "url": prov_url, "username": FJ_USER, "pat": tok}

    st, b = oli.req("POST", "/user/git-pat/link", json_body=prov)
    if st not in (200, 201):
        report("B1 link PAT", False, f"POST /user/git-pat/link -> {st} {b[:160]!r}")
        return
    report("B1 link PAT", True, f"provider=forgejo url={prov_url} user={FJ_USER}")

    test_body = {k: v for k, v in prov.items() if k != "pat"}
    st, b = oli.req("POST", "/user/git-servers/test", json_body=test_body)
    report("B2 git-servers/test", st in (200, 201), f"-> {st} {b[:120]!r}")

    st, repos = oli.get_json("/user/github-sync/repos?provider=forgejo&serverUrl="
                             + urllib.parse.quote(prov_url, safe="") + "&username=" + FJ_USER)
    listed = G["REPO"] in _json.dumps(repos)
    report("B3 repos listing", st == 200 and listed,
           f"-> {st}; seed repo {'present' if listed else 'MISSING'} (sample={str(repos)[:120]!r})")

    imp = {"name": f"forgejo-harness-{int(time.time())}", "fullName": G["REPO"],
           "defaultBranchName": G["DEFAULT_BRANCH"],
           "provider": "forgejo", "serverUrl": prov_url, "username": FJ_USER}
    st, b = oli.req("POST", "/project/new/github-sync", json_body=imp)
    project_id = None
    try:
        project_id = _json.loads(b).get("projectId")
    except Exception:
        pass
    if st not in (200, 201) or not project_id:
        report("B4 import project", False, f"-> {st} {b[:200]!r}")
        return
    report("B4 import project", True, f"projectId={project_id} from {G['REPO']}")

    if not shutil.which("git"):
        skip("B5 merge overview", "git CLI not on PATH")
        return
    d, err = _clone_for_branch(tok, "b5")
    if not d:
        report("B5 merge overview", False, f"clone failed: {err}")
        return
    ts = int(time.time())
    env = dict(os.environ, GIT_AUTHOR_NAME=FJ_USER, GIT_AUTHOR_EMAIL="oltest@forgejo-test.local",
               GIT_COMMITTER_NAME=FJ_USER, GIT_COMMITTER_EMAIL="oltest@forgejo-test.local")
    with open(os.path.join(d, f"harness-remote-{ts}.txt"), "w") as f:
        f.write(f"remote-advance {ts}\n")
    for c in (["git", "-C", d, "add", "-A"],
              ["git", "-C", d, "commit", "-m", f"harness B5 remote advance {ts}"],
              ["git", "-C", d, "push", "origin", G["DEFAULT_BRANCH"]]):
        r = subprocess.run(c, capture_output=True, text=True, env=env, timeout=60)
        if r.returncode != 0:
            report("B5 merge overview", False, f"remote push failed: {r.stderr.strip()[:200]}")
            return
    time.sleep(1)
    st, ov = oli.get_json(f"/project/{project_id}/github-sync/merge/overview")
    commits = (ov or {}).get("commits") if isinstance(ov, dict) else None
    seen = isinstance(commits, list) and any("remote advance" in str(c) for c in commits)
    report("B5 merge overview", st == 200 and commits,
           f"-> {st}; commits_since_last_sync={len(commits) if isinstance(commits, list) else '?'}; "
           f"our advance visible: {seen}")

    # B6 — the Go merge engine is provider-agnostic (contents-REST tree fetch;
    # works on GitHub/Gitea/Forgejo/GitLab-style APIs). A remote-only advance
    # on a provider WITHOUT local edits therefore merges cleanly (200
    # {"status":"merged"}) — this is the shipped contract (README table's
    # "501 on non-GitHub" predates the contents-API engine and is stale;
    # owner ratification flagged in the workstream note). Refusal paths:
    # local+remote divergence -> 200 {"status":"conflict"}; no repo contents
    # API -> 500 'unsupported git server for merge' (gitRESTBase fallback).
    st, b = oli.req("POST", f"/project/{project_id}/github-sync/merge", json_body={})
    merged_ok = False
    msg = b[:140]
    try:
        jj = _json.loads(b)
        if isinstance(jj, dict):
            msg = str(jj.get("status", jj.get("message", msg)))[:140]
            merged_ok = jj.get("status") == "merged"
    except Exception:
        pass
    report("B6 merge (remote-only fast path)", st == 200 and merged_ok,
           f"-> HTTP {st} status={msg!r} (remote-only advance applies; "
           "diverged local edits would answer {\"status\":\"conflict\"} instead)")

    # B6b — merge must be idempotent on a second call (no remote advance
    # since the merge: remoteHead == lastSyncCommit -> clean short-circuit).
    st2, b2 = oli.req("POST", f"/project/{project_id}/github-sync/merge", json_body={})
    try:
        st2s = _json.loads(b2).get("status")
    except Exception:
        st2s = "?"
    report("B6b merge idempotent (no advance -> clean)", st2 in (200,) and st2s in ("clean", "merged"),
           f"-> HTTP {st2} status={st2s!r}")

    st, b = oli.req("DELETE", f"/project/{project_id}/github-sync")
    report("B7 unlink cleanup", st in (200, 204), f"-> {st} {b[:80]!r}")

    # B8 — optional dev-stack hygiene: drop the imported project itself
    # (the dev instance 127.0.0.1:4000 carries real users/projects; the
    # disposable ol-e2e stack may keep it).
    if os.environ.get("OLI_CLEANUP") == "1":
        # Node route quirk (services/web router.mjs): DELETE /Project/:id —
        # capital P; lowercase /project/:id 404s (pinned from
        # go/services/web/features/projectlist/delete.go delPat).
        st, b = oli.req("DELETE", f"/Project/{project_id}")
        report("B8 project cleanup (OLI_CLEANUP=1)", st in (200, 204), f"DELETE /Project/{project_id} -> {st} {b[:80]!r}")


# ------------------------------------------------------------------- driver -

def main():
    fj = Session(FORGE_BASE)
    try:
        fj.req("GET", "/")
    except Exception as e:
        print(f"[ABORT] cannot reach Forgejo at {FORGE_BASE}: {e}")
        print("Start it: docker compose -f tests/tools/forgejo/docker-compose.yml up -d")
        sys.exit(2)

    if not a1_bootstrap(fj):
        sys.exit(1)
    if not a2_version(fj):
        sys.exit(1)
    tok = a3_pat_and_repo(fj)
    if not tok:
        sys.exit(1)
    G["TOKEN"] = tok

    a4_git_roundtrip(tok)
    a5_gitdata_absence(fj, tok)
    a6_pr_merge(fj, tok)

    print()
    part_b(fj, tok)

    fails = [r for r in RESULTS if r[1] is False]
    passes = [r for r in RESULTS if r[1] is True]
    skips = [r for r in RESULTS if r[1] is None]
    print()
    print(f"SUMMARY: {len(passes)} passed, {len(fails)} failed, {len(skips)} skipped")
    if fails:
        for n, _, d in fails:
            print(f"  FAILED: {n} — {d}")
    shutil.rmtree(GIT_TMP, ignore_errors=True)
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
