import importlib
import json
import os
import unittest
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from pydantic import ValidationError

from sefir_agent.schemas import PolishedEmail


class PolishingTests(unittest.IsolatedAsyncioTestCase):
    @classmethod
    def setUpClass(cls) -> None:
        with patch.dict(
            os.environ,
            {
                "LITELLM_MODE": "PRODUCTION",
                "LITELLM_LOCAL_MODEL_COST_MAP": "True",
                "PYTHON_DOTENV_DISABLED": "1",
            },
            clear=True,
        ):
            cls.polishing = importlib.import_module("sefir_agent.polishing")
            cls.litellm = importlib.import_module("litellm")

    def setUp(self) -> None:
        self.environment = patch.dict(
            os.environ,
            {
                "COHERE_API_KEY": "cohere-test-key",
                "LITELLM_MODE": "PRODUCTION",
                "LITELLM_LOCAL_MODEL_COST_MAP": "True",
                "PYTHON_DOTENV_DISABLED": "1",
            },
            clear=True,
        )
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.draft = PolishedEmail(subject="Introduction", body="Hello Alex.")
        self.suggestion = {
            "subject": "A quick introduction",
            "body": "Hi Alex.",
            "body_html": "",
        }
        self.completion = AsyncMock(
            return_value=self.response(json.dumps(self.suggestion))
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

    async def test_plain_text_success_keeps_original_unchanged(self) -> None:
        original = self.draft.model_dump()

        polished = await self.polishing.polish_email(self.draft, provider_id="cohere")

        self.assertEqual(polished.model_dump(), self.suggestion)
        self.assertEqual(self.draft.model_dump(), original)
        self.completion.assert_awaited_once()

    async def test_compatible_html_changes_are_allowed(self) -> None:
        for name, body, body_html, suggested_body, suggested_html in (
            (
                "wording",
                "Hello Alex.",
                "<p>Hello <strong>Alex</strong>.</p>",
                "Hi Alex.",
                "<p>Hi <strong>Alex</strong>.</p>",
            ),
            (
                "attribute order",
                "Hello Alex <https://example.com/?a=1&b=2>.",
                (
                    '<p>Hello <a href="https://example.com/?a=1&amp;b=2" '
                    'rel="nofollow noreferrer">Alex</a>.</p>'
                ),
                "Hi Alex <https://example.com/?a=1&b=2>.",
                (
                    '<p>Hi <a rel="nofollow noreferrer" '
                    'href="https://example.com/?a=1&amp;b=2">Alex</a>.</p>'
                ),
            ),
            (
                "line break",
                "Hello\nAlex.",
                "<p>Hello<br>Alex.</p>",
                "Hi\nAlex.",
                "<p>Hi<br/>Alex.</p>",
            ),
        ):
            with self.subTest(name=name):
                self.draft.body = body
                self.draft.body_html = body_html
                original = self.draft.model_dump()
                self.suggestion["body"] = suggested_body
                self.suggestion["body_html"] = suggested_html
                self.completion.reset_mock()
                self.completion.return_value = self.response(
                    json.dumps(self.suggestion)
                )

                polished = await self.polishing.polish_email(
                    self.draft, provider_id="cohere"
                )

                self.assertEqual(polished.model_dump(), self.suggestion)
                self.assertEqual(self.draft.model_dump(), original)
                self.completion.assert_awaited_once()

    async def test_invalid_model_output_is_rejected(self) -> None:
        for content in (
            "Not JSON",
            json.dumps({"body": "Hi Alex.", "body_html": ""}),
            json.dumps({**self.suggestion, "body": " \n\t "}),
            json.dumps({**self.suggestion, "recipient": "other@example.com"}),
            json.dumps({**self.suggestion, "body_html": 42}),
        ):
            with self.subTest(content=content):
                original = self.draft.model_dump()
                self.completion.reset_mock()
                self.completion.return_value = self.response(content)

                with self.assertRaisesRegex(
                    RuntimeError, "^Agent returned an invalid polished email$"
                ) as raised:
                    await self.polishing.polish_email(self.draft, provider_id="cohere")

                self.assertIsInstance(raised.exception.__cause__, ValidationError)
                self.assertEqual(self.draft.model_dump(), original)
                self.completion.assert_awaited_once()

    async def test_empty_model_response_is_rejected(self) -> None:
        for content in ("", " \n\t "):
            with self.subTest(content=content):
                original = self.draft.model_dump()
                self.completion.reset_mock()
                self.completion.return_value = self.response(content)

                with self.assertRaisesRegex(RuntimeError, "^Email polishing failed$"):
                    await self.polishing.polish_email(self.draft, provider_id="cohere")

                self.assertEqual(self.draft.model_dump(), original)
                self.completion.assert_awaited_once()

        for content in (None, "", " \n\t "):
            with self.subTest(event_content=content):
                original = self.draft.model_dump()
                self.completion.reset_mock()

                async def events(content=content, **kwargs):
                    if content is not None:
                        yield SimpleNamespace(
                            error_code=None,
                            is_final_response=lambda: True,
                            content=SimpleNamespace(
                                parts=[SimpleNamespace(text=content, thought=False)]
                            ),
                        )

                with (
                    patch.object(
                        self.polishing.InMemoryRunner, "run_async", side_effect=events
                    ) as run,
                    self.assertRaisesRegex(
                        RuntimeError, "^Agent returned no polished email$"
                    ),
                ):
                    await self.polishing.polish_email(self.draft, provider_id="cohere")

                self.assertEqual(self.draft.model_dump(), original)
                run.assert_called_once()
                self.completion.assert_not_awaited()

    async def test_provider_failure_does_not_try_another_provider(self) -> None:
        original = self.draft.model_dump()
        error = RuntimeError("Mock provider unavailable")
        self.completion.side_effect = error

        with (
            patch.dict(os.environ, {"OPENAI_API_KEY": "openai-test-key"}),
            self.assertRaisesRegex(RuntimeError, "^Email polishing failed$") as raised,
        ):
            await self.polishing.polish_email(self.draft, provider_id="cohere")

        self.assertNotIn(str(error), str(raised.exception))
        self.assertEqual(self.draft.model_dump(), original)
        self.completion.assert_awaited_once()

    async def test_agent_error_event_hides_private_details(self) -> None:
        original = self.draft.model_dump()
        private_details = "Fictional private provider details"

        async def events(**kwargs):
            yield SimpleNamespace(
                error_code="mock_provider_error", error_message=private_details
            )

        with (
            patch.object(
                self.polishing.InMemoryRunner, "run_async", side_effect=events
            ) as run,
            self.assertRaisesRegex(RuntimeError, "^Email polishing failed$") as raised,
        ):
            await self.polishing.polish_email(self.draft, provider_id="cohere")

        self.assertNotIn(private_details, str(raised.exception))
        self.assertEqual(self.draft.model_dump(), original)
        run.assert_called_once()
        self.completion.assert_not_awaited()

    async def test_changed_html_structure_or_links_are_rejected(self) -> None:
        for name, body_html, suggested_html in (
            (
                "removed formatting",
                "<p>Hello <strong>Alex</strong>.</p>",
                "<p>Hi Alex.</p>",
            ),
            (
                "added formatting",
                "<p>Hello Alex.</p>",
                "<p>Hi <em>Alex</em>.</p>",
            ),
            (
                "changed link",
                '<a href="https://example.com/Work">Hello Alex.</a>',
                '<a href="https://example.com/work">Hi Alex.</a>',
            ),
            (
                "changed rel",
                '<a href="https://example.com" rel="nofollow">Hello Alex.</a>',
                '<a href="https://example.com" rel="noreferrer">Hi Alex.</a>',
            ),
            (
                "changed numbering",
                '<ol start="3"><li>Hello Alex.</li></ol>',
                '<ol start="4"><li>Hi Alex.</li></ol>',
            ),
        ):
            with self.subTest(name=name):
                self.draft.body_html = body_html
                original = self.draft.model_dump()
                self.suggestion["body_html"] = suggested_html
                self.completion.reset_mock()
                self.completion.return_value = self.response(
                    json.dumps(self.suggestion)
                )

                with self.assertRaisesRegex(
                    RuntimeError, "^Agent changed the email HTML structure or links$"
                ):
                    await self.polishing.polish_email(self.draft, provider_id="cohere")

                self.assertEqual(self.draft.model_dump(), original)
                self.completion.assert_awaited_once()

    async def test_invalid_nesting_in_original_or_suggestion_is_rejected(self) -> None:
        for invalid_html in (
            "<p>Hello <strong>Alex</p></strong>",
            "<p>Hello Alex.",
        ):
            for invalid_side in ("original", "suggestion"):
                with self.subTest(html=invalid_html, side=invalid_side):
                    self.draft.body_html = (
                        invalid_html
                        if invalid_side == "original"
                        else "<p>Hello Alex.</p>"
                    )
                    original = self.draft.model_dump()
                    self.suggestion["body_html"] = (
                        invalid_html
                        if invalid_side == "suggestion"
                        else "<p>Hi Alex.</p>"
                    )
                    self.completion.reset_mock()
                    self.completion.return_value = self.response(
                        json.dumps(self.suggestion)
                    )

                    with self.assertRaisesRegex(
                        RuntimeError, "^Invalid email HTML structure$"
                    ) as raised:
                        await self.polishing.polish_email(
                            self.draft, provider_id="cohere"
                        )

                    self.assertIsInstance(raised.exception.__cause__, ValueError)
                    self.assertEqual(self.draft.model_dump(), original)
                    self.completion.assert_awaited_once()

    async def test_changed_email_format_is_rejected(self) -> None:
        for body_html, suggested_html in (
            ("", "<p>Hi Alex.</p>"),
            ("<p>Hello Alex.</p>", ""),
        ):
            with self.subTest(original=body_html, suggestion=suggested_html):
                self.draft.body_html = body_html
                original = self.draft.model_dump()
                self.suggestion["body_html"] = suggested_html
                self.completion.reset_mock()
                self.completion.return_value = self.response(
                    json.dumps(self.suggestion)
                )

                with self.assertRaisesRegex(
                    RuntimeError, "^Agent changed the email format$"
                ):
                    await self.polishing.polish_email(self.draft, provider_id="cohere")

                self.assertEqual(self.draft.model_dump(), original)
                self.completion.assert_awaited_once()
