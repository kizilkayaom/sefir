import asyncio
import os
import sys
import unittest
from contextlib import chdir
from pathlib import Path
from tempfile import TemporaryDirectory
from textwrap import dedent
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from sefir_agent.catalogue import (
    catalogue_server_parameters,
    create_draft,
    get_confirmed_contact,
    get_draft,
)
from sefir_agent.schemas import EmailDraft


class CatalogueServerTests(unittest.IsolatedAsyncioTestCase):
    def test_development_command_remains_available(self) -> None:
        for database in (
            {},
            {"DATABASE_URL": "postgres://test/test"},
            {"DATABASE_URL": "postgres://test/test", "PGPASSWORD": ""},
            {"DATABASE_URL": "postgres://test/test", "PGPASSWORD": "test:p@ss/word"},
        ):
            for extra in ({}, {"SEFIR_CATALOGUE_MCP_BIN": ""}):
                environment = database | extra | {"COHERE_API_KEY": "unused-test-key"}
                with (
                    self.subTest(database=database, extra=extra),
                    patch.dict(os.environ, environment, clear=True),
                ):
                    server = catalogue_server_parameters()

                self.assertEqual(server.command, "uv")
                self.assertEqual(
                    server.args,
                    [
                        "run",
                        "--env-file",
                        ".env",
                        "go",
                        "-C",
                        "mcp",
                        "run",
                        "./cmd/catalogue-mcp",
                    ],
                )
                self.assertEqual(server.env, database)

    async def test_all_catalogue_operations_use_configured_executable(self) -> None:
        draft = EmailDraft(subject="Hello", body="Test draft")
        cases = (
            (
                get_confirmed_contact,
                {"contact_id": "contact-test"},
                {"contact_id": "contact-test"},
                {"name": "Test contact"},
            ),
            (
                create_draft,
                {"contact_id": "contact-test", "draft": draft},
                {
                    "contact_id": "contact-test",
                    "subject": draft.subject,
                    "body": draft.body,
                },
                {"draft_id": "draft-test", "version": 1},
            ),
            (
                get_draft,
                {"draft_id": "draft-test", "version": 1},
                {"draft_id": "draft-test", "version": 1},
                {"subject": draft.subject, "body": draft.body},
            ),
        )
        environment = {
            "SEFIR_CATALOGUE_MCP_BIN": "/test/catalogue mcp",
            "DATABASE_URL": "postgres://test/test",
            "PGPASSWORD": "test:p@ss/word",
            "SEFIR_ROOT": "/test/workspace",
            "COHERE_API_KEY": "unused-test-key",
        }
        for operation, arguments, tool_arguments, response in cases:
            with (
                self.subTest(operation=operation.__name__),
                patch.dict(os.environ, environment, clear=True),
                patch("sefir_agent.catalogue.Client") as client_class,
            ):
                client = AsyncMock()
                client.call_tool.return_value = SimpleNamespace(
                    is_error=False, structured_content=response
                )
                client_class.return_value.__aenter__.return_value = client

                result = await operation(**arguments)

                self.assertEqual(result, response)
                client.call_tool.assert_awaited_once_with(
                    operation.__name__, tool_arguments
                )
                client_class.assert_called_once()
                server = client_class.call_args.args[0]
                self.assertEqual(server.command, environment["SEFIR_CATALOGUE_MCP_BIN"])
                self.assertEqual(server.args, [])
                self.assertEqual(
                    server.env,
                    {
                        "DATABASE_URL": environment["DATABASE_URL"],
                        "PGPASSWORD": environment["PGPASSWORD"],
                    },
                )
                self.assertEqual(client_class.call_args.kwargs, {})

    @unittest.skipUnless(os.name == "posix", "Requires a POSIX executable script")
    async def test_executable_works_without_development_tools(self) -> None:
        source = dedent("""\
            import os
            import sys
            from mcp.server import MCPServer

            server = MCPServer("catalogue-startup-test")

            @server.tool()
            def get_confirmed_contact(contact_id: str) -> dict[str, object]:
                return {
                    "contact_id": contact_id,
                    "database_url": os.environ.get("DATABASE_URL"),
                    "pgpassword": os.environ.get("PGPASSWORD"),
                    "root": os.environ.get("SEFIR_ROOT"),
                    "cohere_key": os.environ.get("COHERE_API_KEY"),
                    "args": sys.argv[1:],
                }

            server.run()
            """)
        with TemporaryDirectory(prefix="sefir catalogue test ") as directory:
            executable = Path(directory) / "catalogue mcp"
            executable.write_text(f"#!{sys.executable}\n{source}")
            executable.chmod(0o700)
            environment = {
                "SEFIR_CATALOGUE_MCP_BIN": str(executable),
                "DATABASE_URL": "postgres://test/test",
                "PGPASSWORD": "test:p@ss/word",
                "SEFIR_ROOT": "/test/workspace",
                "COHERE_API_KEY": "unused-test-key",
                "PATH": directory,
            }

            with patch.dict(os.environ, environment, clear=True), chdir(directory):
                async with asyncio.timeout(15):
                    result = await get_confirmed_contact("contact-test")

            self.assertEqual(
                result,
                {
                    "contact_id": "contact-test",
                    "database_url": environment["DATABASE_URL"],
                    "pgpassword": environment["PGPASSWORD"],
                    "root": None,
                    "cohere_key": None,
                    "args": [],
                },
            )
