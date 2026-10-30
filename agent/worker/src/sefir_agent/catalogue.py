import os

from mcp import Client, StdioServerParameters
from mcp.types import TextContent

from sefir_agent.schemas import EmailDraft


def catalogue_server_parameters() -> StdioServerParameters:
    environment = {}
    if "DATABASE_URL" in os.environ:
        environment["DATABASE_URL"] = os.environ["DATABASE_URL"]
    if "PGPASSWORD" in os.environ:
        environment["PGPASSWORD"] = os.environ["PGPASSWORD"]

    executable = os.environ.get("SEFIR_CATALOGUE_MCP_BIN")
    if executable:
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
            "./cmd/catalogue-mcp",
        ],
        env=environment,
    )


async def get_confirmed_contact(contact_id: str) -> dict[str, object]:
    async with Client(catalogue_server_parameters()) as client:
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
    async with Client(catalogue_server_parameters()) as client:
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
    async with Client(catalogue_server_parameters()) as client:
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
