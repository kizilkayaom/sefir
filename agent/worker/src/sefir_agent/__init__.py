import argparse
import asyncio
from pathlib import Path

from sefir_agent.catalogue import get_confirmed_contact
from sefir_agent.cv import load_cv
from sefir_agent.drafting import draft_outreach
from sefir_agent.schemas import EmailDraft


async def run_outreach(contact_id: str, cv_text: str) -> EmailDraft:
    contact = await get_confirmed_contact(contact_id=contact_id)
    return await draft_outreach(contact=contact, cv_text=cv_text)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Draft an outreach email for a confirmed contact."
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
        draft = asyncio.run(run_outreach(args.contact_id, cv_text))
    except (RuntimeError, TypeError, OSError, ValueError) as exc:
        parser.exit(1, f"Error: {exc}\n")

    print(f"Subject: {draft.subject}\n\n{draft.body}")
