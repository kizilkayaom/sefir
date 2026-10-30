import os
import re
from dataclasses import dataclass


class ProviderConfigurationError(ValueError):
    pass


def valid_model_id(provider_id: str, model: object) -> bool:
    if not isinstance(model, str):
        return False
    if provider_id == "openrouter":
        return len(model) <= 256 and bool(
            re.fullmatch(
                r"[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*(?::[A-Za-z0-9][A-Za-z0-9._-]*)?",
                model,
            )
        )
    return bool(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}", model))


@dataclass(frozen=True)
class Provider:
    id: str
    name: str
    prefix: str
    default_model: str
    key_variable: str
    model_variable: str

    def configured(self) -> bool:
        return bool(os.environ.get(self.key_variable, "").strip())

    def model(self) -> str:
        model = os.environ.get(self.model_variable, "").strip() or self.default_model
        if not valid_model_id(self.id, model):
            if self.id == "openrouter":
                raise ProviderConfigurationError(
                    f"{self.model_variable} must be an OpenRouter publisher/model ID"
                )
            raise ProviderConfigurationError(
                f"{self.model_variable} must be a model ID without a provider prefix"
            )
        return model


PROVIDERS = (
    Provider(
        "cohere",
        "Cohere",
        "cohere_chat",
        "command-a-plus-05-2026",
        "COHERE_API_KEY",
        "SEFIR_COHERE_MODEL",
    ),
    Provider(
        "anthropic",
        "Anthropic (Claude)",
        "anthropic",
        "claude-sonnet-5-5",
        "ANTHROPIC_API_KEY",
        "SEFIR_ANTHROPIC_MODEL",
    ),
    Provider(
        "openai",
        "OpenAI (GPT)",
        "openai",
        "gpt-6-luna",
        "OPENAI_API_KEY",
        "SEFIR_OPENAI_MODEL",
    ),
    Provider(
        "openrouter",
        "OpenRouter",
        "openrouter",
        "openai/gpt-6-luna",
        "OPENROUTER_API_KEY",
        "SEFIR_OPENROUTER_MODEL",
    ),
)


def resolve_provider(provider_id: str | None = None) -> Provider:
    if provider_id is None or provider_id == "":
        provider_id = os.environ.get("SEFIR_DEFAULT_PROVIDER", "").strip()
        if not provider_id:
            provider_id = next(
                (provider.id for provider in PROVIDERS if provider.configured()), ""
            )
    if not isinstance(provider_id, str):
        raise ProviderConfigurationError("Choose a supported AI provider")
    provider = next(
        (provider for provider in PROVIDERS if provider.id == provider_id), None
    )
    if provider is None:
        raise ProviderConfigurationError("Choose a configured AI provider")
    if not provider.configured():
        raise ProviderConfigurationError(f"Set {provider.key_variable} on the server")
    provider.model()
    return provider


def provider_catalog() -> dict[str, object]:
    configured = [provider for provider in PROVIDERS if provider.configured()]
    default = None
    if configured or os.environ.get("SEFIR_DEFAULT_PROVIDER", "").strip():
        default = resolve_provider().id
    return {
        "default_provider": default,
        "providers": [
            {"id": provider.id, "name": provider.name, "model": provider.model()}
            for provider in configured
        ],
        "options": [
            {
                "id": provider.id,
                "name": provider.name,
                "model": provider.model(),
                "configured": provider.configured(),
            }
            for provider in PROVIDERS
        ],
    }
