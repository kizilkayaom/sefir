from mcp.types import TextContent

from mcp import Client, StdioServerParameters
from sefir_agent.schemas import EmailDraft


async def get_confirmed_contact(contact_id: str) -> dict[str, object]:
    server = StdioServerParameters(
        command="uv",
        args=[
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

    async with Client(server) as client:
        result = await client.call_tool(
            "get_confirmed_contact",
            {"contact_id": contact_id},
        )

    if result.is_error:
        message = "\n".join(
            block.text for block in result.content if isinstance(block, TextContent)
        )
        raise RuntimeError(message or "Catalogue lookup failed")

    if not isinstance(result.structured_content, dict):
        raise TypeError("Catalogue returned an invalid contact")

    return result.structured_content


async def create_draft(
    contact_id: str,
    draft: EmailDraft,
) -> dict[str, object]:
    server = StdioServerParameters(
        command="uv",
        args=[
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

    async with Client(server) as client:
        result = await client.call_tool(
            "create_draft",
            {
                "contact_id": contact_id,
                "subject": draft.subject,
                "body": draft.body,
            },
        )

    if result.is_error:
        message = "\n".join(
            block.text for block in result.content if isinstance(block, TextContent)
        )
        raise RuntimeError(message or "Draft creation failed")

    if not isinstance(result.structured_content, dict):
        raise TypeError("Catalogue returned an invalid draft")

    return result.structured_content


async def get_draft(draft_id: str, version: int) -> dict[str, object]:
    server = StdioServerParameters(
        command="uv",
        args=[
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

    async with Client(server) as client:
        result = await client.call_tool(
            "get_draft",
            {"draft_id": draft_id, "version": version},
        )
    if result.is_error:
        message = "\n".join(
            block.text for block in result.content if isinstance(block, TextContent)
        )
        raise RuntimeError(message or "Draft lookup failed")

    if not isinstance(result.structured_content, dict):
        raise TypeError("Catalogue returned an invalid draft")

    return result.structured_content
