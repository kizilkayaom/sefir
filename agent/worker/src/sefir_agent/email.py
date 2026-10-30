import os
from pathlib import Path

from mcp import Client, StdioServerParameters
from mcp.types import TextContent


def email_server_parameters() -> StdioServerParameters:
    environment = {
        key: os.environ[key]
        for key in ("DATABASE_URL", "PGPASSWORD", "SEFIR_ROOT")
        if key in os.environ
    }
    executable = os.environ.get("SEFIR_EMAIL_MCP_BIN")
    if executable:
        environment["SEFIR_ROOT"] = environment.get("SEFIR_ROOT") or str(Path.cwd())
        return StdioServerParameters(command=executable, env=environment)

    return StdioServerParameters(
        command="uv",
        args=[
            "run",
            "--env-file",
            ".env",
            "go",
            "-C",
            "mcp",
            "run",
            "./cmd/email-mcp",
        ],
        env=environment,
    )


async def approve_draft(
    approval_id: str,
    draft_id: str,
    version: int,
    content_hash: str,
    approved_by: str,
) -> dict[str, object]:
    async with Client(email_server_parameters(), mode="legacy") as client:
        result = await client.call_tool(
            "approve_draft",
            {
                "approval_id": approval_id,
                "draft_id": draft_id,
                "version": version,
                "content_hash": content_hash,
                "approved_by": approved_by,
            },
        )

    if result.is_error:
        message = "\n".join(
            block.text for block in result.content if isinstance(block, TextContent)
        )
        raise RuntimeError(message or "Draft approval failed")

    if not isinstance(result.structured_content, dict):
        raise TypeError("Email MCP returned an invalid approval")

    return result.structured_content


async def send_outbox_message(outbox_id: str) -> dict[str, object]:
    async with Client(email_server_parameters(), mode="legacy") as client:
        result = await client.call_tool(
            "send_outbox_message",
            {"outbox_id": outbox_id},
        )

    if result.is_error:
        message = "\n".join(
            block.text for block in result.content if isinstance(block, TextContent)
        )
        raise RuntimeError(message or "Outbox send failed")

    if not isinstance(result.structured_content, dict):
        raise TypeError("Email MCP returned an invalid outbox entry")

    return result.structured_content


async def recover_stale_outbox_entries(
    older_than_minutes: int,
    limit: int,
) -> dict[str, object]:
    if not 1 <= older_than_minutes <= 10080:
        raise ValueError("older_than_minutes must be between 1 and 10080")

    if not 1 <= limit <= 100:
        raise ValueError("limit must be between 1 and 100")

    async with Client(email_server_parameters(), mode="legacy") as client:
        result = await client.call_tool(
            "recover_stale_outbox_entries",
            {
                "older_than_minutes": older_than_minutes,
                "limit": limit,
            },
            read_timeout_seconds=60,
        )

    if result.is_error:
        message = "\n".join(
            block.text for block in result.content if isinstance(block, TextContent)
        )
        raise RuntimeError(message or "Outbox recovery failed")

    if not isinstance(result.structured_content, dict):
        raise TypeError("Email MCP returned an invalid recovery result")

    entries = result.structured_content.get("entries")
    if not isinstance(entries, list) or not all(
        isinstance(entry, dict) for entry in entries
    ):
        raise TypeError("Email MCP returned invalid recovery entries")

    if not isinstance(result.structured_content.get("error"), str):
        raise TypeError("Email MCP returned an invalid recovery error")

    return result.structured_content
