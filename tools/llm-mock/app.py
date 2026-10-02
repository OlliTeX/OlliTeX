"""Olli LLM mock adapter — mockllm's OpenAI + Anthropic endpoints, plus a
GET /v1/models route (mockllm lacks it) so Olli's `listModels` (connection-check
+ model-scan) works. Deterministic: responses are exact-key-matched from
responses.yml (unmatched → defaults.unknown_response).

Run: uvicorn app:app --host 0.0.0.0 --port 8000
"""
import os

# Must be set BEFORE importing mockllm.server (ResponseConfig reads it at
# import and load_responses() requires the file to exist).
_HERE = os.path.dirname(os.path.abspath(__file__))
os.environ.setdefault("MOCKLLM_RESPONSES_FILE", os.path.join(_HERE, "responses.yml"))

from mockllm.server import app  # FastAPI: POST /v1/chat/completions + /v1/messages


def _models():
    return [m.strip() for m in os.environ.get("MOCKLLM_MODELS", "ollitex-small,ollitex-large").split(",") if m.strip()]


# mockllm does NOT expose a models list; Olli calls GET <base>/models (or
# /v1/models for anthropic) before chat. Serve the deterministic list.
@app.get("/v1/models")
def list_models():
    return {
        "object": "list",
        "data": [{"id": m, "object": "model", "owned_by": "mockllm"} for m in _models()],
    }


@app.get("/healthz")
def healthz():
    return {"status": "ok", "models": _models()}
