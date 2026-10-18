import argparse
import asyncio
from pathlib import Path

from sefir_agent.catalogue import create_draft, get_confirmed_contact
from sefir_agent.cv import load_cv
from sefir_agent.drafting import draft_outreach


async def run_outreach(contact_id: str, cv_text: str) -> dict[str, object]:
    contact = await get_confirmed_contact(contact_id=contact_id)
    draft = await draft_outreach(contact=contact, cv_text=cv_text)
    return await create_draft(contact_id=contact_id, draft=draft)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Draft and save an outreach email for a confirmed contact."
    )
    parser.add_argument(
        "--contact-id",
        required=True,
        help="UUID of the confirmed contact to retrieve",
    )
    parser.add_argument(
        "--cv",
        type=Path,
        required=True,
        help="Path to your UTF-8 CV text file",
    )
    args = parser.parse_args()

    try:
        cv_text = load_cv(args.cv)
        saved_draft = asyncio.run(run_outreach(args.contact_id, cv_text))
    except (RuntimeError, TypeError, OSError, ValueError) as exc:
        parser.exit(1, f"Error: {exc}\n")

    print(f"Saved draft: {saved_draft['draft_id']} (version {saved_draft['version']})")
    print(f"Subject: {saved_draft['subject']}\n\n{saved_draft['body']}")
