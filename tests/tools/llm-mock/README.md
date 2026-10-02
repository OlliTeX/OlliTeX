# Olli LLM mock test server (deterministic, reproducible)

A **deterministic, reproducible** LLM test server for Olli's LLM feature, so LLM
tests do **not** depend on an external model or API. Built on
[`mockllm`](https://pypi.org/project/mockllm/) (OpenAI + Anthropic format) plus a
thin adapter that adds the `GET /v1/models` route mockllm lacks (Olli's
`listModels` for connection-check / model-scan calls it first).

```
Olli LLM client ──►  http://<host>:8600/v1/models           (discovery)
                     http://<host>:8600/v1/chat/completions  (openai / openaiCompatible)
                     http://<host>:8600/v1/messages           (anthropic)
```

Deterministic: mockllm is **exact-key-match** on the last user message; anything
unmapped returns `defaults.unknown_response` from `responses.yml`. Both are pinned,
so every response is stable across runs (no network, no model non-determinism).

## Run

```bash
cd tests/tools/llm-mock
docker compose up -d --build      # ollitex/llm-mock on host :8600 (container :8000)
python3 run_llm_mock_test.py       # verify OpenAI + Anthropic + /v1/models deterministic
docker compose down                 # stop
```

## Pointing Olli's LLM at it

Olli's LLM admin config uses `LLM_API_URL` / `llmApiUrl` (base URL) + a model name.
Set (e.g. in the Olli admin LLM settings or via env for a test):

* Provider: `openaiCompatible`
* Base URL: `http://<THIS-HOST-LAN-IP>:8600/v1`
* Model:    `ollitex-small`

> ⚠️ **SSRF guard**: Olli's `assertPublicLlmBaseUrl` **blocks loopback**
> (`localhost` / `127.x` / `::1` / `.local`/`.lan`/`.internal`). A **LAN IP** (e.g.
> the host's own `192.168.x.x` / `10.x.x.x` / Docker-bridge `172.x.x.x`) is allowed.
> So use the host's LAN IP, not `localhost`.
>   * find it: `hostname -I` (first routable IP) or the container's bridge IP.

`run_llm_mock_test.py` already uses a loopback URL for the standalone verification
(it talks to the mock via the host port `:8600`), which is fine for the harness; the
loopback restriction only matters for Olli's OWN outbound LLM calls.

## Files

| File | Purpose |
|------|---------|
| `Dockerfile` | `python:3.14-alpine3.24` + `pip install mockllm` + adapter + responses. |
| `app.py` | Thin adapter: mockllm's OpenAI + Anthropic endpoints + `GET /v1/models`. |
| `responses.yml` | Pinned deterministic responses (exact-key map + default). |
| `docker-compose.yml` | Service on host `:8600`, healthcheck. |
| `run_llm_mock_test.py` | Reproducible verification harness (stdlib, no deps). |

## Extending

* **Add a mapped prompt**: add a key under `responses:` in `responses.yml` (exact
  text of the last user message Olli sends → the deterministic reply). `settings:
  lag_enabled: false` keeps it fast.
* **Change the model list**: set `MOCKLLM_MODELS=ollitex-small,ollitex-large` (env) —
  exposed at `/v1/models`.
