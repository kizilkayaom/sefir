from mcp.types import TextContent

from mcp import Client, StdioServerParameters


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
