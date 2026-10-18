import argparse
import asyncio
from pathlib import Path

from sefir_agent.catalogue import create_draft, get_confirmed_contact, get_draft
from sefir_agent.cv import load_cv
from sefir_agent.drafting import draft_outreach


async def run_outreach(contact_id: str, cv_text: str) -> dict[str, object]:
    contact = await get_confirmed_contact(contact_id=contact_id)
    draft = await draft_outreach(contact=contact, cv_text=cv_text)
    return await create_draft(contact_id=contact_id, draft=draft)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Generate outreach drafts or review a saved draft."
    )
    commands = parser.add_subparsers(dest="command", required=True)

    draft_parser = commands.add_parser("draft", help="Generate and save a draft")
    draft_parser.add_argument(
        "--contact-id",
        required=True,
        help="UUID of the confirmed contact to retrieve",
    )
    draft_parser.add_argument(
        "--cv",
        type=Path,
        required=True,
        help="Path to your UTF-8 CV text file",
    )

    review_parser = commands.add_parser("review", help="Review a saved draft")
    review_parser.add_argument(
        "--draft-id",
        required=True,
        help="UUID of the saved draft to review",
    )
    review_parser.add_argument(
        "--version",
        type=int,
        required=True,
        help="Saved draft version to review (at least 1)",
    )
    args = parser.parse_args()

    if args.command == "review" and args.version < 1:
        review_parser.error("--version must be at least 1")

    try:
        if args.command == "draft":
            cv_text = load_cv(args.cv)
            saved_draft = asyncio.run(run_outreach(args.contact_id, cv_text))
        else:
            saved_draft = asyncio.run(get_draft(args.draft_id, args.version))
    except (RuntimeError, TypeError, OSError, ValueError) as exc:
        parser.exit(1, f"Error: {exc}\n")

    label = "Saved draft" if args.command == "draft" else "Draft"
    print(f"{label}: {saved_draft['draft_id']} (version {saved_draft['version']})")
    print(f"To: {saved_draft['recipient_email'] or '(no recipient email)'}")
    print(f"Subject: {saved_draft['subject']}\n\n{saved_draft['body']}")
