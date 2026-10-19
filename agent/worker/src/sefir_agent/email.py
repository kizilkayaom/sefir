from mcp import Client, StdioServerParameters
from mcp.types import TextContent


async def approve_draft(
    approval_id: str,
    draft_id: str,
    version: int,
    content_hash: str,
    approved_by: str,
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
            "./cmd/email-mcp",
        ],
    )

    async with Client(server) as client:
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
