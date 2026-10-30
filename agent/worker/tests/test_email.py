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

from mcp import Client

from sefir_agent.email import (
    approve_draft,
    email_server_parameters,
    recover_stale_outbox_entries,
    send_outbox_message,
)


class EmailServerTests(unittest.IsolatedAsyncioTestCase):
    def test_development_command_remains_available(self) -> None:
        environment = {
            "DATABASE_URL": "postgres://test/test",
            "SEFIR_ROOT": "/test/workspace",
        }
        for password in ({}, {"PGPASSWORD": ""}, {"PGPASSWORD": "test:p@ss/word"}):
            for extra in ({}, {"SEFIR_EMAIL_MCP_BIN": ""}):
                with (
                    self.subTest(password=password, extra=extra),
                    patch.dict(os.environ, environment | password | extra, clear=True),
                ):
                    server = email_server_parameters()

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
                        "./cmd/email-mcp",
                    ],
                )
                self.assertEqual(server.env, environment | password)

    def test_binary_defaults_to_current_data_directory(self) -> None:
        for extra in ({}, {"SEFIR_ROOT": ""}):
            environment = {"SEFIR_EMAIL_MCP_BIN": "/test/email-mcp"} | extra
            with (
                self.subTest(extra=extra),
                patch.dict(os.environ, environment, clear=True),
            ):
                server = email_server_parameters()
                self.assertEqual(server.env, {"SEFIR_ROOT": str(Path.cwd())})
                self.assertEqual(dict(os.environ), environment)

    async def test_all_email_operations_use_configured_executable(self) -> None:
        approval = {
            "approval_id": "approval-test",
            "draft_id": "draft-test",
            "version": 1,
            "content_hash": "a" * 64,
            "approved_by": "Owner",
        }
        cases = (
            (approve_draft, approval, {"outbox_id": "outbox-test"}, {}),
            (
                send_outbox_message,
                {"outbox_id": "outbox-test"},
                {"state": "sent"},
                {},
            ),
            (
                recover_stale_outbox_entries,
                {"older_than_minutes": 5, "limit": 10},
                {"entries": [], "error": ""},
                {"read_timeout_seconds": 60},
            ),
        )
        environment = {
            "SEFIR_EMAIL_MCP_BIN": "/test/email mcp",
            "DATABASE_URL": "postgres://test/test",
            "PGPASSWORD": "test:p@ss/word",
            "SEFIR_ROOT": "/test/workspace",
        }
        for operation, arguments, response, options in cases:
            with (
                self.subTest(operation=operation.__name__),
                patch.dict(os.environ, environment, clear=True),
                patch("sefir_agent.email.Client") as client_class,
            ):
                client = AsyncMock()
                client.call_tool.return_value = SimpleNamespace(
                    is_error=False, structured_content=response
                )
                client_class.return_value.__aenter__.return_value = client

                result = await operation(**arguments)

                self.assertEqual(result, response)
                client.call_tool.assert_awaited_once_with(
                    operation.__name__, arguments, **options
                )
                client_class.assert_called_once()
                server = client_class.call_args.args[0]
                self.assertEqual(server.command, environment["SEFIR_EMAIL_MCP_BIN"])
                self.assertEqual(server.args, [])
                self.assertEqual(
                    server.env["DATABASE_URL"], environment["DATABASE_URL"]
                )
                self.assertEqual(server.env["PGPASSWORD"], environment["PGPASSWORD"])
                self.assertEqual(server.env["SEFIR_ROOT"], environment["SEFIR_ROOT"])
                self.assertEqual(client_class.call_args.kwargs, {"mode": "legacy"})

    @unittest.skipUnless(os.name == "posix", "Requires a POSIX executable script")
    async def test_executable_receives_configuration_without_development_tools(self):
        source = dedent("""\
            import os
            import sys
            from mcp.server import MCPServer

            server = MCPServer("email-startup-test")

            @server.tool()
            def inspect_environment() -> dict[str, object]:
                return {
                    "database_url": os.environ.get("DATABASE_URL"),
                    "pgpassword": os.environ.get("PGPASSWORD"),
                    "root": os.environ.get("SEFIR_ROOT"),
                    "cohere_key": os.environ.get("COHERE_API_KEY"),
                    "args": sys.argv[1:],
                    "cwd": os.getcwd(),
                }

            server.run()
            """)
        with TemporaryDirectory(prefix="sefir email test ") as directory:
            executable = Path(directory) / "email mcp"
            executable.write_text(f"#!{sys.executable}\n{source}")
            executable.chmod(0o700)
            root = str(Path(directory) / "data")
            environment = {
                "SEFIR_EMAIL_MCP_BIN": str(executable),
                "DATABASE_URL": "postgres://test/test",
                "PGPASSWORD": "test:p@ss/word",
                "SEFIR_ROOT": root,
                "COHERE_API_KEY": "unused-test-key",
                "PATH": directory,
            }

            with patch.dict(os.environ, environment, clear=True), chdir(directory):
                async with asyncio.timeout(15):
                    async with Client(
                        email_server_parameters(), mode="legacy"
                    ) as client:
                        result = await client.call_tool("inspect_environment", {})

            self.assertFalse(result.is_error)
            self.assertEqual(
                result.structured_content,
                {
                    "database_url": environment["DATABASE_URL"],
                    "pgpassword": environment["PGPASSWORD"],
                    "root": root,
                    "cohere_key": None,
                    "args": [],
                    "cwd": str(Path(directory).resolve()),
                },
            )
