import argparse
import asyncio
from pathlib import Path

from temporalio.client import Client
from temporalio.exceptions import WorkflowAlreadyStartedError
from temporalio.service import RPCError

from sefir_agent.catalogue import create_draft, get_confirmed_contact, get_draft
from sefir_agent.cv import load_cv
from sefir_agent.drafting import draft_outreach
from sefir_agent.email import (
    approve_draft,
    recover_stale_outbox_entries,
    send_outbox_message,
)
from sefir_agent.temporal_client import (
    get_approval_workflow_status,
    signal_workflow_approval,
    start_approval_workflow,
)
from sefir_agent.workflow_types import ApprovalWorkflowInput, DraftApproval


async def run_outreach(contact_id: str, cv_text: str) -> dict[str, object]:
    contact = await get_confirmed_contact(contact_id=contact_id)
    draft = await draft_outreach(contact=contact, cv_text=cv_text)
    return await create_draft(contact_id=contact_id, draft=draft)


async def run_start_workflow(draft_id: str, version: int) -> str:
    draft = await get_draft(draft_id=draft_id, version=version)
    request = ApprovalWorkflowInput(
        draft_id=draft["draft_id"],
        version=draft["version"],
        content_hash=draft["content_hash"],
    )

    client = await Client.connect("127.0.0.1:7233")
    return await start_approval_workflow(client, request)


async def run_approve_workflow(approval: DraftApproval) -> None:
    if not approval.approval_id.strip():
        raise ValueError("Approval ID must not be blank")
    if not approval.approved_by.strip():
        raise ValueError("Approver must not be blank")

    client = await Client.connect("127.0.0.1:7233")
    await signal_workflow_approval(client, approval)


async def run_workflow_status(draft_id: str, version: int) -> dict[str, str]:
    client = await Client.connect("127.0.0.1:7233")
    return await get_approval_workflow_status(client, draft_id, version)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Generate, review, approve, send, or recover outreach emails."
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

    approve_parser = commands.add_parser(
        "approve", help="Review and approve a saved draft"
    )

    approve_parser.add_argument(
        "--draft-id", type=str, required=True, help="Saved draft UUID"
    )

    approve_parser.add_argument(
        "--version", type=int, required=True, help="Exact version to approve"
    )

    approve_parser.add_argument(
        "--approved-by", type=str, required=True, help="Approver's name"
    )

    approve_parser.add_argument(
        "--approval-id",
        type=str,
        required=True,
        help="UUID generated once and reused for retries",
    )

    send_parser = commands.add_parser("send", help="Send an approved outbox entry")
    send_parser.add_argument(
        "--outbox-id",
        required=True,
        help="UUID of the approved outbox entry",
    )

    recover_parser = commands.add_parser(
        "recover",
        help="Check stale sending entries against Gmail and record confirmed matches.",
    )
    recover_parser.add_argument(
        "--older-than-minutes",
        default=10,
        type=int,
    )

    recover_parser.add_argument(
        "--limit",
        type=int,
        default=10,
    )

    workflow_parser = commands.add_parser(
        "start-workflow",
        help="Start a workflow that waits for draft approval",
    )

    workflow_parser.add_argument(
        "--draft-id",
        type=str,
        required=True,
    )

    workflow_parser.add_argument(
        "--version",
        type=int,
        required=True,
    )

    workflow_approve_parser = commands.add_parser(
        "approve-workflow",
        help="Review a draft and approve sending through its workflow",
    )

    workflow_approve_parser.add_argument(
        "--draft-id",
        type=str,
        required=True,
    )

    workflow_approve_parser.add_argument(
        "--version",
        type=int,
        required=True,
    )

    workflow_approve_parser.add_argument(
        "--approved-by",
        type=str,
        required=True,
    )

    workflow_approve_parser.add_argument(
        "--approval-id",
        type=str,
        required=True,
    )

    workflow_status_parser = commands.add_parser(
        "workflow-status",
        help="Check an approval workflow's status",
    )
    workflow_status_parser.add_argument(
        "--draft-id",
        type=str,
        required=True,
    )
    workflow_status_parser.add_argument(
        "--version",
        type=int,
        required=True,
    )

    args = parser.parse_args()

    if args.command == "review" and args.version < 1:
        review_parser.error("--version must be at least 1")
    if args.command == "approve" and args.version < 1:
        approve_parser.error("--version must be at least 1")
    if args.command == "approve-workflow" and args.version < 1:
        workflow_approve_parser.error("--version must be at least 1")
    if args.command == "start-workflow":
        if args.version < 1:
            workflow_parser.error("--version must be at least 1")
        try:
            workflow_id = asyncio.run(run_start_workflow(args.draft_id, args.version))
        except (
            RuntimeError,
            TypeError,
            OSError,
            ValueError,
            RPCError,
            WorkflowAlreadyStartedError,
        ) as exc:
            parser.exit(1, f"Error: {exc}\n")

        print(f"Workflow ID: {workflow_id}")
        print("Waiting for approval")
        return

    if args.command == "workflow-status":
        if args.version < 1:
            workflow_status_parser.error("--version must be at least 1")
        try:
            result = asyncio.run(run_workflow_status(args.draft_id, args.version))
        except (RuntimeError, TypeError, OSError, ValueError, RPCError) as exc:
            parser.exit(1, f"Error: {exc}\n")

        print(f"Workflow ID: {result['workflow_id']}")
        print(f"Run ID: {result['run_id']}")
        print(f"Status: {result['status']}")
        return

    if args.command == "send":
        try:
            entry = asyncio.run(send_outbox_message(args.outbox_id))
        except (RuntimeError, TypeError, OSError, ValueError) as exc:
            parser.exit(1, f"Error: {exc}\n")

        print(f"Outbox ID: {entry['outbox_id']}")
        print(f"Outbox state: {entry['state']}")
        print(f"Attempts: {entry['attempts']}")
        if entry.get("gmail_message_id"):
            print(f"Gmail message ID: {entry['gmail_message_id']}")
        if entry.get("last_error"):
            print(f"Last error: {entry['last_error']}")

        if entry["state"] != "sent":
            parser.exit(
                1, f"Error: Sending was not confirmed (state: {entry['state']}).\n"
            )
        return

    if args.command == "recover":
        try:
            result = asyncio.run(
                recover_stale_outbox_entries(args.older_than_minutes, args.limit)
            )
        except (RuntimeError, TypeError, OSError, ValueError) as exc:
            parser.exit(1, f"Error: {exc}\n")

        entries = result["entries"]
        for entry in entries:
            print(f"Outbox ID: {entry['outbox_id']}")
            print(f"Outbox state: {entry['state']}")
            if entry.get("gmail_message_id"):
                print(f"Gmail message ID: {entry['gmail_message_id']}")

        if result["error"]:
            parser.exit(1, f"Error: {result['error']}\n")

        if any(entry["state"] != "sent" for entry in entries):
            parser.exit(1, "Error: Some sends remain unconfirmed after recovery.\n")

        if not entries:
            print("No stale outbox entries found.")
        return

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

    if args.command not in ("approve", "approve-workflow"):
        return

    recipient = saved_draft["recipient_email"]
    if not recipient or not recipient.strip():
        parser.exit(1, "Error: Draft recipient email is required.\n")

    prompt = "\nType approve to approve this exact draft version: "
    if args.command == "approve-workflow":
        prompt = "\nType approve to approve and send this exact draft version: "

    try:
        confirmation = input(prompt).strip().lower()
    except (EOFError, KeyboardInterrupt):
        print("\nApproval cancelled.")
        return

    if confirmation != "approve":
        print("Approval cancelled.")
        return

    if args.command == "approve-workflow":
        workflow_approval = DraftApproval(
            approval_id=args.approval_id,
            draft_id=saved_draft["draft_id"],
            version=saved_draft["version"],
            content_hash=saved_draft["content_hash"],
            approved_by=args.approved_by,
        )
        try:
            asyncio.run(run_approve_workflow(workflow_approval))
        except (RuntimeError, TypeError, OSError, ValueError, RPCError) as exc:
            parser.exit(1, f"Error: {exc}\n")

        print("Approval signal sent.")
        return

    try:
        approval = asyncio.run(
            approve_draft(
                approval_id=args.approval_id,
                draft_id=saved_draft["draft_id"],
                version=saved_draft["version"],
                content_hash=saved_draft["content_hash"],
                approved_by=args.approved_by,
            )
        )
    except (RuntimeError, TypeError, OSError, ValueError) as exc:
        parser.exit(1, f"Error: {exc}\n")

    print(f"Approval ID: {approval['approval_id']}")
    print(f"Outbox ID: {approval['outbox_id']}")
    print(f"Outbox state: {approval['outbox_state']}")
