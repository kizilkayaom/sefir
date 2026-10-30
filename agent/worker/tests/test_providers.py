import json
import os
import unittest
from unittest.mock import patch

from sefir_agent.providers import (
    ProviderConfigurationError,
    provider_catalog,
    resolve_provider,
)


class ProviderTests(unittest.TestCase):
    def setUp(self) -> None:
        self.environment = patch.dict(os.environ, {}, clear=True)
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def test_no_keys_returns_an_empty_catalog(self) -> None:
        catalog = provider_catalog()
        self.assertIsNone(catalog["default_provider"])
        self.assertEqual(catalog["providers"], [])
        self.assertEqual(len(catalog["options"]), 4)
        self.assertTrue(all(not option["configured"] for option in catalog["options"]))
        with self.assertRaises(ProviderConfigurationError):
            resolve_provider()

    def test_existing_cohere_installation_keeps_its_model(self) -> None:
        os.environ["COHERE_API_KEY"] = "cohere-test-key"
        provider = resolve_provider()
        self.assertEqual(provider.id, "cohere")
        self.assertEqual(provider.model(), "command-a-plus-05-2026")

    def test_each_provider_works_without_other_keys(self) -> None:
        for provider_id, variable in (
            ("cohere", "COHERE_API_KEY"),
            ("anthropic", "ANTHROPIC_API_KEY"),
            ("openai", "OPENAI_API_KEY"),
            ("openrouter", "OPENROUTER_API_KEY"),
        ):
            with (
                self.subTest(provider=provider_id),
                patch.dict(os.environ, {variable: "provider-test-key"}, clear=True),
            ):
                catalog = provider_catalog()
                self.assertEqual(catalog["default_provider"], provider_id)
                self.assertEqual(len(catalog["providers"]), 1)
                self.assertEqual(resolve_provider().id, provider_id)

    def test_catalog_does_not_expose_keys(self) -> None:
        os.environ.update(
            COHERE_API_KEY="cohere-test-key",
            ANTHROPIC_API_KEY="anthropic-test-key",
            OPENAI_API_KEY="openai-test-key",
            SEFIR_DEFAULT_PROVIDER="openai",
            SEFIR_OPENAI_MODEL="gpt-6.1-sol",
        )
        catalog = provider_catalog()
        self.assertEqual(catalog["default_provider"], "openai")
        self.assertEqual(catalog["providers"][-1]["model"], "gpt-6.1-sol")
        for provider in catalog["providers"]:
            self.assertEqual(set(provider), {"id", "name", "model"})
        encoded = json.dumps(catalog)
        for key in ("cohere-test-key", "anthropic-test-key", "openai-test-key"):
            self.assertNotIn(key, encoded)

    def test_explicit_selection_overrides_default(self) -> None:
        os.environ.update(
            COHERE_API_KEY="cohere-test-key", OPENAI_API_KEY="openai-test-key"
        )
        self.assertEqual(resolve_provider().id, "cohere")
        self.assertEqual(resolve_provider("openai").id, "openai")

    def test_missing_key_never_falls_back(self) -> None:
        os.environ["COHERE_API_KEY"] = "cohere-test-key"
        for provider_id in ("anthropic", "openai", "openrouter", "unknown", "", None):
            with self.subTest(provider=provider_id):
                os.environ["SEFIR_DEFAULT_PROVIDER"] = "openai"
                with self.assertRaises(ProviderConfigurationError):
                    resolve_provider(provider_id)

    def test_blank_keys_do_not_enable_providers(self) -> None:
        os.environ.update(COHERE_API_KEY="  ", ANTHROPIC_API_KEY="\n")
        self.assertEqual(provider_catalog()["providers"], [])

    def test_model_overrides_cannot_change_provider_or_endpoint(self) -> None:
        os.environ["OPENAI_API_KEY"] = "openai-test-key"
        for model in (
            "anthropic/claude-sonnet-5-5",
            "https://example.test/model",
            "gpt-6-luna\nsecret",
            "x" * 129,
        ):
            with self.subTest(model=model):
                os.environ["SEFIR_OPENAI_MODEL"] = model
                with self.assertRaises(ProviderConfigurationError):
                    resolve_provider("openai")

    def test_invalid_default_is_reported(self) -> None:
        os.environ.update(
            COHERE_API_KEY="cohere-test-key", SEFIR_DEFAULT_PROVIDER="unknown"
        )
        with self.assertRaises(ProviderConfigurationError):
            provider_catalog()

    def test_openrouter_accepts_namespaced_models_and_variants(self) -> None:
        os.environ["OPENROUTER_API_KEY"] = "openrouter-test-key"
        self.assertEqual(resolve_provider().model(), "openai/gpt-6-luna")
        for model in (
            "anthropic/claude-sonnet-5.5",
            "vendor/model:free",
            "openrouter/auto",
        ):
            with self.subTest(model=model):
                os.environ["SEFIR_OPENROUTER_MODEL"] = model
                self.assertEqual(resolve_provider("openrouter").model(), model)
                self.assertNotIn("openrouter-test-key", json.dumps(provider_catalog()))

    def test_openrouter_rejects_urls_and_extra_routing_prefixes(self) -> None:
        os.environ["OPENROUTER_API_KEY"] = "openrouter-test-key"
        for model in (
            "gpt-6-luna",
            "https://example.test/model",
            "openrouter/openai/gpt-6-luna",
            "vendor/model?key=secret",
            "vendor/../model",
            "vendor/model\nsecret",
            "vendor/" + "x" * 250,
        ):
            with self.subTest(model=model):
                os.environ["SEFIR_OPENROUTER_MODEL"] = model
                with self.assertRaises(ProviderConfigurationError):
                    resolve_provider("openrouter")
