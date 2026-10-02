# Olli LLM mock test server — deterministic, reproducible OpenAI + Anthropic
# mock (via mockllm) for Olli's LLM feature tests, so tests do NOT depend on an
# external LLM. Adds a GET /v1/models route (mockllm lacks it) so Olli's model
# discovery + connection-check (which call listModels first) work.
#
# Base: python:3.14-alpine3.24 (verified: `pip install mockllm` + tiktoken work
# here; alpine is lighter than the glibc slim fallback). Build:
#   docker build -t ollitex/llm-mock:main tests/tools/llm-mock
ARG PYTHON_IMAGE=python:3.14-alpine3.24
FROM ${PYTHON_IMAGE}

# mockllm brings: fastapi, uvicorn, pydantic, pyyaml, click, watchdog, tiktoken,
# python-json-logger, httpx. curl = healthcheck.
RUN apk add --no-cache curl ca-certificates \
    && python -m pip install --no-cache-dir --upgrade pip \
    && pip install --no-cache-dir "mockllm"

WORKDIR /app
COPY app.py responses.yml ./
# mockllm ResponseConfig reads this at import + reloads if the file changes.
ENV MOCKLLM_RESPONSES_FILE=/app/responses.yml
# Deterministic model list exposed at GET /v1/models (comma-separated).
ENV MOCKLLM_MODELS=ollitex-small,ollitex-large

EXPOSE 8000
# In-container healthcheck on the discovery route (loopback here is fine — it's
# the container's own probe, not Olli's SSRF-guarded base URL).
HEALTHCHECK --interval=10s --timeout=5s --start-period=15s --retries=3 \
    CMD curl -sf http://127.0.0.1:8000/v1/models >/dev/null || exit 1

CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000"]
