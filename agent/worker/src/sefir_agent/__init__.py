import argparse
import asyncio
import json

from sefir_agent.catalogue import get_confirmed_contact


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Fetch a confirmed contact from the catalogue."
    )
    parser.add_argument(
        "--contact-id",
        required=True,
        help="UUID of the confirmed contact to retrieve",
    )
    args = parser.parse_args()

    contact = asyncio.run(get_confirmed_contact(args.contact_id))
    print(json.dumps(contact, indent=2))
