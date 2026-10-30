import importlib
import json
import os
import unittest
from unittest.mock import AsyncMock, patch

from sefir_agent.providers import ProviderConfigurationError


class DraftingTests(unittest.IsolatedAsyncioTestCase):
    @classmethod
    def setUpClass(cls) -> None:
        with patch.dict(
            os.environ,
            {
                "LITELLM_MODE": "PRODUCTION",
                "LITELLM_LOCAL_MODEL_COST_MAP": "True",
                "PYTHON_DOTENV_DISABLED": "1",
            },
        ):
            cls.drafting = importlib.import_module("sefir_agent.drafting")
            cls.litellm = importlib.import_module("litellm")

    def setUp(self) -> None:
        self.environment = patch.dict(
            os.environ,
            {
                "COHERE_API_KEY": "cohere-test-key",
                "ANTHROPIC_API_KEY": "anthropic-test-key",
                "OPENAI_API_KEY": "openai-test-key",
                "OPENROUTER_API_KEY": "openrouter-test-key",
                "LITELLM_MODE": "PRODUCTION",
                "LITELLM_LOCAL_MODEL_COST_MAP": "True",
                "PYTHON_DOTENV_DISABLED": "1",
            },
            clear=True,
        )
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.completion = AsyncMock(
            return_value=self.response(
                '{"subject":"Hello","body":"A fictional introduction."}'
            )
        )
        self.client = patch(
            "google.adk.models.lite_llm.LiteLLMClient.acompletion",
            self.completion,
        )
        self.client.start()
        self.addCleanup(self.client.stop)

    def response(self, content: str):
        return self.litellm.ModelResponse(
            choices=[
                {
                    "message": {"role": "assistant", "content": content},
                    "finish_reason": "stop",
                }
            ]
        )

    async def test_all_providers_keep_the_same_draft_contract(self) -> None:
        for provider, model, key in (
            ("cohere", "cohere_chat/command-a-plus-05-2026", "cohere-test-key"),
            ("anthropic", "anthropic/claude-sonnet-5-5", "anthropic-test-key"),
            ("openai", "openai/gpt-6-luna", "openai-test-key"),
            ("openrouter", "openrouter/openai/gpt-6-luna", "openrouter-test-key"),
        ):
            with self.subTest(provider=provider):
                self.completion.reset_mock()
                draft = await self.drafting.draft_outreach(
                    {"name": "Alex Example"}, "Fictional resume", provider
                )
                self.assertEqual(draft.subject, "Hello")
                self.assertEqual(draft.body, "A fictional introduction.")
                self.completion.assert_awaited_once()
                arguments = self.completion.call_args.kwargs
                self.assertEqual(arguments["model"], model)
                self.assertEqual(arguments["api_key"], key)
                self.assertIn("Fictional resume", json.dumps(arguments["messages"]))

    async def test_openrouter_model_override_keeps_routing_and_key(self) -> None:
        os.environ["SEFIR_OPENROUTER_MODEL"] = "vendor/model:free"
        await self.drafting.draft_outreach(
            {"name": "Alex Example"}, "Fictional resume", "openrouter"
        )
        arguments = self.completion.call_args.kwargs
        self.assertEqual(arguments["model"], "openrouter/vendor/model:free")
        self.assertEqual(arguments["api_key"], "openrouter-test-key")

    async def test_unconfigured_provider_never_calls_a_model(self) -> None:
        del os.environ["OPENAI_API_KEY"]
        with self.assertRaises(ProviderConfigurationError):
            await self.drafting.draft_outreach(
                {"name": "Alex Example"}, "Fictional resume", "openai"
            )
        self.completion.assert_not_awaited()

    async def test_provider_failure_does_not_try_another_provider(self) -> None:
        self.completion.side_effect = RuntimeError("Mock provider unavailable")
        with self.assertRaisesRegex(RuntimeError, "Mock provider unavailable"):
            await self.drafting.draft_outreach(
                {"name": "Alex Example"}, "Fictional resume", "anthropic"
            )
        self.completion.assert_awaited_once()

    async def test_invalid_model_output_is_rejected(self) -> None:
        for content in (
            "Not JSON",
            '{"subject":"","body":"Hello"}',
            '{"subject":"Hello","body":"Hello","recipient":"other@example.com"}',
        ):
            with self.subTest(content=content):
                self.completion.return_value = self.response(content)
                with self.assertRaisesRegex(RuntimeError, "invalid email draft"):
                    await self.drafting.draft_outreach(
                        {"name": "Alex Example"}, "Fictional resume", "openai"
                    )
