import io
import json
import os
import unittest
from unittest.mock import MagicMock, patch
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, urlparse

from sefir_agent.models import (
    MAX_RESPONSE_BYTES,
    ModelDiscoveryError,
    list_models,
    openai_chat_models,
)
from sefir_agent.web_bridge import dispatch


class ModelDiscoveryTests(unittest.TestCase):
    def setUp(self):
        self.environment = patch.dict(
            os.environ,
            {
                name: "private-test-key"
                for name in (
                    "OPENAI_API_KEY",
                    "ANTHROPIC_API_KEY",
                    "COHERE_API_KEY",
                    "OPENROUTER_API_KEY",
                )
            },
            clear=True,
        )
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.opener = MagicMock()
        factory = patch("sefir_agent.models.build_opener", return_value=self.opener)
        factory.start()
        self.addCleanup(factory.stop)

    def pages(self, *pages):
        self.opener.open.side_effect = [
            io.BytesIO(json.dumps(page).encode()) for page in pages
        ]

    def test_openai_only_offers_known_text_chat_models(self):
        supported = openai_chat_models()
        self.assertIn("gpt-6-luna", supported)
        self.assertNotIn("gpt-audio", supported)
        self.pages(
            {
                "data": [
                    {"id": "gpt-6-luna", "api_key": "unexpected-secret"},
                    {"id": "gpt-6-luna"},
                    {"id": "gpt-6.1-sol", "shutdown_date": "2000-01-01"},
                    {"id": "gpt-audio"},
                    {"id": "text-embedding-3-small"},
                    {"id": "gpt-unknown"},
                    {"id": "https://example.test/model"},
                ]
            }
        )
        result = list_models("openai")
        self.assertEqual(
            result, {"models": [{"id": "gpt-6-luna", "name": "gpt-6-luna"}]}
        )
        request = self.opener.open.call_args.args[0]
        self.assertEqual(request.full_url, "https://api.openai.com/v1/models")
        self.assertEqual(request.get_header("Authorization"), "Bearer private-test-key")
        self.assertIsNone(request.data)

    def test_cohere_paginates_and_filters_deprecated_and_non_chat_models(self):
        self.pages(
            {
                "models": [
                    {"name": "command-current", "endpoints": ["chat"]},
                    {
                        "name": "command-old",
                        "endpoints": ["chat"],
                        "is_deprecated": True,
                    },
                    {"name": "embed-model", "endpoints": ["embed"]},
                ],
                "next_page_token": "next&token=2",
            },
            {"models": [{"name": "command-new", "endpoints": ["chat"]}]},
        )
        result = list_models("cohere")
        self.assertEqual(
            [item["id"] for item in result["models"]],
            ["command-current", "command-new"],
        )
        request = self.opener.open.call_args.args[0]
        parsed = urlparse(request.full_url)
        self.assertEqual(parsed.netloc, "api.cohere.com")
        self.assertEqual(
            parse_qs(parsed.query),
            {
                "endpoint": ["chat"],
                "page_size": ["1000"],
                "page_token": ["next&token=2"],
            },
        )

    def test_anthropic_paginates_and_preserves_display_names(self):
        self.pages(
            {
                "data": [
                    {
                        "id": "claude-new",
                        "display_name": "Claude New",
                        "lifecycle": "active",
                    },
                    {"id": "claude-old", "lifecycle": "deprecated"},
                    {"id": "claude-gone", "lifecycle": "retired"},
                ],
                "has_more": True,
                "last_id": "claude-gone",
            },
            {
                "data": [{"id": "claude-next", "display_name": "bad\nname"}],
                "has_more": False,
            },
        )
        result = list_models("anthropic")
        self.assertEqual(
            result["models"],
            [
                {"id": "claude-new", "name": "Claude New"},
                {"id": "claude-next", "name": "claude-next"},
            ],
        )
        request = self.opener.open.call_args.args[0]
        self.assertEqual(request.get_header("Anthropic-version"), "2026-10-21")
        self.assertEqual(
            parse_qs(urlparse(request.full_url).query)["after_id"], ["claude-gone"]
        )

    def test_repeated_or_missing_pagination_cursor_is_rejected(self):
        for pages in (
            [{"data": [], "has_more": True}],
            [{"data": [], "has_more": True, "last_id": "same"}] * 2,
        ):
            with self.subTest(pages=pages):
                self.pages(*pages)
                with self.assertRaisesRegex(
                    ModelDiscoveryError, "models_invalid_response"
                ):
                    list_models("anthropic")

    def test_upstream_errors_are_sanitized(self):
        for status, expected in [
            (401, "models_auth_failed"),
            (403, "models_auth_failed"),
            (429, "models_rate_limited"),
            (500, "models_unavailable"),
            (302, "models_unavailable"),
        ]:
            with self.subTest(status=status):
                self.opener.open.side_effect = HTTPError(
                    "https://example.test/private-test-key",
                    status,
                    "private-test-key",
                    {},
                    io.BytesIO(b"private-test-key"),
                )
                with self.assertRaises(ModelDiscoveryError) as result:
                    list_models("anthropic")
                self.assertEqual(str(result.exception), expected)
        self.opener.open.side_effect = URLError("private-test-key")
        with self.assertRaisesRegex(ModelDiscoveryError, "^models_unavailable$"):
            list_models("anthropic")

    def test_malformed_and_oversized_responses_are_rejected(self):
        for body in (
            b"not json",
            b"[]",
            b"{}",
            b'{"data":null}',
            b'{"data":[null]}',
            b" " * (MAX_RESPONSE_BYTES + 1),
        ):
            with self.subTest(size=len(body)):
                self.opener.open.side_effect = [io.BytesIO(body)]
                with self.assertRaisesRegex(
                    ModelDiscoveryError, "models_invalid_response"
                ):
                    list_models("anthropic")

    def test_unknown_and_unconfigured_providers_do_not_make_requests(self):
        for provider in ("unknown", "https://example.test", "openai"):
            with (
                self.subTest(provider=provider),
                patch.dict(os.environ, {}, clear=True),
                self.assertRaisesRegex(ModelDiscoveryError, "provider_unavailable"),
            ):
                list_models(provider)
        self.opener.open.assert_not_called()

    def test_empty_catalogue_is_successful(self):
        self.pages({"models": []})
        self.assertEqual(list_models("cohere"), {"models": []})

    def test_openrouter_filters_for_text_drafting_and_preserves_namespaced_ids(self):
        architecture = {
            "input_modalities": ["text", "image"],
            "output_modalities": ["text"],
        }
        self.pages(
            {
                "data": [
                    {
                        "id": "openai/gpt-6-luna",
                        "name": "OpenAI: GPT-6 Luna",
                        "architecture": architecture,
                    },
                    {
                        "id": "vendor/model:free",
                        "name": "Free model",
                        "architecture": architecture,
                    },
                    {
                        "id": "vendor/expired",
                        "architecture": architecture,
                        "expiration_date": "2000-01-01",
                    },
                    {
                        "id": "vendor/image",
                        "architecture": {
                            "input_modalities": ["text"],
                            "output_modalities": ["text", "image"],
                        },
                    },
                    {
                        "id": "vendor/audio",
                        "architecture": {
                            "input_modalities": ["audio"],
                            "output_modalities": ["text"],
                        },
                    },
                    {
                        "id": "vendor/embed",
                        "architecture": {
                            "input_modalities": ["text"],
                            "output_modalities": ["embeddings"],
                        },
                    },
                    {"id": "vendor/missing-architecture"},
                    {"id": "https://example.test/model", "architecture": architecture},
                ]
            }
        )
        self.assertEqual(
            list_models("openrouter"),
            {
                "models": [
                    {"id": "vendor/model:free", "name": "Free model"},
                    {"id": "openai/gpt-6-luna", "name": "OpenAI: GPT-6 Luna"},
                ]
            },
        )
        request = self.opener.open.call_args.args[0]
        self.assertEqual(
            request.full_url,
            "https://openrouter.ai/api/v1/models/user?limit=100&offset=0&output_modalities=text",
        )
        self.assertEqual(request.get_header("Authorization"), "Bearer private-test-key")

    def test_openrouter_paginates_before_filtering(self):
        self.pages(
            {"data": [{"id": f"vendor/filtered-{index}"} for index in range(100)]},
            {
                "data": [
                    {
                        "id": "vendor/usable",
                        "architecture": {
                            "input_modalities": ["text"],
                            "output_modalities": ["text"],
                        },
                    }
                ]
            },
        )
        self.assertEqual(
            list_models("openrouter"),
            {"models": [{"id": "vendor/usable", "name": "vendor/usable"}]},
        )
        self.assertEqual(
            parse_qs(urlparse(self.opener.open.call_args.args[0].full_url).query)[
                "offset"
            ],
            ["100"],
        )


class ModelDiscoveryBridgeTests(unittest.IsolatedAsyncioTestCase):
    async def test_discovery_errors_are_returned_without_provider_details(self):
        with patch(
            "sefir_agent.models.list_models",
            side_effect=ModelDiscoveryError("models_auth_failed"),
        ):
            self.assertEqual(
                await dispatch("models", {"provider": "cohere"}),
                {"error": "models_auth_failed"},
            )

    async def test_discovery_does_not_connect_to_temporal(self):
        with (
            patch(
                "sefir_agent.models.list_models", return_value={"models": []}
            ) as models,
            patch("sefir_agent.web_bridge.Client.connect") as connect,
        ):
            self.assertEqual(
                await dispatch("models", {"provider": "cohere"}), {"models": []}
            )
            models.assert_called_once_with("cohere")
            connect.assert_not_called()
