import json
import os
from datetime import UTC, datetime
from importlib.metadata import distribution
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import HTTPRedirectHandler, Request, build_opener

from sefir_agent.providers import PROVIDERS, valid_model_id


class ModelDiscoveryError(Exception):
    pass


class NoRedirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


ENDPOINTS = {
    "openai": "https://api.openai.com/v1/models",
    "anthropic": "https://api.anthropic.com/v1/models",
    "cohere": "https://api.cohere.com/v1/models",
    "openrouter": "https://openrouter.ai/api/v1/models/user",
}
MAX_RESPONSE_BYTES = 2_097_152


def openai_chat_models() -> set[str]:
    path = distribution("litellm").locate_file(
        "litellm/model_prices_and_context_window_backup.json"
    )
    try:
        catalogue = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise ModelDiscoveryError("models_unavailable") from exc
    return {
        model.removeprefix("openai/")
        for model, details in catalogue.items()
        if isinstance(details, dict)
        and details.get("litellm_provider") == "openai"
        and details.get("mode") == "chat"
        and not any(
            feature in model
            for feature in (
                "audio",
                "realtime",
                "search",
                "computer",
                "cyber",
                "daybreak",
                "research",
            )
        )
    }


def fetch_page(opener, request: Request) -> dict:
    try:
        with opener.open(request, timeout=8) as response:
            body = response.read(MAX_RESPONSE_BYTES + 1)
    except HTTPError as exc:
        exc.close()
        if exc.code in {401, 403, 498}:
            raise ModelDiscoveryError("models_auth_failed") from None
        if exc.code == 429:
            raise ModelDiscoveryError("models_rate_limited") from None
        raise ModelDiscoveryError("models_unavailable") from None
    except (OSError, URLError) as exc:
        raise ModelDiscoveryError("models_unavailable") from exc
    if len(body) > MAX_RESPONSE_BYTES:
        raise ModelDiscoveryError("models_invalid_response")
    try:
        page = json.loads(body)
    except (ValueError, UnicodeError) as exc:
        raise ModelDiscoveryError("models_invalid_response") from exc
    if not isinstance(page, dict):
        raise ModelDiscoveryError("models_invalid_response")
    return page


def list_models(provider_id: str) -> dict:
    provider = next((item for item in PROVIDERS if item.id == provider_id), None)
    if provider is None or not provider.configured():
        raise ModelDiscoveryError("provider_unavailable")
    key = os.environ[provider.key_variable].strip()
    headers = {"Authorization": f"Bearer {key}", "Accept": "application/json"}
    params = {}
    if provider_id == "cohere":
        params = {"endpoint": "chat", "page_size": "1000"}
    elif provider_id == "anthropic":
        params = {"limit": "1000"}
        headers["anthropic-version"] = "2026-10-21"
    elif provider_id == "openrouter":
        params = {"limit": "100", "offset": "0", "output_modalities": "text"}
    supported = openai_chat_models() if provider_id == "openai" else set()
    today = datetime.now(UTC).date()
    models = {}
    cursors = set()
    opener = build_opener(NoRedirects())
    for _ in range(20 if provider_id == "openrouter" else 10):
        url = ENDPOINTS[provider_id]
        if params:
            url += "?" + urlencode(params)
        page = fetch_page(opener, Request(url, headers=headers))
        entries = page.get("models" if provider_id == "cohere" else "data")
        if not isinstance(entries, list):
            raise ModelDiscoveryError("models_invalid_response")
        for entry in entries:
            if not isinstance(entry, dict):
                raise ModelDiscoveryError("models_invalid_response")
            model_id = entry.get("name" if provider_id == "cohere" else "id")
            if not valid_model_id(provider_id, model_id):
                continue
            if provider_id == "cohere":
                if entry.get("is_deprecated") or "chat" not in (
                    entry.get("endpoints") or []
                ):
                    continue
            elif provider_id == "anthropic":
                if entry.get("lifecycle") in {"deprecated", "retired"}:
                    continue
            elif provider_id == "openrouter":
                architecture = entry.get("architecture")
                if not isinstance(architecture, dict):
                    continue
                inputs = architecture.get("input_modalities")
                if not isinstance(inputs, list) or "text" not in inputs:
                    continue
                if architecture.get("output_modalities") != ["text"]:
                    continue
                expiration = entry.get("expiration_date")
                if expiration is not None:
                    try:
                        if datetime.strptime(expiration, "%Y-%m-%d").date() <= today:
                            continue
                    except (ValueError, TypeError):
                        continue
            else:
                if model_id not in supported:
                    continue
                shutdown = entry.get("shutdown_date")
                if shutdown is not None:
                    try:
                        if datetime.strptime(shutdown, "%Y-%m-%d").date() <= today:
                            continue
                    except (ValueError, TypeError):
                        continue
            name = entry.get(
                "name" if provider_id == "openrouter" else "display_name", model_id
            )
            if (
                not isinstance(name, str)
                or not name.isprintable()
                or not 1 <= len(name) <= 160
            ):
                name = model_id
            models[model_id] = {"id": model_id, "name": name}
        if provider_id == "openrouter" and len(entries) == 100:
            params["offset"] = str(int(params["offset"]) + 100)
            continue
        cursor = None
        if provider_id == "cohere":
            cursor = page.get("next_page_token")
            parameter = "page_token"
        elif provider_id == "anthropic" and page.get("has_more"):
            cursor = page.get("last_id")
            if not cursor:
                raise ModelDiscoveryError("models_invalid_response")
            parameter = "after_id"
        if not cursor:
            return {
                "models": sorted(models.values(), key=lambda item: item["name"].lower())
            }
        if not isinstance(cursor, str) or len(cursor) > 4096 or cursor in cursors:
            raise ModelDiscoveryError("models_invalid_response")
        cursors.add(cursor)
        params[parameter] = cursor
    raise ModelDiscoveryError("models_invalid_response")
