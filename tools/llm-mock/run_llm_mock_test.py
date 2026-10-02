#!/usr/bin/env python3
"""Verify the Olli LLM mock test server (tools/llm-mock) — deterministic +
reproducible responses on the exact surfaces Olli calls:

  * GET  <base>/v1/models           (Olli listModels discovery)
  * POST <base>/v1/chat/completions (openai / openaiCompatible)
  * POST <base>/v1/messages         (anthropic)

No external LLM is used. Assertions pin the EXACT deterministic response text
(from responses.yml), so the check is reproducible.

Env: LLM_MOCK_BASE (default http://127.0.0.1:8600)
"""
import json
import os
import ssl
import sys
import urllib.error
import urllib.request

BASE = os.environ.get("LLM_MOCK_BASE", "http://127.0.0.1:8600").rstrip("/")
CTX = ssl.create_default_context()
CTX.check_hostname = False
CTX.verify_mode = ssl.CERT_NONE

DEFAULT_RESPONSE = (
    "This is a deterministic response from the Olli mockLLM test server "
    "(no external model was used)."
)
OK_PROMPT = "Reply with the single word OK."  # Olli's admin chatProbe prompt

OA_HEADERS = {
    "accept": "application/json",
    "content-type": "application/json",
    "user-agent": "overleaf-llm-module",
    "authorization": "Bearer sk-mock-test",
}
ANT_HEADERS = {
    "accept": "application/json",
    "content-type": "application/json",
    "user-agent": "overleaf-llm-module",
    "anthropic-version": "2023-06-01",
    "x-api-key": "overleaf-local",
}

FAILS = []


def check(label, ok, got=None, want=None):
    print(f"  [{'OK' if ok else 'FAIL'}] {label}" + ("" if ok else f"   got={got!r} want={want!r}"))
    if not ok:
        FAILS.append(label)


def call(method, path, payload, headers):
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(BASE + path, data=data, method=method)
    for k, v in headers.items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, context=CTX if BASE.startswith("https") else None, timeout=20) as r:
            return r.status, json.loads(r.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")
    except Exception as e:  # noqa: BLE001
        return -1, f"error: {e}"


def pick(content):
    # OpenAI: choices[0].message.content ; Anthropic: content[0].text
    if isinstance(content, dict):
        c = content.get("choices")
        if c and isinstance(c, list) and isinstance(c[0], dict):
            m = c[0].get("message")
            if isinstance(m, dict):
                return m.get("content")
        ct = content.get("content")
        if ct and isinstance(ct, list) and isinstance(ct[0], dict):
            return ct[0].get("text")
    return None


def main() -> int:
    # 1) discovery (listModels)
    st, j = call("GET", "/v1/models", None, {"accept": "application/json"})
    print(f"== GET /v1/models -> HTTP {st} (Olli listModels discovery) ==")
    data = j.get("data") if isinstance(j, dict) else None
    ids = [ (d.get("id") if isinstance(d, dict) else d) for d in (data or []) ]
    check("returns 200", st == 200, st, 200)
    check("lists ollitex-small", "ollitex-small" in ids, ids)

    # 2) OpenAI — pinned prompt ("Reply ... OK.")
    st, j = call("POST", "/v1/chat/completions",
                 {"model": "ollitex-small", "max_tokens": 16, "temperature": 0, "stream": False,
                  "messages": [{"role": "user", "content": OK_PROMPT}]},
                 OA_HEADERS)
    print(f"== POST /v1/chat/completions (openai, pinned) -> HTTP {st} ==")
    content = pick(j)
    check("returns 200", st == 200, st, 200)
    check("deterministic content == 'OK'", content == "OK", content, "OK")

    # 3) OpenAI — unmapped prompt -> deterministic default
    st, j = call("POST", "/v1/chat/completions",
                 {"model": "ollitex-small", "max_tokens": 16, "temperature": 0, "stream": False,
                  "messages": [{"role": "user", "content": "a prompt with no pinned response"}]},
                 OA_HEADERS)
    print(f"== POST /v1/chat/completions (openai, unmapped -> default) -> HTTP {st} ==")
    content = pick(j)
    check("returns 200", st == 200, st, 200)
    check("deterministic default response", content == DEFAULT_RESPONSE, (content or "")[:50], DEFAULT_RESPONSE[:50])

    # 4) Anthropic — pinned prompt
    st, j = call("POST", "/v1/messages",
                 {"model": "ollitex-small", "max_tokens": 16,
                  "messages": [{"role": "user", "content": OK_PROMPT}]},
                 ANT_HEADERS)
    print(f"== POST /v1/messages (anthropic, pinned) -> HTTP {st} ==")
    text = pick(j)
    check("returns 200", st == 200, st, 200)
    check("deterministic content[0].text == 'OK'", text == "OK", text, "OK")

    # 5) reproducibility: the pinned prompt must return 'OK' again on a 2nd call
    st, j = call("POST", "/v1/chat/completions",
                 {"model": "ollitex-small", "max_tokens": 16, "temperature": 0, "stream": False,
                  "messages": [{"role": "user", "content": OK_PROMPT}]},
                 OA_HEADERS)
    print("== reproducibility (2nd call, same prompt) ==")
    check("still deterministic (== 'OK')", (st == 200) and (pick(j) == "OK"), pick(j), "OK")

    print("\n" + ("RESULT: all checks passed" if not FAILS else f"RESULT: {len(FAILS)} FAILURES: {FAILS}"))
    return 0 if not FAILS else 1


if __name__ == "__main__":
    sys.exit(main())
